// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"fmt"
	"sort"

	"oblikovati.org/elmer/elmer/meshfmt"
)

// meshexport.go is NEW for this add-in (no ccx equivalent): CalculiX decks address a
// boundary condition by symbolic (element, face) pairs and let the solver resolve node
// lists internally, but Elmer's native mesh-database format (meshfmt.Mesh) is node-based
// — mesh.boundary needs each boundary face's explicit node list. This file is the
// adapter between the cloned TetMesh/FaceGroups pipeline and meshfmt.Mesh.

// BoundFace is one resolved constraint face: the host FaceKey it was picked from. Task 11
// extends this (or wraps it) with the condition Kind (Dirichlet/Neumann/...); exportMesh
// only needs the key to assign a stable, insertion-ordered boundary id.
type BoundFace struct {
	Key string
}

// tetFaceMidsides lists, for a quadratic (10-node) tet, the mid-edge node indices (into
// the element's Nodes slice, 0-based) between each consecutive pair of tetFaceCorners'
// face corners. The C3D10/Elmer edge order is (1,2),(2,3),(3,1),(1,4),(2,4),(3,4) at
// slice positions 4-9 (see meshfmt.Tet's doc comment; gmshTet10ToElmerOrder in
// mshparse.go is what puts a tet's Nodes in this order in the first place). Each row is
// derived from tetFaceCorners' corner triple by pairing consecutive corners; e.g. P1's
// corners are 1,2,3 so its midsides are mid(1,2), mid(2,3), mid(3,1) = slice indices 4,5,6
// — cross-checked against elmer/meshfmt's order2Tet10 golden fixture (face nodes
// {1,2,3,5,6,7}), see meshexport_test.go's TestExportMeshQuadraticBoundaryFace.
var tetFaceMidsides = [4][3]int{
	{4, 5, 6}, // P1 (corners 1,2,3): mid(1,2) mid(2,3) mid(3,1)
	{7, 8, 4}, // P2 (corners 1,4,2): mid(1,4) mid(4,2) mid(2,1)
	{8, 9, 5}, // P3 (corners 2,4,3): mid(2,4) mid(4,3) mid(3,2)
	{9, 7, 6}, // P4 (corners 3,4,1): mid(3,4) mid(4,1) mid(1,3)
}

// exportMesh converts an already-built TetMesh + FaceGroups into the meshfmt.Mesh
// ElmerSolver's native mesh-database format expects, plus the faceKey -> boundary-id
// mapping (1-based, insertion order of bound) the deck writer (Task 11) uses to address
// each condition. Node coordinates are NOT rescaled here: hostmesh.go's pullSurface
// already welds host cm into metres (see units.go's modelUnitM), so a TetMesh's node
// coordinates are already in meshfmt.Mesh's unit — converting again here would silently
// double-scale the geometry.
//
// Example:
//
//	m, ids, err := exportMesh(tetMesh, faceGroups, []BoundFace{{Key: "fixedFace"}})
//	ids["fixedFace"] // 1
func exportMesh(m *TetMesh, groups *FaceGroups, bound []BoundFace) (meshfmt.Mesh, map[string]int, error) {
	nodes, nodeIndex := exportNodes(m)
	tets, elemIndex := exportTets(m, nodeIndex)
	boundaryIDs := assignBoundaryIDs(bound)
	boundary, err := exportBoundary(bound, boundaryIDs, groups, elementsByID(m), elemIndex, nodeIndex)
	if err != nil {
		return meshfmt.Mesh{}, nil, err
	}
	return meshfmt.Mesh{Nodes: nodes, Tets: tets, Boundary: boundary}, boundaryIDs, nil
}

// exportNodes returns the mesh's node coordinates in ascending-original-id order, plus a
// map from a TetMesh node id to its compact 1-based meshfmt index. meshfmt.Mesh requires
// Nodes to be dense (id = slice index + 1); compacting through this map keeps that
// contract even if a future multi-body merge leaves gaps in the id space.
func exportNodes(m *TetMesh) ([][3]float64, map[int]int) {
	sorted := sortNodesByID(m.Nodes)
	nodes := make([][3]float64, len(sorted))
	index := make(map[int]int, len(sorted))
	for i, n := range sorted {
		nodes[i] = [3]float64{n.X, n.Y, n.Z}
		index[n.ID] = i + 1
	}
	return nodes, index
}

// compactNodeIndex maps each node's original TetMesh id to its compact 1-based meshfmt
// index, in ascending-id order — the same mapping exportNodes builds, exposed standalone
// for render.go: ElmerSolver writes its VTU result points in the same order
// meshfmt.Write wrote mesh.nodes (this same ascending-id compaction), so a node's VTU
// point index is compactNodeIndex(mesh.Nodes)[id]-1.
func compactNodeIndex(nodes []Node) map[int]int {
	sorted := sortNodesByID(nodes)
	index := make(map[int]int, len(sorted))
	for i, n := range sorted {
		index[n.ID] = i + 1
	}
	return index
}

