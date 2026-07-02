// SPDX-License-Identifier: GPL-2.0-only

// Package equations writes the per-physics deck fragments (stress solver, material,
// boundary conditions) onto a shared sif.Builder. It depends only on femmodel (the
// domain-shaped input values) and sif (the deck-writing primitives) — never on package
// elmer itself, so deck.go (which DOES import equations) can never form an import cycle.
package equations

import (
	"fmt"

	"oblikovati.org/elmer/elmer/femmodel"
	"oblikovati.org/elmer/elmer/sif"
)

// gPaToPa converts a Young's modulus from GPa (femmodel.MaterialObject's unit) to the
// Pascals Elmer's SIF dialect expects.
const gPaToPa = 1e9

// gCm3ToKgM3 converts a density from g/cm3 (femmodel.MaterialObject's unit) to the kg/m3
// Elmer's SIF dialect expects.
const gCm3ToKgM3 = 1000.0

// stressSolverProcedure is the fixed Elmer library/routine pair for the linear-elasticity
// stress solver, matching the vendored solver-validated smoke case
// (vendor-src/elmer/test/case.sif).
const stressSolverProcedure = sif.FileAttr("StressSolve/StressSolver")

// ElasticityInput is everything WriteElasticity needs to render one elasticity equation
// across one or more bodies: the resolved mesh body ids, the material and equation
// settings (femmodel-shaped), the resolved boundary ids per constraint kind, the load
// vectors/magnitudes those boundaries apply, and the shared ResultOutput solver section
// deck.go builds once per deck (never nil — see WriteElasticity's guard).
//
// Example:
//
//	in := equations.ElasticityInput{
//		Bodies: []int{1}, Material: mat, Eq: eq,
//		FixedBIDs: []int{1}, ForceBIDs: []int{2}, ForceN: [3]float64{0, 0, -1000},
//		OutputSolver: outSolver,
//	}
//	err := equations.WriteElasticity(b, in)
type ElasticityInput struct {
	Bodies       []int // mesh body ids
	Material     femmodel.MaterialObject
	Eq           femmodel.EquationObject
	FixedBIDs    []int // boundary ids
	ForceBIDs    []int
	ForceN       [3]float64 // per the load default, SI newtons, area-normalized in the deck
	PressBIDs    []int
	PressurePa   float64      // positive = pushes INTO the face -> written as Normal Force = -p
	OutputSolver *sif.Section // shared ResultOutput solver section (deck.go builds it)
}

// WriteElasticity writes one linear-elasticity equation onto b: a shared stress-solver
// section, per-body material properties, and the fixed/force/pressure boundary conditions
// resolved onto their boundary ids. Every body in in.Bodies shares the SAME stress-solver
// and output-solver *sif.Section instances (rendered once, referenced by every body's
// Active Solvers list) rather than one copy per body.
//
// Example:
//
//	err := equations.WriteElasticity(b, in)
func WriteElasticity(b *sif.Builder, in ElasticityInput) error {
	if err := validateElasticityInput(in); err != nil {
		return err
	}
	stress, err := stressSolverSection(in.Eq)
	if err != nil {
		return err
	}
	for _, body := range in.Bodies {
		writeMaterial(b, body, in.Material)
		b.AddSolver(body, stress)
		b.AddSolver(body, in.OutputSolver)
	}
	writeBoundaryConditions(b, in)
	return nil
}

// validateElasticityInput rejects the two ElasticityInput shapes that would otherwise
// silently produce a broken or empty deck: no resolved body ids, or a nil OutputSolver
// (which sif.Section.Set would render as a silent "Integer 0" reference).
func validateElasticityInput(in ElasticityInput) error {
	if len(in.Bodies) == 0 {
		return fmt.Errorf("equations: ElasticityInput.Bodies is empty, want at least one mesh body id")
	}
	if in.OutputSolver == nil {
		return fmt.Errorf("equations: ElasticityInput.OutputSolver is nil, want deck.go's shared ResultOutput solver section")
	}
	return nil
}

