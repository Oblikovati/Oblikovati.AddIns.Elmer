// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"os"
	"path/filepath"
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

// TestSurfaceRenderDataErrorsWhenValuesTooShort pins pointValue's out-of-range guard as
// reached through surfaceRenderData: a values slice shorter than the mesh's own node count
// means the mesh and the VTU drifted out of sync, and must error rather than panic or
// silently read past the slice.
func TestSurfaceRenderDataErrorsWhenValuesTooShort(t *testing.T) {
	mesh := &TetMesh{
		Nodes: []Node{{ID: 1, X: 0, Y: 0, Z: 0}, {ID: 2, X: 1, Y: 0, Z: 0}, {ID: 3, X: 0, Y: 1, Z: 0}},
		Surface: []BoundaryFacet{
			{Nodes: []int{1, 2, 3}, Corners: [3]int{1, 2, 3}, Face: 1},
		},
	}
	if _, _, _, err := surfaceRenderData(mesh, []float64{42}); err == nil {
		t.Fatal("surfaceRenderData: want an error when values is shorter than the mesh's node count")
	}
}
