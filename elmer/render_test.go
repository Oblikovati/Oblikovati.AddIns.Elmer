// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"oblikovati.org/elmer/elmer/vtu"
)

// twoPointVTU writes a minimal 2-point VTU carrying both a "displacement" vector field
// (point 0: a 3-4-5 triangle vector, so its magnitude is exactly 5) and a "vonmises" scalar
// field (10, 20) — enough to exercise resultFieldFor's two branches independently of the
// full solve pipeline.
func twoPointVTU(t *testing.T, path string) *vtu.Result {
	t.Helper()
	content := `<?xml version="1.0"?>
<VTKFile type="UnstructuredGrid" version="0.1" byte_order="LittleEndian">
  <UnstructuredGrid>
    <Piece NumberOfPoints="2" NumberOfCells="0">
      <PointData>
        <DataArray type="Float64" Name="displacement" NumberOfComponents="3" format="ascii">
 3.0 4.0 0.0  0.0 0.0 0.0
        </DataArray>
        <DataArray type="Float64" Name="vonmises" NumberOfComponents="1" format="ascii">
 10.0 20.0
        </DataArray>
      </PointData>
      <Points>
        <DataArray type="Float64" NumberOfComponents="3" format="ascii">
 0.0 0.0 0.0  1.0 0.0 0.0
        </DataArray>
      </Points>
    </Piece>
  </UnstructuredGrid>
</VTKFile>`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := vtu.ReadFile(path)
	if err != nil {
		t.Fatalf("vtu.ReadFile: %v", err)
	}
	return res
}

// vtuWithPoints writes a minimal VTU carrying the given points (in file order) and a matching
// "vonmises" scalar field (same order) — lets a test choose the points' file order directly,
// to exercise pointIndexForNodes' geometry-based node/point correspondence check (finding 1,
// task-12 review) with permuted or alien point orders a real solve never hands twoPointVTU.
func vtuWithPoints(t *testing.T, path string, points [][3]float64, vonmises []float64) *vtu.Result {
	t.Helper()
	var pts, vals strings.Builder
	for _, p := range points {
		fmt.Fprintf(&pts, " %g %g %g ", p[0], p[1], p[2])
	}
	for _, v := range vonmises {
		fmt.Fprintf(&vals, " %g", v)
	}
	content := fmt.Sprintf(`<?xml version="1.0"?>
<VTKFile type="UnstructuredGrid" version="0.1" byte_order="LittleEndian">
  <UnstructuredGrid>
    <Piece NumberOfPoints="%d" NumberOfCells="0">
      <PointData>
        <DataArray type="Float64" Name="vonmises" NumberOfComponents="1" format="ascii">
%s
        </DataArray>
      </PointData>
      <Points>
        <DataArray type="Float64" NumberOfComponents="3" format="ascii">
%s
        </DataArray>
      </Points>
    </Piece>
  </UnstructuredGrid>
</VTKFile>`, len(points), vals.String(), pts.String())
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := vtu.ReadFile(path)
	if err != nil {
		t.Fatalf("vtu.ReadFile: %v", err)
	}
	return res
}

