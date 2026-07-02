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
// M1 seeds exactly one equation via NewDefaultAnalysis (Analysis.SetEquation never removes it,
// so a *constructed* aggregate's index 0 always exists); a multi-equation aggregate widens this
// projection in a later milestone. A legally-constructible zero-value femmodel.Analysis{} (e.g.
// from a caller that skips NewDefaultAnalysis) carries a nil Equations() slice instead — rather
// than index out of range, projectAnalysis degrades gracefully to the same seeded default
// elasticity equation NewDefaultAnalysis itself uses, so the projection is always deterministic.
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
		Equation:   equationOrDefault(a),
		Load:       a.LoadDefaults(),
	}
}

// equationOrDefault returns the aggregate's first equation, or femmodel.DefaultElasticityEquation
// when the aggregate carries none — the zero-value-Analysis degradation path documented on
// projectAnalysis.
func equationOrDefault(a *femmodel.Analysis) femmodel.EquationObject {
	if eqs := a.Equations(); len(eqs) > 0 {
		return eqs[0]
	}
	return femmodel.DefaultElasticityEquation()
}
