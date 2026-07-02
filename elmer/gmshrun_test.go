// SPDX-License-Identifier: GPL-2.0-only

package elmer

import "testing"

// Cloned from Oblikovati.AddIns.CalculiX's ccx/volumemesh_test.go (package ccx -> elmer
// only), with the scale-dependent assertions unaffected — boxSurface builds coordinates
// directly in the mesh's own units (metres here, mm in ccx), so TestGmshMeshesBoxIntoTets
// needs no numeric adjustment, only requireGmsh replacing ccx's requireSolver (this task
// clones the mesh pipeline only; the ElmerSolver equivalent of requireSolver arrives with
// Task 11+'s deck assembly).

// requireGmsh resolves the vendored gmsh binary, skipping the test when it hasn't been
// built locally (mirrors elmer/meshfmt/elmergrid_test.go's requireElmerGrid pattern; CI's
// solvers job always has it built).
func requireGmsh(t *testing.T) string {
	t.Helper()
	bin, err := resolveGmshBin()
	if err != nil {
		t.Skipf("gmsh not available: %v", err)
	}
	return bin
}

// boxSurface returns a raw triangle soup for an sx×sy×sz box with each face's vertices
// listed independently (24 vertices, 12 triangles) — exercising the weld, which must
// collapse the 24 shared-corner vertices down to 8.
func boxSurface(sx, sy, sz float64) ([]float64, []int) {
	v := [8][3]float64{
		{0, 0, 0}, {sx, 0, 0}, {sx, sy, 0}, {0, sy, 0},
		{0, 0, sz}, {sx, 0, sz}, {sx, sy, sz}, {0, sy, sz},
	}
	quads := [6][4]int{{0, 3, 2, 1}, {4, 5, 6, 7}, {0, 1, 5, 4}, {1, 2, 6, 5}, {2, 3, 7, 6}, {3, 0, 4, 7}}
	var coords []float64
	var idx []int
	for _, q := range quads {
		base := len(coords) / 3
		for _, c := range q {
			coords = append(coords, v[c][0], v[c][1], v[c][2])
		}
		idx = append(idx, base, base+1, base+2, base, base+2, base+3)
	}
	return coords, idx
}

func TestGmshMeshesBoxIntoTets(t *testing.T) {
	bin := requireGmsh(t)
	coords, idx := boxSurface(0.10, 0.10, 0.10) // a 100 mm box in metres
	surface, err := weldSurface(coords, idx)
	if err != nil {
		t.Fatalf("weld: %v", err)
	}
	mesh, err := NewGmshMesher(bin).Mesh(surface, MeshOptions{SizeM: 0.04, Order: 2}, t.TempDir())
	if err != nil {
		t.Fatalf("mesh: %v", err)
	}
	if len(mesh.Elements) == 0 {
		t.Fatal("no tetrahedra produced")
	}
	for i, e := range mesh.Elements {
		if !e.IsQuadratic() {
			t.Fatalf("element %d has %d nodes, want 10 (quadratic)", i, len(e.Nodes))
		}
	}
	if len(mesh.Surface) == 0 {
		t.Error("no boundary facets captured (needed for load/BC binding)")
	}
}

func TestResolveGmshBinFindsVendoredBinary(t *testing.T) {
	// This is a light smoke test, not a hard requirement: it just proves
	// resolveGmshBin's wiring (env -> gmshDefaultPath -> LookPath) compiles and runs
	// end to end; requireGmsh above is what other tests should call to skip cleanly.
	if _, err := resolveGmshBin(); err != nil {
		t.Skipf("gmsh not available: %v", err)
	}
}