// sortNodesByID returns a copy of nodes sorted ascending by ID, the common ordering
// exportNodes and compactNodeIndex both compact node ids against.
func sortNodesByID(nodes []Node) []Node {
	sorted := append([]Node(nil), nodes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	return sorted
}

// exportTets returns the mesh's tets in ascending-original-id order (mirroring
// exportNodes) plus a map from a TetMesh element id to its compact 1-based meshfmt
// index — a boundary face's Parent. TetElement.Body is 0-based (0 for a single-body
// mesh); meshfmt.Tet.Body is Elmer's 1-based body/material-region id.
func exportTets(m *TetMesh, nodeIndex map[int]int) ([]meshfmt.Tet, map[int]int) {
	sorted := append([]TetElement(nil), m.Elements...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	tets := make([]meshfmt.Tet, len(sorted))
	index := make(map[int]int, len(sorted))
	for i, e := range sorted {
		tets[i] = meshfmt.Tet{Body: e.Body + 1, Nodes: remapNodes(e.Nodes, nodeIndex)}
		index[e.ID] = i + 1
	}
	return tets, index
}

// elementsByID indexes a mesh's elements by their original id, for resolving a
// FaceGroups.ElemFace's Elem back to the element that owns it.
func elementsByID(m *TetMesh) map[int]TetElement {
	index := make(map[int]TetElement, len(m.Elements))
	for _, e := range m.Elements {
		index[e.ID] = e
	}
	return index
}

// assignBoundaryIDs assigns each bound face a 1-based boundary id in bound's order — the
// deterministic insertion order the caller (Task 11's constraint resolver) already
// established.
func assignBoundaryIDs(bound []BoundFace) map[string]int {
	ids := make(map[string]int, len(bound))
	for i, b := range bound {
		ids[b.Key] = i + 1
	}
	return ids
}

// exportBoundary renders every bound face's element-faces into meshfmt.BoundaryFace rows,
// erroring (naming the offending key) when a bound face has no matching FaceGroups entry —
// buildFaceGroups failed to bind it, or the caller passed a key it never resolved.
func exportBoundary(
	bound []BoundFace, ids map[string]int, groups *FaceGroups,
	elems map[int]TetElement, elemIndex, nodeIndex map[int]int,
) ([]meshfmt.BoundaryFace, error) {
	var out []meshfmt.BoundaryFace
	for _, bf := range bound {
		faces, ok := groups.ElemFaces[bf.Key]
		if !ok {
			return nil, fmt.Errorf("exportMesh: bound face %q has no matching group in FaceGroups (buildFaceGroups did not bind it)", bf.Key)
		}
		rows, err := exportBoundaryFaces(faces, ids[bf.Key], elems, elemIndex, nodeIndex)
		if err != nil {
			return nil, fmt.Errorf("exportMesh: face %q: %w", bf.Key, err)
		}
		out = append(out, rows...)
	}
	return out, nil
}

// exportBoundaryFaces renders one face key's element-faces into meshfmt.BoundaryFace rows.
func exportBoundaryFaces(
	faces []ElemFace, boundaryID int, elems map[int]TetElement, elemIndex, nodeIndex map[int]int,
) ([]meshfmt.BoundaryFace, error) {
	out := make([]meshfmt.BoundaryFace, 0, len(faces))
	for _, ef := range faces {
		el, ok := elems[ef.Elem]
		if !ok {
			return nil, fmt.Errorf("element %d not found in the mesh", ef.Elem)
		}
		out = append(out, meshfmt.BoundaryFace{
			Boundary: boundaryID,
			Parent:   elemIndex[ef.Elem],
			Nodes:    remapNodes(faceNodes(el, ef.Face), nodeIndex),
		})
	}
	return out, nil
}

// faceNodes returns the boundary-triangle node ids for one tet face (1-based, P1..P4): 3
// corners for a linear (4-node) element, or 3 corners + 3 mid-edge nodes (Elmer type-306
// order) for a quadratic (10-node) element.
func faceNodes(el TetElement, face int) []int {
	corners := tetFaceCorners[face-1]
	out := []int{el.Nodes[corners[0]], el.Nodes[corners[1]], el.Nodes[corners[2]]}
	if !el.IsQuadratic() {
		return out
	}
	mids := tetFaceMidsides[face-1]
	return append(out, el.Nodes[mids[0]], el.Nodes[mids[1]], el.Nodes[mids[2]])
}

// remapNodes translates a slice of TetMesh node ids through the compact meshfmt index.
func remapNodes(ids []int, index map[int]int) []int {
	out := make([]int, len(ids))
	for i, id := range ids {
		out[i] = index[id]
	}
	return out
}