// stressSolverSection builds the shared linear-elasticity stress-solver section: the
// StressSolve procedure, the Displacement vector variable, and the linear/steady-state
// solver settings projected from eq. Calculate Stresses is written only when eq requests
// it (a false value is simply omitted, matching the "(when set)" deck rule).
func stressSolverSection(eq femmodel.EquationObject) (*sif.Section, error) {
	s, err := sif.NewSection(sif.Solver)
	if err != nil {
		return nil, fmt.Errorf("equations: building stress solver section: %w", err)
	}
	s.Set("Equation", "Stress Solver")
	s.Set("Procedure", stressSolverProcedure)
	s.Set("Variable", "Displacement")
	s.Set("Variable DOFs", 3)
	if eq.CalculateStresses {
		s.Set("Calculate Stresses", true)
	}
	s.Set("Optimize Bandwidth", true)
	s.Set("Stabilize", true)
	setLinearSystemKeys(s, eq)
	return s, nil
}

// setLinearSystemKeys writes the linear-system and steady-state solver keys the reference
// elasticity solve needs, projected 1:1 from eq's femmodel-neutral fields plus three fixed
// robustness keys (Abort Not Converged/Residual Output/Precondition Recompute) the M1
// reference deck always sets the same way.
func setLinearSystemKeys(s *sif.Section, eq femmodel.EquationObject) {
	s.Set("Linear System Solver", eq.LinearSolverType)
	s.Set("Linear System Iterative Method", eq.LinearIterativeMethod)
	s.Set("Linear System Max Iterations", eq.LinearIterations)
	s.Set("Linear System Convergence Tolerance", eq.LinearTolerance)
	s.Set("Linear System Preconditioning", eq.LinearPreconditioning)
	s.Set("Linear System Abort Not Converged", false)
	s.Set("Linear System Residual Output", 1)
	s.Set("Linear System Precondition Recompute", 1)
	s.Set("Steady State Convergence Tolerance", eq.SteadyStateTolerance)
}

// writeMaterial sets body's Material section from m, converting Young's modulus and
// density into the SI units (Pa, kg/m3) the SIF dialect expects.
func writeMaterial(b *sif.Builder, body int, m femmodel.MaterialObject) {
	b.Material(body, "Youngs Modulus", m.YoungGPa*gPaToPa)
	b.Material(body, "Poisson ratio", m.Poisson)
	b.Material(body, "Density", m.DensityGCm3*gCm3ToKgM3)
	b.Material(body, "Name", m.Name)
}

// writeBoundaryConditions writes every fixed/force/pressure boundary condition in.
func writeBoundaryConditions(b *sif.Builder, in ElasticityInput) {
	writeFixedBCs(b, in.FixedBIDs)
	writeForceBCs(b, in.ForceBIDs, in.ForceN)
	writePressureBCs(b, in.PressBIDs, in.PressurePa)
}

// writeFixedBCs zeroes all three displacement DOFs on each fixed boundary id.
func writeFixedBCs(b *sif.Builder, ids []int) {
	for _, id := range ids {
		b.Boundary(id, "Displacement 1", 0.0)
		b.Boundary(id, "Displacement 2", 0.0)
		b.Boundary(id, "Displacement 3", 0.0)
	}
}

// writeForceBCs writes all three force components on each force boundary id, adding the
// "Force i Normalize by Area" flag only for the nonzero components (a zero component needs
// no area normalization since it contributes no force either way).
func writeForceBCs(b *sif.Builder, ids []int, force [3]float64) {
	for _, id := range ids {
		for i, v := range force {
			key := fmt.Sprintf("Force %d", i+1)
			b.Boundary(id, key, v)
			if v != 0 {
				b.Boundary(id, key+" Normalize by Area", true)
			}
		}
	}
}

// writePressureBCs writes each pressure boundary id's Normal Force, negating pressurePa —
// a positive user-facing pressure pushes INTO the face (compression), but Elmer's Normal
// Force convention is positive-outward along the face normal, so the sign must flip.
func writePressureBCs(b *sif.Builder, ids []int, pressurePa float64) {
	for _, id := range ids {
		b.Boundary(id, "Normal Force", -pressurePa)
	}
}
