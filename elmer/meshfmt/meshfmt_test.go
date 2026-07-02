// SPDX-License-Identifier: GPL-2.0-only

package meshfmt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cube5Tet is the Task 5 solver-validated 5-tet unit-cube decomposition, reproduced here as a
// Mesh literal so TestWriteGoldenCube5Tet can drive it through Write and diff the result against
// the byte-copied fixture in testdata/cube5tet (vendor-src/elmer/test/mesh — the exact mesh the
// CI smoke job solves to the analytic answer).
func cube5Tet() Mesh {
	return Mesh{
		Nodes: [][3]float64{
			{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0},
			{0, 0, 1}, {1, 0, 1}, {1, 1, 1}, {0, 1, 1},
		},
		Tets: []Tet{
			{Body: 1, Nodes: []int{1, 2, 4, 5}},
			{Body: 1, Nodes: []int{2, 3, 4, 7}},
			{Body: 1, Nodes: []int{5, 6, 2, 7}},
			{Body: 1, Nodes: []int{5, 8, 7, 4}},
			{Body: 1, Nodes: []int{2, 4, 5, 7}},
		},
		Boundary: []BoundaryFace{
			{Boundary: 1, Parent: 1, Nodes: []int{1, 2, 4}},
			{Boundary: 1, Parent: 2, Nodes: []int{2, 3, 4}},
			{Boundary: 2, Parent: 3, Nodes: []int{5, 6, 7}},
			{Boundary: 2, Parent: 4, Nodes: []int{5, 7, 8}},
		},
	}
}

// order2Tet10 is a single hand-computed quadratic tet: the corner nodes of cube5Tet's first tet
// (1,2,4,5) renumbered 1-4, plus the 6 edge midpoints in Elmer's (1,2),(2,3),(3,1),(1,4),(2,4),
// (3,4) order (same as CalculiX C3D10), plus that tet's bottom (z=0) face as a quadratic
// boundary triangle. See meshfmt_test.go's package doc comment / task report for the by-hand
// midpoint derivation.
func order2Tet10() Mesh {
	return Mesh{
		Nodes: [][3]float64{
			{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {0, 0, 1}, // corners 1-4
			{0.5, 0, 0}, {0.5, 0.5, 0}, {0, 0.5, 0}, // edges (1,2) (2,3) (3,1)
			{0, 0, 0.5}, {0.5, 0, 0.5}, {0, 0.5, 0.5}, // edges (1,4) (2,4) (3,4)
		},
		Tets: []Tet{
			{Body: 1, Nodes: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		},
		Boundary: []BoundaryFace{
			{Boundary: 1, Parent: 1, Nodes: []int{1, 2, 3, 5, 6, 7}},
		},
	}
}

// assertMatchesGolden writes m to a fresh t.TempDir() and compares all four Elmer mesh files
// against goldenDir byte-for-byte, failing with a diff-friendly message on any mismatch.
func assertMatchesGolden(t *testing.T, m Mesh, goldenDir string) {
	t.Helper()
	dir := t.TempDir()
	if err := Write(dir, m); err != nil {
		t.Fatalf("Write(%s, ...) = %v, want nil", dir, err)
	}
	for _, name := range []string{"mesh.header", "mesh.nodes", "mesh.elements", "mesh.boundary"} {
		want, err := os.ReadFile(filepath.Join(goldenDir, name))
		if err != nil {
			t.Fatalf("read golden %s: %v", name, err)
		}
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read written %s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s mismatch\n--- got ---\n%s--- want ---\n%s", name, got, want)
		}
	}
}

// TestWriteGoldenCube5Tet pins Write's output against the solver-validated 5-tet cube fixture
// (linear tets, type 504/303).
func TestWriteGoldenCube5Tet(t *testing.T) {
	assertMatchesGolden(t, cube5Tet(), filepath.Join("testdata", "cube5tet"))
}

// TestWriteGoldenOrder2Tet10 pins Write's output for a quadratic tet (type 510/306), including
// the hand-computed edge-midpoint node order.
func TestWriteGoldenOrder2Tet10(t *testing.T) {
	assertMatchesGolden(t, order2Tet10(), filepath.Join("testdata", "order2tet10"))
}

// TestWriteUnsupportedTetNodeCountErrors proves a tet with a node count other than 4 or 10 is
// rejected, naming both the offending element index and its actual node count rather than
// panicking or silently mis-tagging the element type.
func TestWriteUnsupportedTetNodeCountErrors(t *testing.T) {
	m := Mesh{
		Nodes: [][3]float64{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}},
		Tets:  []Tet{{Body: 1, Nodes: []int{1, 2, 3}}},
	}
	err := Write(t.TempDir(), m)
	if err == nil {
		t.Fatal("Write(...) = nil error, want an unsupported-node-count error")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("error = %q, want it to mention the offending node count 3", err.Error())
	}
	if !strings.Contains(err.Error(), "1") {
		t.Errorf("error = %q, want it to mention the offending element index (1-based) 1", err.Error())
	}
}

// TestWriteUnsupportedBoundaryNodeCountErrors proves a boundary face with a node count other
// than 3 or 6 is rejected, naming the offending element index and node count.
func TestWriteUnsupportedBoundaryNodeCountErrors(t *testing.T) {
	m := Mesh{
		Nodes:    [][3]float64{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {0, 0, 1}},
		Boundary: []BoundaryFace{{Boundary: 1, Parent: 1, Nodes: []int{1, 2, 3, 4}}},
	}
	err := Write(t.TempDir(), m)
	if err == nil {
		t.Fatal("Write(...) = nil error, want an unsupported-node-count error")
	}
	if !strings.Contains(err.Error(), "4") {
		t.Errorf("error = %q, want it to mention the offending node count 4", err.Error())
	}
}

// TestFormatCoordPrecisionSafe proves formatCoord round-trips arbitrary float64 values exactly
// (shortest round-trip decimal) while still guaranteeing a decimal point on integer-valued
// results, matching the fixture's "0.0"/"1.0" style without losing precision on non-integers.
func TestFormatCoordPrecisionSafe(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0.0"},
		{1, "1.0"},
		{-1, "-1.0"},
		{0.5, "0.5"},
		{1.0 / 3.0, "0.3333333333333333"},
		{123456789.123456, "1.23456789123456e+08"},
	}
	for _, c := range cases {
		if got := formatCoord(c.in); got != c.want {
			t.Errorf("formatCoord(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
