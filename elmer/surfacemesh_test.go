// SPDX-License-Identifier: GPL-2.0-only

package elmer

import "testing"

// Cloned from Oblikovati.AddIns.CalculiX's ccx/volumemesh_test.go and ccx/prereq_test.go
// (package ccx -> elmer only). weldEpsilonFraction is scale-relative, so these
// assertions (vertex/triangle counts, open-edge detection) are unaffected by the cm->m
// rescale — boxSurface here uses metre-scale dimensions purely for readability.

func TestWeldCollapsesDuplicateCorners(t *testing.T) {
	coords, idx := boxSurface(0.10, 0.10, 0.10)
	s, err := weldSurface(coords, idx)
	if err != nil {
		t.Fatalf("weld: %v", err)
	}
	if len(s.Verts) != 8 {
		t.Errorf("welded vertex count = %d, want 8", len(s.Verts))
	}
	if len(s.Tris) != 12 {
		t.Errorf("triangle count = %d, want 12", len(s.Tris))
	}
}

func TestOpenEdgesDetectsHoles(t *testing.T) {
	coords, idx := boxSurface(0.10, 0.10, 0.10)
	box, err := weldSurface(coords, idx)
	if err != nil {
		t.Fatalf("weld: %v", err)
	}
	if open := box.openEdges(); open != 0 {
		t.Errorf("watertight box has %d open edges, want 0", open)
	}
	// Drop a triangle to punch a hole; its three edges become open.
	box.Tris = box.Tris[1:]
	if open := box.openEdges(); open == 0 {
		t.Error("box with a missing triangle should report open edges")
	}
}
