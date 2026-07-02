// SPDX-License-Identifier: GPL-2.0-only

package elmer

import "sort"

// Cloned verbatim from Oblikovati.AddIns.CalculiX's ccx/elementfaces.go (package ccx ->
// elmer only). ElemFace is part of the FaceGroups contract Tasks 11-13 build on.

// ElemFace identifies one face of a tetrahedral element by the CalculiX/Elmer face
// number (1..4, the same P1..P4 convention both toolchains inherit from Abaqus).
type ElemFace struct {
	Elem int
	Face int
}

// tetFaceCorners lists the corner indices (into a tet element's first four nodes) for each
// face Pn, in the Abaqus/CalculiX/Elmer C3D4/C3D10 convention:
//
//	P1 = 1-2-3, P2 = 1-4-2, P3 = 2-4-3, P4 = 3-4-1   (1-based corners)
var tetFaceCorners = [4][3]int{
	{0, 1, 2}, // P1
	{0, 3, 1}, // P2
	{1, 3, 2}, // P3
	{2, 3, 0}, // P4
}

// faceElemIndex maps the sorted corner-node triple of every tetrahedron face to its
// element-face. A surface triangle's three corner nodes resolve to the single tet face it
// lies on, which a pressure load or boundary condition addresses as (element, face number).
func faceElemIndex(mesh *TetMesh) map[[3]int]ElemFace {
	index := make(map[[3]int]ElemFace, len(mesh.Elements)*4)
	for _, el := range mesh.Elements {
		if len(el.Nodes) < 4 {
			continue
		}
		for f, corners := range tetFaceCorners {
			key := sortedTriple(el.Nodes[corners[0]], el.Nodes[corners[1]], el.Nodes[corners[2]])
			index[key] = ElemFace{Elem: el.ID, Face: f + 1}
		}
	}
	return index
}

// resolveElemFaces maps each boundary-facet corner triple to its element-face, dropping any
// triple with no matching tet face (should not happen for a conformal mesh).
func resolveElemFaces(facets [][3]int, index map[[3]int]ElemFace) []ElemFace {
	out := make([]ElemFace, 0, len(facets))
	for _, corners := range facets {
		if ef, ok := index[sortedTriple(corners[0], corners[1], corners[2])]; ok {
			out = append(out, ef)
		}
	}
	return out
}

// sortedTriple returns the three node ids in ascending order, the order-independent key
// for a triangular face.
func sortedTriple(a, b, c int) [3]int {
	t := [3]int{a, b, c}
	sort.Ints(t[:])
	return t
}
