// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"strings"
	"testing"

	"oblikovati.org/api/wire"
)

// facegroups.go has no equivalent unit test in Oblikovati.AddIns.CalculiX's ccx (there it
// is only exercised through the full requireSolver-gated host-study integration tests).
// Its pure helpers (matchFace, groupBoundaryByFace) are cheap to test synthetically, so
// this file adds that coverage rather than leaving it untested until Task 12's
// full-pipeline tests arrive.

// twoFaceBoxMesh returns a TetMesh whose Surface holds two boundary facets on distinct
// gmsh surface groups: a bottom face (z=0, normal -Z) tagged group 1, and a top face
// (z=1, normal +Z) tagged group 2 — enough to exercise groupBoundaryByFace + matchFace's
// normal/centroid matching without needing a real gmsh run.
func twoFaceBoxMesh() *TetMesh {
	return &TetMesh{
		Nodes: []Node{
			{ID: 1, X: 0, Y: 0, Z: 0}, {ID: 2, X: 1, Y: 0, Z: 0}, {ID: 3, X: 0, Y: 1, Z: 0},
			{ID: 4, X: 0, Y: 0, Z: 1}, {ID: 5, X: 1, Y: 0, Z: 1}, {ID: 6, X: 0, Y: 1, Z: 1},
		},
		Surface: []BoundaryFacet{
			// bottom (z=0): winding 1,3,2 gives normal (0,0,-1)
			{Nodes: []int{1, 3, 2}, Corners: [3]int{1, 3, 2}, Face: 1},
			// top (z=1): winding 4,5,6 gives normal (0,0,+1)
			{Nodes: []int{4, 5, 6}, Corners: [3]int{4, 5, 6}, Face: 2},
		},
	}
}

func TestGroupBoundaryByFaceSeparatesGmshGroups(t *testing.T) {
	groups := groupBoundaryByFace(twoFaceBoxMesh())
	if len(groups) != 2 {
		t.Fatalf("group count = %d, want 2 (one per gmsh surface tag)", len(groups))
	}
	for _, tag := range []int{1, 2} {
		agg, ok := groups[tag]
		if !ok {
			t.Fatalf("missing group for gmsh tag %d", tag)
		}
		if agg.count != 1 {
			t.Errorf("group %d facet count = %d, want 1", tag, agg.count)
		}
	}
}

func TestMatchFacePicksAlignedNearestGroup(t *testing.T) {
	groups := groupBoundaryByFace(twoFaceBoxMesh())

	// The host's "bottom" face tessellation: centroid near (1/3,1/3,0), normal -Z.
	match := matchFace(groups, [3]float64{0.33, 0.33, 0}, [3]float64{0, 0, -1})
	if match == nil {
		t.Fatal("matchFace: no group matched a face with an aligned normal")
	}
	if match.count != 1 || match.centroid()[2] != 0 {
		t.Errorf("matched group centroid = %v, want the z=0 group", match.centroid())
	}
}

func TestMatchFaceRejectsMisalignedNormal(t *testing.T) {
	groups := groupBoundaryByFace(twoFaceBoxMesh())

	// A face pointing sideways (+X) aligns with neither the top nor the bottom group.
	if match := matchFace(groups, [3]float64{0.5, 0.5, 0.5}, [3]float64{1, 0, 0}); match != nil {
		t.Errorf("matchFace matched a group for a misaligned (+X) normal: %+v", match)
	}
}

// unitTetSurfaceMesh returns a single linear tet (same corners as unitTetMesh) whose
// Surface lists its P1 face (nodes 1,2,3, the z=0 face) under gmsh tag 7 — a mesh shaped
// like what a real gmsh run + parseMSH would hand buildFaceGroups.
func unitTetSurfaceMesh() *TetMesh {
	mesh, _ := unitTetMesh()
	mesh.Surface = []BoundaryFacet{
		{Nodes: []int{1, 2, 3}, Corners: [3]int{1, 2, 3}, Face: 7},
	}
	return mesh
}

// TestBuildFaceGroupsBindsHostFaceAcrossBodyRetries drives buildFaceGroups end to end
// through a fake two-body host: body 0 doesn't have the picked face (FaceCalculateFacets
// errors), body 1 does — exercising pullFaceOnAnyBody's per-body retry, and, through
// buildFaceGroups itself, faceElemIndex/resolveElemFaces/sortedTriple (element-face
// resolution) and faceAgg.nodeList/surfaceCentroidNormal (host/mesh face matching).
func TestBuildFaceGroupsBindsHostFaceAcrossBodyRetries(t *testing.T) {
	host := &meshHost{
		wantBody: 1,
		faces: map[string][]float64{
			// A flat horizontal cm-scale triangle: its normal is +-Z regardless of
			// winding, aligning with the mesh's z=0 P1 face (also +-Z).
			"bottomFace": {0, 0, 0, 10, 0, 0, 0, 10, 0},
		},
	}
	e := NewEngine(host)
	solids := []wire.BodyInfo{{Index: 0, Name: "Solid0"}, {Index: 1, Name: "Solid1"}}

	groups, err := e.buildFaceGroups([]string{"bottomFace"}, unitTetSurfaceMesh(), solids)
	if err != nil {
		t.Fatalf("buildFaceGroups: %v", err)
	}

	wantElemFaces := []ElemFace{{Elem: 1, Face: 1}}
	if got := groups.ElemFaces["bottomFace"]; len(got) != 1 || got[0] != wantElemFaces[0] {
		t.Errorf("ElemFaces[bottomFace] = %+v, want %+v", got, wantElemFaces)
	}
	if got := groups.Nodes["bottomFace"]; len(got) != 3 {
		t.Errorf("Nodes[bottomFace] = %v, want the 3 nodes of the tet's P1 face", got)
	}
	if n := groups.Normals["bottomFace"]; n[2] != 1 && n[2] != -1 {
		t.Errorf("Normals[bottomFace] = %v, want a unit +-Z normal", n)
	}
}

func TestBuildFaceGroupsErrorsWhenNoMeshGroupMatches(t *testing.T) {
	host := &meshHost{wantBody: 0, faces: map[string][]float64{
		// A vertical face (+X normal): does not align with the mesh's only (z=0) group.
		"sideFace": {0, 0, 0, 0, 10, 0, 0, 0, 10},
	}}
	e := NewEngine(host)
	solids := []wire.BodyInfo{{Index: 0, Name: "Solid0"}}

	_, err := e.buildFaceGroups([]string{"sideFace"}, unitTetSurfaceMesh(), solids)
	if err == nil {
		t.Fatal("buildFaceGroups: expected an error when no mesh group matches")
	}
	if !strings.Contains(err.Error(), "sideFace") {
		t.Errorf("error %q does not mention the offending face key", err)
	}
}

func TestPullFaceOnAnyBodyErrorsWhenNoBodyHasTheFace(t *testing.T) {
	e := NewEngine(&meshHost{wantBody: 99}) // no body index ever matches
	solids := []wire.BodyInfo{{Index: 0}, {Index: 1}}

	_, err := e.pullFaceOnAnyBody("ghostFace", solids)
	if err == nil {
		t.Fatal("pullFaceOnAnyBody: expected an error when no body resolves the face")
	}
	if !strings.Contains(err.Error(), "ghostFace") || !strings.Contains(err.Error(), "2 solid bodies") {
		t.Errorf("error %q should name the face key and the body count searched", err)
	}
}
