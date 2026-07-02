// SPDX-License-Identifier: GPL-2.0-only

package femmodel

import (
	"strings"
	"testing"
)

// TestNewDefaultAnalysisSolver pins the M1 default simulation setup: steady state, Cartesian 3D,
// converged in a single outer iteration (elasticity is linear).
func TestNewDefaultAnalysisSolver(t *testing.T) {
	s := NewDefaultAnalysis().Solver()
	if s.SimulationType != "steady state" {
		t.Errorf("SimulationType = %q, want %q", s.SimulationType, "steady state")
	}
	if s.CoordinateSystem != "cartesian 3D" {
		t.Errorf("CoordinateSystem = %q, want %q", s.CoordinateSystem, "cartesian 3D")
	}
	if s.SteadyStateMaxIter != 1 {
		t.Errorf("SteadyStateMaxIter = %d, want 1", s.SteadyStateMaxIter)
	}
}

// TestNewDefaultAnalysisMesh pins the M1 default mesh: quadratic (order 2) elements at 5 mm.
func TestNewDefaultAnalysisMesh(t *testing.T) {
	m := NewDefaultAnalysis().Mesh()
	if m.Order != 2 {
		t.Errorf("Order = %d, want 2", m.Order)
	}
	if m.MaxSizeMM != 5 {
		t.Errorf("MaxSizeMM = %v, want 5", m.MaxSizeMM)
	}
}

// TestNewDefaultAnalysisMaterial pins the M1 default material: a steel-ish fallback.
func TestNewDefaultAnalysisMaterial(t *testing.T) {
	mat := NewDefaultAnalysis().DefaultMaterial()
	if mat.Name != "steel" {
		t.Errorf("Name = %q, want %q", mat.Name, "steel")
	}
	if mat.YoungGPa != 210 {
		t.Errorf("YoungGPa = %v, want 210", mat.YoungGPa)
	}
	if mat.Poisson != 0.3 {
		t.Errorf("Poisson = %v, want 0.3", mat.Poisson)
	}
	if mat.DensityGCm3 != 7.9 {
		t.Errorf("DensityGCm3 = %v, want 7.9", mat.DensityGCm3)
	}
}

// TestNewDefaultAnalysisEquations pins the M1 default: exactly one elasticity equation with the
// reference linear-solver settings.
func TestNewDefaultAnalysisEquations(t *testing.T) {
	eqs := NewDefaultAnalysis().Equations()
	if len(eqs) != 1 {
		t.Fatalf("len(Equations()) = %d, want 1", len(eqs))
	}
	eq := eqs[0]
	if eq.Kind != "elasticity" {
		t.Errorf("Kind = %q, want %q", eq.Kind, "elasticity")
	}
	if eq.LinearSolverType != "Iterative" {
		t.Errorf("LinearSolverType = %q, want %q", eq.LinearSolverType, "Iterative")
	}
	if eq.LinearIterativeMethod != "BiCGStab" {
		t.Errorf("LinearIterativeMethod = %q, want %q", eq.LinearIterativeMethod, "BiCGStab")
	}
	if eq.LinearPreconditioning != "ILU0" {
		t.Errorf("LinearPreconditioning = %q, want %q", eq.LinearPreconditioning, "ILU0")
	}
	if eq.LinearIterations != 500 {
		t.Errorf("LinearIterations = %d, want 500", eq.LinearIterations)
	}
	if eq.LinearTolerance != 1e-8 {
		t.Errorf("LinearTolerance = %v, want 1e-8", eq.LinearTolerance)
	}
	if eq.SteadyStateTolerance != 1e-5 {
		t.Errorf("SteadyStateTolerance = %v, want 1e-5", eq.SteadyStateTolerance)
	}
	if !eq.CalculateStresses {
		t.Error("CalculateStresses = false, want true")
	}
}

// TestNewDefaultAnalysisLoadDefaults pins the M1 default load: a 1000 N force.
func TestNewDefaultAnalysisLoadDefaults(t *testing.T) {
	l := NewDefaultAnalysis().LoadDefaults()
	if l.LoadType != "force" {
		t.Errorf("LoadType = %q, want %q", l.LoadType, "force")
	}
	if l.LoadN != 1000 {
		t.Errorf("LoadN = %v, want 1000", l.LoadN)
	}
	if l.PressureMPa != 0 {
		t.Errorf("PressureMPa = %v, want 0", l.PressureMPa)
	}
}

