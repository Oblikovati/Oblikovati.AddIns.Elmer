// SPDX-License-Identifier: GPL-2.0-only

package elmer

import "testing"

// TestMergeTetMeshesTagsBodies: merging two single-tet meshes offsets the second body's node
// and element ids, tags each element with its source body, and offsets the gmsh surface tag
// so the two bodies' face groups stay distinct. Cloned/adapted from
// Oblikovati.AddIns.CalculiX's ccx/multimaterial_test.go's TestMergeTetMeshesTagsBodies
// (finding 2, task-12 review: mergeTetMeshes' offset arithmetic had no direct test of its
// own — this pins nodeOff/elemOff/faceOff accumulation across two source meshes).
func TestMergeTetMeshesTagsBodies(t *testing.T) {
	a := &TetMesh{
		Nodes:    []Node{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}},
		Elements: []TetElement{{ID: 1, Nodes: []int{1, 2, 3, 4}}},
		Surface:  []BoundaryFacet{{Nodes: []int{1, 2, 3}, Corners: [3]int{1, 2, 3}, Face: 1}},
	}
	b := &TetMesh{
		Nodes:    []Node{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}},
		Elements: []TetElement{{ID: 1, Nodes: []int{1, 2, 3, 4}}},
		Surface:  []BoundaryFacet{{Nodes: []int{2, 3, 4}, Corners: [3]int{2, 3, 4}, Face: 1}},
	}
	m := mergeTetMeshes([]*TetMesh{a, b})

	if len(m.Nodes) != 8 || len(m.Elements) != 2 {
		t.Fatalf("merged sizes = %d nodes, %d elems; want 8, 2", len(m.Nodes), len(m.Elements))
	}
	if m.Elements[0].Body != 0 || m.Elements[1].Body != 1 {
		t.Errorf("body tags = (%d, %d), want (0, 1)", m.Elements[0].Body, m.Elements[1].Body)
	}
	if m.Elements[1].ID != 2 || m.Elements[1].Nodes[0] != 5 {
		t.Errorf("body-1 element id/nodes not offset: id=%d nodes=%v", m.Elements[1].ID, m.Elements[1].Nodes)
	}
	if m.Surface[1].Face == m.Surface[0].Face {
		t.Errorf("surface tags collided across bodies: both %d", m.Surface[0].Face)
	}
	if m.Surface[1].Corners[0] != 6 {
		t.Errorf("body-1 facet corner not offset: %d, want 6", m.Surface[1].Corners[0])
	}
}
