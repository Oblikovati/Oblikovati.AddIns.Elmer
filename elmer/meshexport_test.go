// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"strings"
	"testing"
)

// unitTetMesh returns a single linear tet (nodes 1-4 at the unit-tet corners, element id
// 1, Body 0) plus a matching one-facet FaceGroups binding "faceA" to element 1's face 1
// (corners 1,2,3 per tetFaceCorners[0]) — the minimal fixture exportMesh needs.
func unitTetMesh() (*TetMesh, *FaceGroups) {
	mesh := &TetMesh{
		Nodes: []Node{
			{ID: 1, X: 0, Y: 0, Z: 0},
			{ID: 2, X: 1, Y: 0, Z: 0},
			{ID: 3, X: 0, Y: 1, Z: 0},
			{ID: 4, X: 0, Y: 0, Z: 1},
		},
		Elements: []TetElement{
			{ID: 1, Nodes: []int{1, 2, 3, 4}, Body: 0},
		},
	}
	groups := &FaceGroups{
		Nodes:     map[string][]int{"faceA": {1, 2, 3}},
		ElemFaces: map[string][]ElemFace{"faceA": {{Elem: 1, Face: 1}}},
		Normals:   map[string][3]float64{"faceA": {0, 0, -1}},
	}
	return mesh, groups
}

func TestExportMeshBuildsMeshfmtMesh(t *testing.T) {
	mesh, groups := unitTetMesh()
	bound := []BoundFace{{Key: "faceA"}}

	out, boundaryIDs, err := exportMesh(mesh, groups, bound)
	if err != nil {
		t.Fatalf("exportMesh: %v", err)
	}

	if len(out.Nodes) != 4 {
		t.Errorf("node count = %d, want 4", len(out.Nodes))
	}
	if len(out.Tets) != 1 {
		t.Fatalf("tet count = %d, want 1", len(out.Tets))
	}
	if out.Tets[0].Body != 1 {
		t.Errorf("tet body = %d, want 1 (TetElement.Body 0 -> Elmer's 1-based body id)", out.Tets[0].Body)
	}
	if got := len(out.Tets[0].Nodes); got != 4 {
		t.Errorf("tet node count = %d, want 4", got)
	}

	if want := (map[string]int{"faceA": 1}); boundaryIDs["faceA"] != want["faceA"] {
		t.Errorf("boundaryIDs = %+v, want %+v", boundaryIDs, want)
	}

	if len(out.Boundary) != 1 {
		t.Fatalf("boundary face count = %d, want 1", len(out.Boundary))
	}
	bf := out.Boundary[0]
	if bf.Boundary != 1 {
		t.Errorf("boundary id = %d, want 1", bf.Boundary)
	}
	if bf.Parent != 1 {
		t.Errorf("boundary parent = %d, want 1 (the exported tet index)", bf.Parent)
	}
	if len(bf.Nodes) != 3 {
		t.Errorf("boundary face node count = %d, want 3 (linear tet face)", len(bf.Nodes))
	}
}

func TestExportMeshErrorsOnUnboundFaceKey(t *testing.T) {
	mesh, groups := unitTetMesh()
	bound := []BoundFace{{Key: "missingFace"}}

	_, _, err := exportMesh(mesh, groups, bound)
	if err == nil {
		t.Fatal("exportMesh: expected an error for a face key absent from FaceGroups, got nil")
	}
	if !strings.Contains(err.Error(), "missingFace") {
		t.Errorf("error %q does not mention the offending key %q", err, "missingFace")
	}
}

// TestExportMeshQuadraticBoundaryFace exercises the C3D10/Elmer face-to-midside-node
// mapping (tetFaceMidsides): a quadratic tet's face 1 boundary triangle must carry its 3
// corners plus the 3 edge midpoints in Elmer's (a,b) (b,c) (c,a) order, matching
// elmer/meshfmt's order2Tet10 golden fixture (nodes {1,2,3,5,6,7} for that same face).
func TestExportMeshQuadraticBoundaryFace(t *testing.T) {
	mesh := &TetMesh{
		Nodes: make([]Node, 10), // coordinates are irrelevant to this assertion
		Elements: []TetElement{
			{ID: 1, Nodes: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, Body: 0},
		},
	}
	for i := range mesh.Nodes {
		mesh.Nodes[i] = Node{ID: i + 1}
	}
	groups := &FaceGroups{
		ElemFaces: map[string][]ElemFace{"faceA": {{Elem: 1, Face: 1}}},
	}

	out, _, err := exportMesh(mesh, groups, []BoundFace{{Key: "faceA"}})
	if err != nil {
		t.Fatalf("exportMesh: %v", err)
	}
	want := []int{1, 2, 3, 5, 6, 7}
	got := out.Boundary[0].Nodes
	if len(got) != len(want) {
		t.Fatalf("boundary nodes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("boundary nodes = %v, want %v", got, want)
			break
		}
	}
}
