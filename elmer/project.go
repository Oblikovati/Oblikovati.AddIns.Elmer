// SPDX-License-Identifier: GPL-2.0-only

package elmer

import "oblikovati.org/elmer/elmer/femmodel"

// StudySettings is the flat, solve-time view of a femmodel.Analysis: exactly the fields the sif
// builder and mesh pipeline (Tasks 8+) read. femmodel.Analysis stays the sole source of truth —
// there is no flat-settings "extras" mechanism — this is a read-only snapshot taken once per
// study run, never written back into the aggregate.
type StudySettings struct {
	Simulation femmodel.SolverObject
	Mesh       femmodel.MeshObject
	Material   femmodel.MaterialObject
	Equation   femmodel.EquationObject
	Load       femmodel.LoadDefaults
}

// projectAnalysis flattens a femmodel.Analysis into the StudySettings the pipeline consumes.
// M1 seeds exactly one equation (Analysis.SetEquation never removes it, so index 0 always
// exists); a multi-equation aggregate widens this projection in a later milestone.
//
// Example:
//
//	settings := projectAnalysis(femmodel.NewDefaultAnalysis())
//	settings.Equation.Kind // "elasticity"
func projectAnalysis(a *femmodel.Analysis) StudySettings {
	return StudySettings{
		Simulation: a.Solver(),
		Mesh:       a.Mesh(),
		Material:   a.DefaultMaterial(),
		Equation:   a.Equations()[0],
		Load:       a.LoadDefaults(),
	}
}
