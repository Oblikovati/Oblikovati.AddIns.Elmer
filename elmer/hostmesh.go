// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"fmt"

	"oblikovati.org/api/wire"
)

// Cloned from Oblikovati.AddIns.CalculiX's ccx/hostmesh.go (package ccx -> elmer only).
// The load-bearing change: coordinates are scaled to METRES (modelUnitM, host cm *0.01)
// rather than ccx's millimetres (modelUnitMM, host cm *10) — see units.go and
// task-10-report.md.

// facetTolerance is the chordal tessellation tolerance (host model units) requested for
// the surface pull. It also sets the scale at which the volume mesher approximates curved
// faces; a panel knob can override it later.
const facetTolerance = 0.05

// pullSurface fetches the body's triangulated surface from the host and welds it into a
// watertight indexed mesh in metres (host coordinates are in model units = cm). The
// welded surface is the input to the volume mesher.
func (e *Engine) pullSurface(bodyIndex int) (*SurfaceMesh, error) {
	facets, err := e.api.Body().CalculateFacets(wire.CalculateFacetsArgs{
		BodyIndex: bodyIndex,
		Tolerance: facetTolerance,
	})
	if err != nil {
		return nil, fmt.Errorf("calculate facets for body %d: %w", bodyIndex, err)
	}
	coords := scaleCoords(facets.VertexCoordinates, modelUnitM)
	surface, err := weldSurface(coords, facets.VertexIndices)
	if err != nil {
		return nil, err
	}
	if open := surface.openEdges(); open > 0 {
		return nil, fmt.Errorf("the body surface is not watertight (%d open/non-manifold edges); it cannot be meshed into a solid", open)
	}
	return surface, nil
}

// pullFaceFacets fetches the triangulation of a single B-rep face (by reference key) in
// metres, for matching against the volume mesh's boundary facets (face-group binding).
func (e *Engine) pullFaceFacets(bodyIndex int, faceKey string) (*SurfaceMesh, error) {
	facets, err := e.api.Body().FaceCalculateFacets(wire.FaceFacetsArgs{
		BodyIndex: bodyIndex,
		FaceKey:   faceKey,
		Tolerance: facetTolerance,
	})
	if err != nil {
		return nil, fmt.Errorf("calculate facets for face %s: %w", faceKey, err)
	}
	coords := scaleCoords(facets.VertexCoordinates, modelUnitM)
	return weldSurface(coords, facets.VertexIndices)
}

// scaleCoords returns a copy of a flat coordinate slice multiplied by factor.
func scaleCoords(coords []float64, factor float64) []float64 {
	out := make([]float64, len(coords))
	for i, c := range coords {
		out[i] = c * factor
	}
	return out
}