// TestSurfaceRenderDataRemapsPermutedVTUPoints proves the geometry-based node/point
// correspondence check: when a VTU's points arrive in a PERMUTATION of the mesh's compact
// ascending-id node order — ElmerSolver's InvNodePerm renumbering path, which "Optimize
// Bandwidth = True" (this add-in's deck setting) can engage — surfaceRenderData must still
// paint each node's OWN value, recovered by matching coordinates, not the value that happens
// to sit at its file position. A silent positional read here would still produce a
// plausible-looking flood plot (same value range, same shape) with the values on the wrong
// nodes — the worst kind of bug because peak-based oracles would still pass.
func TestSurfaceRenderDataRemapsPermutedVTUPoints(t *testing.T) {
	mesh := &TetMesh{
		Nodes: []Node{{ID: 1, X: 0, Y: 0, Z: 0}, {ID: 2, X: 1, Y: 0, Z: 0}, {ID: 3, X: 0, Y: 1, Z: 0}},
		Surface: []BoundaryFacet{
			{Nodes: []int{1, 2, 3}, Corners: [3]int{1, 2, 3}, Face: 1},
		},
	}
	// File order is node2, node3, node1 (a permutation of the mesh's ascending-id order),
	// each point carrying its OWN node's vonmises value (node1=10, node2=20, node3=30).
	res := vtuWithPoints(t, filepath.Join(t.TempDir(), "permuted.vtu"),
		[][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 0}},
		[]float64{20, 30, 10},
	)
	values, _, _, err := resultFieldFor(res, resultFieldVonMises)
	if err != nil {
		t.Fatalf("resultFieldFor: %v", err)
	}
	_, _, scalars, err := surfaceRenderData(mesh, res, values)
	if err != nil {
		t.Fatalf("surfaceRenderData: %v", err)
	}
	want := []float64{10, 20, 30} // node1, node2, node3 in first-seen (Corners) order
	if len(scalars) != 3 || scalars[0] != want[0] || scalars[1] != want[1] || scalars[2] != want[2] {
		t.Errorf("scalars = %v, want %v (each node's own value, not its file position's)", scalars, want)
	}
}

// TestSurfaceRenderDataErrorsOnAlienVTUPoint proves the correspondence check is honest about
// failure: a VTU point that matches no mesh node within tolerance (a mesh/VTU pair that
// drifted out of sync) must error naming the unmatched node, never silently default to 0 or
// misattribute another node's value.
func TestSurfaceRenderDataErrorsOnAlienVTUPoint(t *testing.T) {
	mesh := &TetMesh{
		Nodes: []Node{{ID: 1, X: 0, Y: 0, Z: 0}, {ID: 2, X: 1, Y: 0, Z: 0}},
		Surface: []BoundaryFacet{
			{Nodes: []int{1, 2}, Corners: [3]int{1, 2, 1}, Face: 1},
		},
	}
	res := vtuWithPoints(t, filepath.Join(t.TempDir(), "alien.vtu"),
		[][3]float64{{0, 0, 0}, {5, 5, 5}}, // second point matches no mesh node
		[]float64{10, 99},
	)
	values, _, _, err := resultFieldFor(res, resultFieldVonMises)
	if err != nil {
		t.Fatalf("resultFieldFor: %v", err)
	}
	if _, _, _, err := surfaceRenderData(mesh, res, values); err == nil {
		t.Fatal("surfaceRenderData: want an error when a mesh node has no matching VTU point")
	}
}

// TestResultFieldForVonMises pins the default (any kind != displacement) branch: the raw
// scalar "vonmises" DataArray, labelled/unitted for the status report.
func TestResultFieldForVonMises(t *testing.T) {
	res := twoPointVTU(t, filepath.Join(t.TempDir(), "case.vtu"))
	vals, label, unit, err := resultFieldFor(res, resultFieldVonMises)
	if err != nil {
		t.Fatalf("resultFieldFor: %v", err)
	}
	if label != "von Mises stress" || unit != "Pa" {
		t.Errorf("label/unit = %q/%q, want \"von Mises stress\"/\"Pa\"", label, unit)
	}
	if len(vals) != 2 || vals[0] != 10 || vals[1] != 20 {
		t.Errorf("vals = %v, want [10 20]", vals)
	}
}

// TestResultFieldForDisplacementComputesMagnitude pins the displacement branch: the
// 3-component vector field is reshaped into a per-point magnitude (3-4-5 -> 5).
func TestResultFieldForDisplacementComputesMagnitude(t *testing.T) {
	res := twoPointVTU(t, filepath.Join(t.TempDir(), "case.vtu"))
	vals, label, unit, err := resultFieldFor(res, resultFieldDisplacement)
	if err != nil {
		t.Fatalf("resultFieldFor: %v", err)
	}
	if label != "displacement" || unit != "m" {
		t.Errorf("label/unit = %q/%q, want \"displacement\"/\"m\"", label, unit)
	}
	if len(vals) != 2 || vals[0] != 5 || vals[1] != 0 {
		t.Errorf("vals = %v, want [5 0] (sqrt(3^2+4^2)=5)", vals)
	}
}

