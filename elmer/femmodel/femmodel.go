// SPDX-License-Identifier: GPL-2.0-only

// Package femmodel is the Elmer-shaped domain aggregate: the sole source of truth for what an
// Elmer study will solve, independent of the sif/mesh/pipeline machinery that consumes it (see
// elmer.projectAnalysis). It is pure — no imports from the engine or oblikovati.org/api — so it
// unit-tests in isolation and mirrors the CalculiX add-in's ccx/femmodel idiom: unexported
// aggregate fields behind invariant-guarding accessors/mutators, and neutral strings for enums
// (the pipeline layer maps them onto Elmer's own SIF keyword vocabulary).
package femmodel

import "fmt"

// SolverObject is the Elmer <Simulation> section object: the coordinate system, the analysis
// mode ("steady state" in M1; "transient" is a later milestone), and how many outer
// steady-state iterations the equation loop runs before it is considered converged.
type SolverObject struct {
	SimulationType     string
	CoordinateSystem   string
	SteadyStateMaxIter int
}

// newDefaultSolver returns the M1 default: a steady-state Cartesian 3D simulation converged in
// a single outer iteration (linear elasticity needs no more).
func newDefaultSolver() SolverObject {
	return SolverObject{SimulationType: "steady state", CoordinateSystem: "cartesian 3D", SteadyStateMaxIter: 1}
}

// MeshObject is the volume-mesh object: element order (2 = quadratic, Elmer's usual default for
// stress accuracy) and the target characteristic element size in millimetres.
type MeshObject struct {
	Order     int
	MaxSizeMM float64
}

// newDefaultMesh returns the M1 default: quadratic (order 2) elements at a 5 mm target size.
func newDefaultMesh() MeshObject {
	return MeshObject{Order: 2, MaxSizeMM: 5}
}

// MaterialObject is the M1 single material assignment, applied to the whole body (per-body
// scoping is a later milestone, mirroring ccx/femmodel.MaterialObject's ScopeAll fallback).
type MaterialObject struct {
	Name        string
	YoungGPa    float64
	Poisson     float64
	DensityGCm3 float64
}

// newDefaultMaterial returns a steel-ish fallback material: 210 GPa, Poisson 0.3, 7.9 g/cm3.
func newDefaultMaterial() MaterialObject {
	return MaterialObject{Name: "steel", YoungGPa: 210, Poisson: 0.3, DensityGCm3: 7.9}
}

// EquationObject is one Elmer <Solver> equation block. Kind selects the physics ("elasticity" in
// M1 — later milestones add more, which is why Analysis carries a slice of these even though M1
// seeds exactly one). The Linear* fields configure the linear-system solve run inside each
// steady-state iteration; SteadyStateTolerance gates the outer loop bounded by
// SolverObject.SteadyStateMaxIter.
type EquationObject struct {
	Kind                  string // "elasticity" (M1)
	LinearSolverType      string
	LinearIterativeMethod string
	LinearPreconditioning string
	LinearIterations      int
	LinearTolerance       float64
	SteadyStateTolerance  float64
	CalculateStresses     bool
}

// DefaultElasticityEquation returns the M1 reference elasticity solve: an iterative BiCGStab
// linear solve with ILU0 preconditioning (500 iterations, 1e-8 tolerance) inside a looser 1e-5
// steady-state gate, with stress recovery enabled. Exported so elmer.projectAnalysis can fall
// back to it when a zero-value Analysis carries no equations, without duplicating the literal
// seed values (NewDefaultAnalysis uses this same function, so the two can never drift apart).
//
// Example:
//
//	eq := femmodel.DefaultElasticityEquation()
//	eq.Kind // "elasticity"
func DefaultElasticityEquation() EquationObject {
	return EquationObject{
		Kind:                  "elasticity",
		LinearSolverType:      "Iterative",
		LinearIterativeMethod: "BiCGStab",
		LinearPreconditioning: "ILU0",
		LinearIterations:      500,
		LinearTolerance:       1e-8,
		SteadyStateTolerance:  1e-5,
		CalculateStresses:     true,
	}
}

// LoadDefaults holds the study's default load — the numbers the implicit convention applies to
// the loaded faces at solve time. LoadType selects which of the two fields is active
// ("force" | "pressure"); it is a neutral string here, cast at the pipeline layer.
type LoadDefaults struct {
	LoadType    string
	LoadN       float64
	PressureMPa float64
}

// newDefaultLoadDefaults returns the M1 default: a 1000 N force load.
func newDefaultLoadDefaults() LoadDefaults {
	return LoadDefaults{LoadType: "force", LoadN: 1000}
}

