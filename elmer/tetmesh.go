// SPDX-License-Identifier: GPL-2.0-only

package elmer

// Cloned from Oblikovati.AddIns.CalculiX's ccx/tetmesh.go (package ccx -> elmer only; see
// task-10-report.md). Node/TetElement/BoundaryFacet/TetMesh are load-bearing names: Tasks
// 11-13 build the deck/mesh-export/study pipeline directly on top of them.

// Node is a mesh node: a 1-based id (CalculiX/gmsh use 1-based numbering) and its
// coordinates in model units.
type Node struct {
	ID      int
	X, Y, Z float64
}

// TetElement is a tetrahedral finite element: a 1-based id and the node ids of its
// corners. A 4-id element is linear; a 10-id element is quadratic. Body is the source
// body's index in a merged multi-body mesh (0 for a single-body mesh), used to group
// elements into per-body material regions (meshexport.go maps this to Elmer's 1-based
// mesh.elements body id).
type TetElement struct {
	ID    int
	Nodes []int
	Body  int
}

// IsQuadratic reports whether this element is a 10-node quadratic tetrahedron.
func (e TetElement) IsQuadratic() bool { return len(e.Nodes) == 10 }

// BoundaryFacet is a triangular face on the mesh surface, carrying the node ids of the
// triangle and the gmsh elementary surface tag it belongs to. Quadratic meshes give
// 6-node triangles (the 3 corners plus 3 edge midpoints); Corners holds the 3 corner ids
// used for face-group matching. Face is gmsh's reclassified surface id — facets sharing
// a Face lie on one smooth B-rep face, which the engine maps to a host FaceKey.
type BoundaryFacet struct {
	Nodes   []int // 3 (linear) or 6 (quadratic) node ids
	Corners [3]int
	Face    int
}

// TetMesh is a solid tetrahedral mesh: nodes, volume elements, and the triangular
// facets on its outer surface (used to bind loads/BCs to picked B-rep faces).
type TetMesh struct {
	Nodes    []Node
	Elements []TetElement
	Surface  []BoundaryFacet
}

// nodeByID indexes nodes by their 1-based id for O(1) coordinate lookup.
func (m *TetMesh) nodeByID() map[int]Node {
	index := make(map[int]Node, len(m.Nodes))
	for _, n := range m.Nodes {
		index[n.ID] = n
	}
	return index
}
