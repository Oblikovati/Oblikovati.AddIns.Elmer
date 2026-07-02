// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"testing"

	"oblikovati.org/elmer/elmer/femmodel"
)

// TestProjectAnalysisCopiesEveryField asserts projectAnalysis fills every StudySettings field
// with the aggregate's own values, field-by-field, against the default aggregate — so a field
// silently dropped from the projection (the M40 "silent degradation" failure mode) fails loudly
// here instead of surfacing later as a solver reading a zero value.
func TestProjectAnalysisCopiesEveryField(t *testing.T) {
	a := femmodel.NewDefaultAnalysis()
	got := projectAnalysis(a)

	if got.Simulation != a.Solver() {
		t.Errorf("Simulation = %+v, want %+v", got.Simulation, a.Solver())
	}
	if got.Mesh != a.Mesh() {
		t.Errorf("Mesh = %+v, want %+v", got.Mesh, a.Mesh())
	}
	if got.Material != a.DefaultMaterial() {
		t.Errorf("Material = %+v, want %+v", got.Material, a.DefaultMaterial())
	}
	if got.Equation != a.Equations()[0] {
		t.Errorf("Equation = %+v, want %+v", got.Equation, a.Equations()[0])
	}
	if got.Load != a.LoadDefaults() {
		t.Errorf("Load = %+v, want %+v", got.Load, a.LoadDefaults())
	}
}

// TestProjectAnalysisReflectsCustomValues proves the projection reads the aggregate's current
// state (not the M1 defaults baked in some other way) by mutating every object first.
func TestProjectAnalysisReflectsCustomValues(t *testing.T) {
	a := femmodel.NewDefaultAnalysis()
	a.SetSolver(femmodel.SolverObject{SimulationType: "transient", CoordinateSystem: "axi symmetric", SteadyStateMaxIter: 40})
	a.SetMesh(femmodel.MeshObject{Order: 1, MaxSizeMM: 1.5})
	a.SetDefaultMaterial(femmodel.MaterialObject{Name: "aluminium", YoungGPa: 69, Poisson: 0.33, DensityGCm3: 2.70})
	eq := femmodel.EquationObject{Kind: "elasticity", LinearSolverType: "Direct", LinearTolerance: 1e-10,
		SteadyStateTolerance: 1e-6, CalculateStresses: false}
	if err := a.SetEquation(0, eq); err != nil {
		t.Fatalf("SetEquation(0, ...) = %v, want nil", err)
	}
	a.SetLoadDefaults(femmodel.LoadDefaults{LoadType: "pressure", PressureMPa: 2.5})

	got := projectAnalysis(a)

	if got.Simulation != a.Solver() || got.Mesh != a.Mesh() || got.Material != a.DefaultMaterial() ||
		got.Equation != eq || got.Load != a.LoadDefaults() {
		t.Errorf("projectAnalysis(a) = %+v, did not track the mutated aggregate", got)
	}
}

// TestProjectAnalysisZeroValueDegradesToDefaultEquation proves the M40 "silent degradation"
// failure mode does not apply here as an outright panic: a legally-constructible zero-value
// femmodel.Analysis{} has a nil Equations() slice, so projectAnalysis must not index it
// unconditionally. It degrades gracefully to the same seeded default elasticity equation
// NewDefaultAnalysis uses, and produces zero-valued Simulation/Mesh/Material/Load — deterministic,
// not a panic.
func TestProjectAnalysisZeroValueDegradesToDefaultEquation(t *testing.T) {
	got := projectAnalysis(&femmodel.Analysis{})

	want := StudySettings{
		Simulation: femmodel.SolverObject{},
		Mesh:       femmodel.MeshObject{},
		Material:   femmodel.MaterialObject{},
		Equation:   femmodel.DefaultElasticityEquation(),
		Load:       femmodel.LoadDefaults{},
	}
	if got != want {
		t.Errorf("projectAnalysis(&femmodel.Analysis{}) = %+v, want %+v", got, want)
	}
}
