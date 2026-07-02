// SPDX-License-Identifier: GPL-2.0-only

package equations

import (
	"bytes"
	"strings"
	"testing"

	"oblikovati.org/elmer/elmer/femmodel"
	"oblikovati.org/elmer/elmer/sif"
)

// newOutputSolver returns a minimal ResultOutput-shaped solver section, standing in for the
// shared instance deck.go builds — WriteElasticity only requires it be non-nil.
func newOutputSolver(t *testing.T) *sif.Section {
	t.Helper()
	s, err := sif.NewSection(sif.Solver)
	if err != nil {
		t.Fatalf("NewSection(Solver): %v", err)
	}
	s.Set("Equation", "ResultOutput")
	return s
}

// writeOne runs WriteElasticity against a fresh Builder and returns the rendered deck text,
// failing the test on any error from either step.
func writeOne(t *testing.T, in ElasticityInput) string {
	t.Helper()
	b := sif.NewBuilder()
	if err := WriteElasticity(b, in); err != nil {
		t.Fatalf("WriteElasticity: %v", err)
	}
	var buf bytes.Buffer
	if err := sif.Write(&buf, b); err != nil {
		t.Fatalf("sif.Write: %v", err)
	}
	return buf.String()
}

// TestWriteElasticityRejectsNoBodies pins the empty-Bodies rejection: a caller that forgot
// to resolve any mesh body id gets a clear error rather than a silent no-op deck.
func TestWriteElasticityRejectsNoBodies(t *testing.T) {
	in := ElasticityInput{OutputSolver: newOutputSolver(t)}
	b := sif.NewBuilder()
	err := WriteElasticity(b, in)
	if err == nil || !strings.Contains(err.Error(), "Bodies") {
		t.Fatalf("WriteElasticity error = %v, want mention of Bodies", err)
	}
}

// TestWriteElasticityRejectsNilOutputSolver pins the load-bearing nil-pointer guard called
// out in the interfaces contract: passing a nil *sif.Section here would otherwise render as
// a silent "Integer 0" reference (see sif.Section.Set's doc comment) rather than failing.
func TestWriteElasticityRejectsNilOutputSolver(t *testing.T) {
	in := ElasticityInput{Bodies: []int{1}}
	b := sif.NewBuilder()
	err := WriteElasticity(b, in)
	if err == nil || !strings.Contains(err.Error(), "OutputSolver") {
		t.Fatalf("WriteElasticity error = %v, want mention of OutputSolver", err)
	}
}

// TestWriteElasticityMaterialUnitConversions pins the two unit conversions the deck writer
// owns: Young's modulus GPa -> Pa (x1e9) and density g/cm3 -> kg/m3 (x1000).
func TestWriteElasticityMaterialUnitConversions(t *testing.T) {
	in := ElasticityInput{
		Bodies:       []int{1},
		Material:     femmodel.MaterialObject{Name: "steel", YoungGPa: 210, Poisson: 0.3, DensityGCm3: 7.9},
		Eq:           femmodel.DefaultElasticityEquation(),
		OutputSolver: newOutputSolver(t),
	}
	out := writeOne(t, in)
	for _, want := range []string{
		"  Youngs Modulus = Real 2.1e+11\n",
		"  Density = Real 7900\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
}

// TestWriteElasticityCalculateStressesOmittedWhenFalse pins the "(when set)" rule from the
// deck contents brief: Calculate Stresses is only written when the equation asks for it.
func TestWriteElasticityCalculateStressesOmittedWhenFalse(t *testing.T) {
	in := ElasticityInput{
		Bodies:       []int{1},
		Eq:           femmodel.EquationObject{CalculateStresses: false},
		OutputSolver: newOutputSolver(t),
	}
	out := writeOne(t, in)
	if strings.Contains(out, "Calculate Stresses") {
		t.Errorf("output contains Calculate Stresses when the equation disabled it; got:\n%s", out)
	}
}

// TestWriteElasticityFixedBoundaryZeroesAllDOFs pins the fixed-BC shape directly at the
// equations-package layer (the golden deck test in package elmer exercises this too, but
// only transitively): every fixed boundary id gets all three displacement DOFs zeroed.
func TestWriteElasticityFixedBoundaryZeroesAllDOFs(t *testing.T) {
	in := ElasticityInput{
		Bodies:       []int{1},
		Eq:           femmodel.DefaultElasticityEquation(),
		FixedBIDs:    []int{1},
		OutputSolver: newOutputSolver(t),
	}
	out := writeOne(t, in)
	for _, want := range []string{
		"  Displacement 1 = Real 0\n",
		"  Displacement 2 = Real 0\n",
		"  Displacement 3 = Real 0\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
}

// TestWriteElasticityForceOnlyNormalizesNonzeroComponents pins the per-component "Force i
// Normalize by Area" rule: only the nonzero force components get the flag, not all three.
func TestWriteElasticityForceOnlyNormalizesNonzeroComponents(t *testing.T) {
	in := ElasticityInput{
		Bodies:       []int{1},
		Eq:           femmodel.DefaultElasticityEquation(),
		ForceBIDs:    []int{2},
		ForceN:       [3]float64{0, 0, -1000},
		OutputSolver: newOutputSolver(t),
	}
	out := writeOne(t, in)
	if !strings.Contains(out, "  Force 3 Normalize by Area = Logical True\n") {
		t.Errorf("output missing Force 3 Normalize by Area; got:\n%s", out)
	}
	for _, unwanted := range []string{"Force 1 Normalize by Area", "Force 2 Normalize by Area"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output contains %q for a zero force component; got:\n%s", unwanted, out)
		}
	}
}

// TestWriteElasticityPressureSignFlip pins the sign convention at the equations-package
// layer: a positive PressurePa (compression) is written as a negative Normal Force.
func TestWriteElasticityPressureSignFlip(t *testing.T) {
	in := ElasticityInput{
		Bodies:       []int{1},
		Eq:           femmodel.DefaultElasticityEquation(),
		PressBIDs:    []int{3},
		PressurePa:   1.0e7,
		OutputSolver: newOutputSolver(t),
	}
	out := writeOne(t, in)
	if !strings.Contains(out, "  Normal Force = Real -1e+07\n") {
		t.Errorf("output missing negated Normal Force; got:\n%s", out)
	}
}

// TestWriteElasticitySharesSolverSectionsAcrossBodies pins the multi-body fan-out: every
// body in Bodies gets the SAME stress-solver and output-solver section instances (Active
// Solvers referencing shared sections), not a duplicate per body.
func TestWriteElasticitySharesSolverSectionsAcrossBodies(t *testing.T) {
	in := ElasticityInput{
		Bodies:       []int{1, 2},
		Material:     femmodel.MaterialObject{Name: "steel", YoungGPa: 210, Poisson: 0.3, DensityGCm3: 7.9},
		Eq:           femmodel.DefaultElasticityEquation(),
		OutputSolver: newOutputSolver(t),
	}
	out := writeOne(t, in)
	if got := strings.Count(out, "Solver 1\n"); got != 1 {
		t.Errorf("want exactly one emitted Solver 1 section shared across bodies, got %d in:\n%s", got, out)
	}
	if got := strings.Count(out, "Active Solvers(2) = Integer 1 2\n"); got != 2 {
		t.Errorf("want both bodies' Equations referencing solver ids 1 2, got %d in:\n%s", got, out)
	}
}