// Analysis is the root aggregate: exactly one Solver, Mesh, and default Material, one or more
// Equations (M1 seeds exactly one elasticity equation — Elmer is equation-centric, so this stays
// a slice from day one), and one LoadDefaults template. There is no flat-settings "extras"
// escape hatch: the aggregate is the sole source of truth the pipeline projects from (see
// elmer.projectAnalysis). Mutators replace whole value objects; Equations() defensively copies
// so a caller cannot mutate the aggregate by aliasing its internal slice.
type Analysis struct {
	solver       SolverObject
	mesh         MeshObject
	material     MaterialObject
	equations    []EquationObject
	loadDefaults LoadDefaults
}

// NewDefaultAnalysis returns the M1 reference study: steady-state Cartesian 3D solved in one
// outer iteration, a 5 mm quadratic mesh, a steel-ish material, one elasticity equation with the
// reference linear-solver settings, and a 1000 N default force load.
//
// Example:
//
//	a := femmodel.NewDefaultAnalysis()
//	a.Solver().SimulationType // "steady state"
func NewDefaultAnalysis() *Analysis {
	return &Analysis{
		solver:       newDefaultSolver(),
		mesh:         newDefaultMesh(),
		material:     newDefaultMaterial(),
		equations:    []EquationObject{DefaultElasticityEquation()},
		loadDefaults: newDefaultLoadDefaults(),
	}
}

// Solver returns the study's single solver (simulation) object.
//
// Example:
//
//	sim := a.Solver()
//	sim.SimulationType // "steady state"
func (a *Analysis) Solver() SolverObject { return a.solver }

// SetSolver replaces the solver object wholesale.
//
// Example:
//
//	a.SetSolver(femmodel.SolverObject{SimulationType: "steady state", CoordinateSystem: "cartesian 3D", SteadyStateMaxIter: 3})
func (a *Analysis) SetSolver(s SolverObject) { a.solver = s }

// Mesh returns the study's single volume-mesh object.
//
// Example:
//
//	m := a.Mesh()
//	m.Order // 2
func (a *Analysis) Mesh() MeshObject { return a.mesh }

// SetMesh replaces the mesh object wholesale.
//
// Example:
//
//	a.SetMesh(femmodel.MeshObject{Order: 1, MaxSizeMM: 2.5})
func (a *Analysis) SetMesh(m MeshObject) { a.mesh = m }

// DefaultMaterial returns the study's single material assignment.
//
// Example:
//
//	mat := a.DefaultMaterial()
//	mat.Name // "steel"
func (a *Analysis) DefaultMaterial() MaterialObject { return a.material }

// SetDefaultMaterial replaces the material object wholesale.
//
// Example:
//
//	a.SetDefaultMaterial(femmodel.MaterialObject{Name: "aluminium", YoungGPa: 69, Poisson: 0.33, DensityGCm3: 2.70})
func (a *Analysis) SetDefaultMaterial(m MaterialObject) { a.material = m }

// Equations returns a defensive copy of the equation list: mutating the result never affects the
// aggregate. Go through SetEquation to change one in place.
//
// Example:
//
//	eqs := a.Equations()
//	eqs[0].Kind = "thermal" // does not change a
func (a *Analysis) Equations() []EquationObject {
	cp := make([]EquationObject, len(a.equations))
	copy(cp, a.equations)
	return cp
}

// SetEquation replaces the equation at index i wholesale. It errors — naming the offending index
// and the current equation count — rather than panicking or silently no-op-ing when i is out of
// range.
//
// Example:
//
//	err := a.SetEquation(0, femmodel.EquationObject{Kind: "elasticity"})
func (a *Analysis) SetEquation(i int, eq EquationObject) error {
	if i < 0 || i >= len(a.equations) {
		return fmt.Errorf("femmodel: equation index %d out of range, have %d equation(s)", i, len(a.equations))
	}
	a.equations[i] = eq
	return nil
}

// LoadDefaults returns the study's default-load template.
//
// Example:
//
//	l := a.LoadDefaults()
//	l.LoadType // "force"
func (a *Analysis) LoadDefaults() LoadDefaults { return a.loadDefaults }

// SetLoadDefaults replaces the default-load template wholesale.
//
// Example:
//
//	a.SetLoadDefaults(femmodel.LoadDefaults{LoadType: "pressure", PressureMPa: 2.5})
func (a *Analysis) SetLoadDefaults(l LoadDefaults) { a.loadDefaults = l }