// TestSetSolverReplacesWholeObject proves SetSolver replaces the entire value, not a merge.
func TestSetSolverReplacesWholeObject(t *testing.T) {
	a := NewDefaultAnalysis()
	want := SolverObject{SimulationType: "transient", CoordinateSystem: "axi symmetric", SteadyStateMaxIter: 20}
	a.SetSolver(want)
	if got := a.Solver(); got != want {
		t.Errorf("Solver() = %+v, want %+v", got, want)
	}
}

// TestSetMeshReplacesWholeObject proves SetMesh replaces the entire value, not a merge.
func TestSetMeshReplacesWholeObject(t *testing.T) {
	a := NewDefaultAnalysis()
	want := MeshObject{Order: 1, MaxSizeMM: 2.5}
	a.SetMesh(want)
	if got := a.Mesh(); got != want {
		t.Errorf("Mesh() = %+v, want %+v", got, want)
	}
}

// TestSetDefaultMaterialReplacesWholeObject proves SetDefaultMaterial replaces the entire value.
func TestSetDefaultMaterialReplacesWholeObject(t *testing.T) {
	a := NewDefaultAnalysis()
	want := MaterialObject{Name: "aluminium", YoungGPa: 69, Poisson: 0.33, DensityGCm3: 2.70}
	a.SetDefaultMaterial(want)
	if got := a.DefaultMaterial(); got != want {
		t.Errorf("DefaultMaterial() = %+v, want %+v", got, want)
	}
}

// TestSetLoadDefaultsReplacesWholeObject proves SetLoadDefaults replaces the entire value.
func TestSetLoadDefaultsReplacesWholeObject(t *testing.T) {
	a := NewDefaultAnalysis()
	want := LoadDefaults{LoadType: "pressure", LoadN: 0, PressureMPa: 3.5}
	a.SetLoadDefaults(want)
	if got := a.LoadDefaults(); got != want {
		t.Errorf("LoadDefaults() = %+v, want %+v", got, want)
	}
}

// TestSetEquationReplacesWholeObject proves SetEquation replaces the whole equation at the given
// index.
func TestSetEquationReplacesWholeObject(t *testing.T) {
	a := NewDefaultAnalysis()
	want := EquationObject{Kind: "elasticity", LinearSolverType: "Direct", LinearIterativeMethod: "",
		LinearPreconditioning: "", LinearIterations: 0, LinearTolerance: 1e-10,
		SteadyStateTolerance: 1e-6, CalculateStresses: false}
	if err := a.SetEquation(0, want); err != nil {
		t.Fatalf("SetEquation(0, ...) = %v, want nil", err)
	}
	if got := a.Equations()[0]; got != want {
		t.Errorf("Equations()[0] = %+v, want %+v", got, want)
	}
}

// TestSetEquationOutOfRangeErrors proves an out-of-range index errors, naming the offending
// index and the current equation count — not a silent no-op or a panic. Asserts the error text
// itself (not just non-nil) so the "names the offending value" contract can't silently regress
// to a generic message.
func TestSetEquationOutOfRangeErrors(t *testing.T) {
	a := NewDefaultAnalysis()
	for _, i := range []int{-1, 1, 99} {
		err := a.SetEquation(i, EquationObject{})
		if err == nil {
			t.Fatalf("SetEquation(%d, ...) = nil error, want an out-of-range error", i)
		}
	}

	err := a.SetEquation(99, EquationObject{})
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("SetEquation(99, ...) error = %q, want it to mention the offending index 99", err.Error())
	}
	if !strings.Contains(err.Error(), "1") {
		t.Errorf("SetEquation(99, ...) error = %q, want it to mention the current count 1", err.Error())
	}
}

// TestEquationsReturnsDefensiveCopy proves mutating the slice returned by Equations() does not
// affect the aggregate's own state.
func TestEquationsReturnsDefensiveCopy(t *testing.T) {
	a := NewDefaultAnalysis()
	eqs := a.Equations()
	eqs[0].Kind = "thermal"
	if got := a.Equations()[0].Kind; got != "elasticity" {
		t.Errorf("aggregate Kind = %q after external mutation, want unaffected %q", got, "elasticity")
	}
}
