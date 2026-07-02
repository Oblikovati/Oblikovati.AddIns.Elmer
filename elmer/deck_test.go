// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"oblikovati.org/elmer/elmer/femmodel"
	"oblikovati.org/elmer/elmer/sif"
)

// cantileverSettings returns the steel-cantilever StudySettings the golden deck fixture
// (testdata/golden_cantilever.sif) was hand-authored against: default steady-state/Cartesian
// 3D simulation, the M1 reference elasticity equation, and a 1000 N force load (deck.go
// projects the scalar LoadN onto -Z, matching the M0/M1 plan's cantilever oracle).
func cantileverSettings() StudySettings {
	a := femmodel.NewDefaultAnalysis()
	return StudySettings{
		Simulation: a.Solver(),
		Mesh:       a.Mesh(),
		Material:   a.DefaultMaterial(),
		Equation:   femmodel.DefaultElasticityEquation(),
		Load:       a.LoadDefaults(),
	}
}

// TestBuildDeckGoldenCantilever byte-compares buildDeck's output for one fixed body and one
// force-loaded boundary against testdata/golden_cantilever.sif — pinning the whole
// Simulation/Constants/stress-solver/output-solver/material/BC assembly in one shot.
func TestBuildDeckGoldenCantilever(t *testing.T) {
	s := cantileverSettings()
	bcs := map[string]int{"fixedFace": 1, "loadedFace": 2}
	resolved := resolvedConstraints{Fixed: []int{1}, Force: []int{2}}

	b, err := buildDeck(s, []int{1}, bcs, resolved)
	if err != nil {
		t.Fatalf("buildDeck: %v", err)
	}

	var buf bytes.Buffer
	if err := sif.Write(&buf, b); err != nil {
		t.Fatalf("sif.Write: %v", err)
	}

	want, err := os.ReadFile("testdata/golden_cantilever.sif")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if buf.String() != string(want) {
		t.Errorf("buildDeck output mismatch\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}

// TestBuildDeckPressureSignFlip pins the sign convention: a positive user-facing pressure
// (compression, pushing INTO the face) is written as a NEGATIVE Normal Force, matching the
// upstream Elmer convention that a positive Normal Force pushes OUT along the face normal.
func TestBuildDeckPressureSignFlip(t *testing.T) {
	s := cantileverSettings()
	s.Load = femmodel.LoadDefaults{LoadType: "pressure", PressureMPa: 10}
	bcs := map[string]int{"fixedFace": 1, "loadedFace": 2}
	resolved := resolvedConstraints{Fixed: []int{1}, Pressure: []int{2}}

	b, err := buildDeck(s, []int{1}, bcs, resolved)
	if err != nil {
		t.Fatalf("buildDeck: %v", err)
	}
	var buf bytes.Buffer
	if err := sif.Write(&buf, b); err != nil {
		t.Fatalf("sif.Write: %v", err)
	}
	want := "  Normal Force = Real -1e+07\n"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("output missing %q (10 MPa -> -1e7 Pa); got:\n%s", want, buf.String())
	}
}

// TestBuildDeckUnitConversions pins the two load-bearing unit conversions the brief calls
// out explicitly: Young's modulus GPa -> Pa (x1e9) and density g/cm3 -> kg/m3 (x1000).
func TestBuildDeckUnitConversions(t *testing.T) {
	s := cantileverSettings()
	s.Material = femmodel.MaterialObject{Name: "steel", YoungGPa: 210, Poisson: 0.3, DensityGCm3: 7.9}
	bcs := map[string]int{"fixedFace": 1}
	resolved := resolvedConstraints{Fixed: []int{1}}

	b, err := buildDeck(s, []int{1}, bcs, resolved)
	if err != nil {
		t.Fatalf("buildDeck: %v", err)
	}
	var buf bytes.Buffer
	if err := sif.Write(&buf, b); err != nil {
		t.Fatalf("sif.Write: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"2.1e+11", "7900"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q (unit conversion); got:\n%s", want, out)
		}
	}
}

// TestBuildDeckRejectsUnsupportedCoordinateSystem pins writeSimulation's enum guard: a
// CoordinateSystem value this deck writer's SIF-keyword mapping doesn't recognize errors,
// naming the offending value, rather than silently writing an empty/wrong Coordinate System.
func TestBuildDeckRejectsUnsupportedCoordinateSystem(t *testing.T) {
	s := cantileverSettings()
	s.Simulation.CoordinateSystem = "polar 2D"

	_, err := buildDeck(s, []int{1}, map[string]int{"fixedFace": 1}, resolvedConstraints{Fixed: []int{1}})
	if err == nil || !strings.Contains(err.Error(), "polar 2D") {
		t.Fatalf("buildDeck error = %v, want mention of the offending CoordinateSystem %q", err, "polar 2D")
	}
}

// TestBuildDeckRejectsUnsupportedSimulationType mirrors the CoordinateSystem guard for
// SimulationType.
func TestBuildDeckRejectsUnsupportedSimulationType(t *testing.T) {
	s := cantileverSettings()
	s.Simulation.SimulationType = "transient"

	_, err := buildDeck(s, []int{1}, map[string]int{"fixedFace": 1}, resolvedConstraints{Fixed: []int{1}})
	if err == nil || !strings.Contains(err.Error(), "transient") {
		t.Fatalf("buildDeck error = %v, want mention of the offending SimulationType %q", err, "transient")
	}
}

// TestBuildDeckPropagatesWriteElasticityError pins the equations.WriteElasticity
// error-propagation path: an ElasticityInput WriteElasticity itself rejects (here, no
// resolved body ids) surfaces through buildDeck rather than being swallowed.
func TestBuildDeckPropagatesWriteElasticityError(t *testing.T) {
	s := cantileverSettings()

	_, err := buildDeck(s, nil, map[string]int{"fixedFace": 1}, resolvedConstraints{Fixed: []int{1}})
	if err == nil || !strings.Contains(err.Error(), "Bodies") {
		t.Fatalf("buildDeck error = %v, want the underlying WriteElasticity Bodies error", err)
	}
}

// TestBuildDeckRejectsUnknownBoundaryID pins the bcs-validation rule: a resolved boundary id
// that was never assigned by exportMesh (not a value present in bcs) is an add-in bug —
// Task 12's constraint resolution drifted from the mesh export — and must error rather than
// silently write a Target Boundaries reference to a boundary the mesh never defined.
func TestBuildDeckRejectsUnknownBoundaryID(t *testing.T) {
	s := cantileverSettings()
	bcs := map[string]int{"fixedFace": 1}
	resolved := resolvedConstraints{Fixed: []int{1}, Force: []int{99}}

	_, err := buildDeck(s, []int{1}, bcs, resolved)
	if err == nil || !strings.Contains(err.Error(), "99") {
		t.Fatalf("buildDeck error = %v, want mention of the offending boundary id 99", err)
	}
}

// TestWriteDeckFilesWritesCaseSif pins writeDeckFiles' contract: the rendered deck lands at
// dir/case.sif — the fixed filename runElmerSolver's ELMERSOLVER_STARTINFO (solve.go,
// deckName) always points ElmerSolver at — with byte-identical content to sif.Write's own
// output.
func TestWriteDeckFilesWritesCaseSif(t *testing.T) {
	s := cantileverSettings()
	b, err := buildDeck(s, []int{1}, map[string]int{"fixedFace": 1}, resolvedConstraints{Fixed: []int{1}})
	if err != nil {
		t.Fatalf("buildDeck: %v", err)
	}

	dir := t.TempDir()
	if err := writeDeckFiles(dir, b); err != nil {
		t.Fatalf("writeDeckFiles: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "case.sif"))
	if err != nil {
		t.Fatalf("read written deck: %v", err)
	}
	var want bytes.Buffer
	if err := sif.Write(&want, b); err != nil {
		t.Fatalf("sif.Write: %v", err)
	}
	if string(got) != want.String() {
		t.Errorf("written deck mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want.String())
	}
}

// TestWriteDeckFilesErrorsOnBadDir pins the error-propagation path: an unwritable target
// directory surfaces os.Create's error rather than panicking or silently no-op-ing.
func TestWriteDeckFilesErrorsOnBadDir(t *testing.T) {
	b := sif.NewBuilder()
	err := writeDeckFiles(filepath.Join(t.TempDir(), "does-not-exist"), b)
	if err == nil {
		t.Fatal("writeDeckFiles: want an error for a nonexistent directory, got nil")
	}
}