// TestResultFieldForErrorsWhenFieldMissing pins the "the VTU doesn't carry this field"
// guard for both kinds, so a mismatched deck/VTU pair fails cleanly instead of painting a
// zero-valued field.
func TestResultFieldForErrorsWhenFieldMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.vtu")
	content := `<?xml version="1.0"?>
<VTKFile type="UnstructuredGrid" version="0.1" byte_order="LittleEndian">
  <UnstructuredGrid>
    <Piece NumberOfPoints="1" NumberOfCells="0">
      <PointData></PointData>
      <Points>
        <DataArray type="Float64" NumberOfComponents="3" format="ascii">
 0.0 0.0 0.0
        </DataArray>
      </Points>
    </Piece>
  </UnstructuredGrid>
</VTKFile>`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := vtu.ReadFile(path)
	if err != nil {
		t.Fatalf("vtu.ReadFile: %v", err)
	}
	if _, _, _, err := resultFieldFor(res, resultFieldVonMises); err == nil {
		t.Fatal("resultFieldFor(vonmises): want an error when the field is absent")
	}
	if _, _, _, err := resultFieldFor(res, resultFieldDisplacement); err == nil {
		t.Fatal("resultFieldFor(displacement): want an error when the field is absent")
	}
}

// TestMinMaxSliceEmptyAndNonEmpty pins minMaxSlice's empty-slice default and its normal
// min/max scan.
func TestMinMaxSliceEmptyAndNonEmpty(t *testing.T) {
	if lo, hi := minMaxSlice(nil); lo != 0 || hi != 0 {
		t.Errorf("minMaxSlice(nil) = %v,%v, want 0,0", lo, hi)
	}
	lo, hi := minMaxSlice([]float64{3, 1, 4, 1, 5})
	if lo != 1 || hi != 5 {
		t.Errorf("minMaxSlice([3 1 4 1 5]) = %v,%v, want 1,5", lo, hi)
	}
}

// TestRampMapperWidensDegenerateRange pins the "hi <= lo" guard: a zero-span field still
// produces a valid, strictly increasing color ramp.
func TestRampMapperWidensDegenerateRange(t *testing.T) {
	m := rampMapper(5, 5)
	if len(m.Values) == 0 {
		t.Fatal("rampMapper: no values")
	}
	if m.Values[len(m.Values)-1] <= m.Values[0] {
		t.Errorf("rampMapper(5,5) values not widened into a valid range: %v", m.Values)
	}
}

// TestSurfaceRenderDataErrorsWhenValuesTooShort pins the "the VTU and the mesh drifted out of
// sync" guard as reached through surfaceRenderData: a points/values slice shorter than the
// mesh's own node count cannot cover every node, and must error rather than panic or silently
// read past the slice.
func TestSurfaceRenderDataErrorsWhenValuesTooShort(t *testing.T) {
	mesh := &TetMesh{
		Nodes: []Node{{ID: 1, X: 0, Y: 0, Z: 0}, {ID: 2, X: 1, Y: 0, Z: 0}, {ID: 3, X: 0, Y: 1, Z: 0}},
		Surface: []BoundaryFacet{
			{Nodes: []int{1, 2, 3}, Corners: [3]int{1, 2, 3}, Face: 1},
		},
	}
	res := &vtu.Result{Points: [][3]float64{{0, 0, 0}}}
	if _, _, _, err := surfaceRenderData(mesh, res, []float64{42}); err == nil {
		t.Fatal("surfaceRenderData: want an error when values is shorter than the mesh's node count")
	}
}
