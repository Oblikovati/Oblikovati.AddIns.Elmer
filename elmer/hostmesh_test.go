// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"oblikovati.org/api/wire"
)

// meshHost is a minimal fake HostCaller serving one solid body's surface facets and named
// face facets, in host model units (cm) — enough to drive pullSurface/pullFaceFacets
// (hostmesh.go) and buildFaceGroups/pullFaceOnAnyBody (facegroups.go) without a live host.
// Mirrors Oblikovati.AddIns.CalculiX's ccx/hoststudy_test.go boxHost, scoped down to what
// this task's pure mesh-plumbing methods need (no selection decoding, no graphics calls).
type meshHost struct {
	faces    map[string][]float64 // faceKey -> flat xyz triangle-soup coordinates (host cm)
	wantBody int                  // FaceCalculateFacets only succeeds when args.BodyIndex == wantBody
}

// boxFacetsCM is a unit-cube-ish box's whole-surface triangle soup in host cm, reused as
// pullSurface's body facets response.
func boxFacetsCM() ([]float64, []int) {
	v := [8][3]float64{
		{0, 0, 0}, {10, 0, 0}, {10, 10, 0}, {0, 10, 0},
		{0, 0, 10}, {10, 0, 10}, {10, 10, 10}, {0, 10, 10},
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

func (h *meshHost) Call(method string, req []byte) ([]byte, error) {
	switch method {
	case wire.MethodBodyCalculateFacets:
		coords, idx := boxFacetsCM()
		return json.Marshal(wire.FacetSetResult{VertexCoordinates: coords, VertexIndices: idx})
	case wire.MethodFaceCalculateFacets:
		return h.faceFacets(req)
	default:
		return []byte("{}"), nil
	}
}

// faceFacets returns a triangle for the requested face key, mimicking the real
// FaceCalculateFacets handler being body-scoped: it errors unless args.BodyIndex matches
// h.wantBody, so pullFaceOnAnyBody's per-body retry loop has something real to retry past.
func (h *meshHost) faceFacets(req []byte) ([]byte, error) {
	var args wire.FaceFacetsArgs
	if err := json.Unmarshal(req, &args); err != nil {
		return nil, err
	}
	if args.BodyIndex != h.wantBody {
		return nil, errFaceNotOnBody
	}
	coords, ok := h.faces[args.FaceKey]
	if !ok {
		return json.Marshal(wire.FacetSetResult{}) // empty triangulation: weld yields 0 tris, matchFace finds nothing
	}
	return json.Marshal(wire.FacetSetResult{VertexCoordinates: coords, VertexIndices: []int{0, 1, 2}})
}

var errFaceNotOnBody = fmt.Errorf("face not found on this body")

func TestPullSurfaceScalesHostCmToMetresAndWelds(t *testing.T) {
	e := NewEngine(&meshHost{})
	surface, err := e.pullSurface(0)
	if err != nil {
		t.Fatalf("pullSurface: %v", err)
	}
	if len(surface.Verts) != 8 {
		t.Fatalf("welded vertex count = %d, want 8", len(surface.Verts))
	}
	// The host box spans 0..10 cm; scaled by modelUnitM (0.01) it must span 0..0.1 m.
	var maxCoord float64
	for _, v := range surface.Verts {
		for _, c := range v {
			if c > maxCoord {
				maxCoord = c
			}
		}
	}
	if want := 0.10; maxCoord < want-1e-9 || maxCoord > want+1e-9 {
		t.Errorf("max welded coordinate = %v, want %v (10 cm host -> 0.1 m)", maxCoord, want)
	}
	if open := surface.openEdges(); open != 0 {
		t.Errorf("pulled box surface has %d open edges, want a watertight 0", open)
	}
}

func TestPullSurfaceErrorsWhenSurfaceIsNotWatertight(t *testing.T) {
	e := NewEngine(&openBoxHost{})
	_, err := e.pullSurface(0)
	if err == nil {
		t.Fatal("pullSurface: expected an error for a non-watertight surface")
	}
	if !strings.Contains(err.Error(), "watertight") {
		t.Errorf("error %q does not mention watertightness", err)
	}
}

// openBoxHost serves only 5 of a box's 6 faces (one triangle pair dropped), producing a
// hole pullSurface must reject.
type openBoxHost struct{}

func (openBoxHost) Call(method string, _ []byte) ([]byte, error) {
	if method != wire.MethodBodyCalculateFacets {
		return []byte("{}"), nil
	}
	coords, idx := boxFacetsCM()
	// Drop the last quad's two triangles (12 indices = the last 12 entries).
	return json.Marshal(wire.FacetSetResult{VertexCoordinates: coords, VertexIndices: idx[:len(idx)-6]})
}

func TestPullFaceFacetsScalesHostCmToMetres(t *testing.T) {
	e := NewEngine(&meshHost{faces: map[string][]float64{
		"faceA": {0, 0, 0, 10, 0, 0, 0, 10, 0},
	}})
	surface, err := e.pullFaceFacets(0, "faceA")
	if err != nil {
		t.Fatalf("pullFaceFacets: %v", err)
	}
	if len(surface.Verts) != 3 {
		t.Fatalf("vertex count = %d, want 3", len(surface.Verts))
	}
	if got := surface.Verts[1][0]; got < 0.10-1e-9 || got > 0.10+1e-9 {
		t.Errorf("vertex[1].x = %v, want 0.10 (10 cm host -> 0.1 m)", got)
	}
}
