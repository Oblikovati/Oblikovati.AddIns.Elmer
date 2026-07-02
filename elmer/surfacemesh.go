// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"bufio"
	"fmt"
	"io"
	"math"
)

// Cloned verbatim (package ccx -> elmer only) from Oblikovati.AddIns.CalculiX's
// ccx/surfacemesh.go — this is where ccx actually keeps the weld logic (there is no
// separate weld.go in ccx; the task-10 brief's guessed "elmer/weld.go" split does not
// exist upstream, see task-10-report.md). weldEpsilonFraction is a scale-relative
// fraction of the surface's own bounding-box diagonal, so it needs no change for the
// cm->m rescale (that lives in hostmesh.go's modelUnitM, applied to coordinates before
// they ever reach weldSurface).

// SurfaceMesh is an indexed triangle surface: welded vertices and triangles referencing
// them by 0-based index. This is the watertight input handed to the volume mesher.
type SurfaceMesh struct {
	Verts [][3]float64
	Tris  [][3]int
}

// weldEpsilonFraction sets the vertex-merge tolerance as a fraction of the surface's
// bounding-box diagonal. Host tessellation emits per-face duplicate vertices at shared
// edges; welding within this tolerance stitches them into one manifold surface.
const weldEpsilonFraction = 1e-6

// weldSurface merges coincident vertices of a raw triangle soup (flat coordinate triples
// + flat triangle-index triples, as the host returns) into an indexed SurfaceMesh. Two
// vertices within the bbox-relative epsilon collapse to one, so triangles that shared a
// B-rep edge become topologically connected.
func weldSurface(coords []float64, indices []int) (*SurfaceMesh, error) {
	if len(coords)%3 != 0 {
		return nil, fmt.Errorf("surface coords length %d is not a multiple of 3", len(coords))
	}
	if len(indices)%3 != 0 {
		return nil, fmt.Errorf("surface index length %d is not a multiple of 3", len(indices))
	}
	eps := weldEpsilon(coords)
	mesh := &SurfaceMesh{}
	remap := make(map[[3]int64]int, len(coords)/3)
	src := make([]int, len(coords)/3) // raw vertex -> welded index
	for i := 0; i < len(coords)/3; i++ {
		v := [3]float64{coords[3*i], coords[3*i+1], coords[3*i+2]}
		src[i] = mesh.intern(v, eps, remap)
	}
	for t := 0; t < len(indices)/3; t++ {
		tri := addWeldedTri(src, indices[3*t], indices[3*t+1], indices[3*t+2])
		if tri != nil {
			mesh.Tris = append(mesh.Tris, *tri)
		}
	}
	return mesh, nil
}

// addWeldedTri maps a raw triangle's three indices through the weld remap, dropping any
// triangle that collapses to a degenerate (two welded corners coincide).
func addWeldedTri(src []int, a, b, c int) *[3]int {
	tri := [3]int{src[a], src[b], src[c]}
	if tri[0] == tri[1] || tri[1] == tri[2] || tri[0] == tri[2] {
		return nil
	}
	return &tri
}

// intern returns the welded index of v, quantising to an epsilon grid so coincident
// vertices share a key. A new vertex is appended on first sight.
func (m *SurfaceMesh) intern(v [3]float64, eps float64, remap map[[3]int64]int) int {
	key := [3]int64{
		int64(math.Round(v[0] / eps)),
		int64(math.Round(v[1] / eps)),
		int64(math.Round(v[2] / eps)),
	}
	if idx, ok := remap[key]; ok {
		return idx
	}
	idx := len(m.Verts)
	m.Verts = append(m.Verts, v)
	remap[key] = idx
	return idx
}

// weldEpsilon derives the merge tolerance from the coordinate bounding box.
func weldEpsilon(coords []float64) float64 {
	if len(coords) == 0 {
		return 1e-9
	}
	lo := [3]float64{coords[0], coords[1], coords[2]}
	hi := lo
	for i := 0; i < len(coords)/3; i++ {
		for k := 0; k < 3; k++ {
			c := coords[3*i+k]
			lo[k] = math.Min(lo[k], c)
			hi[k] = math.Max(hi[k], c)
		}
	}
	diag := math.Sqrt((hi[0]-lo[0])*(hi[0]-lo[0]) + (hi[1]-lo[1])*(hi[1]-lo[1]) + (hi[2]-lo[2])*(hi[2]-lo[2]))
	if diag == 0 {
		return 1e-9
	}
	return diag * weldEpsilonFraction
}

// openEdges counts the edges of the welded surface that are NOT shared by exactly two
// triangles. A watertight manifold surface has zero such edges; any other count means the
// surface has a hole or a non-manifold junction and cannot be filled into a solid volume.
func (m *SurfaceMesh) openEdges() int {
	count := make(map[[2]int]int, len(m.Tris)*3)
	for _, t := range m.Tris {
		count[edgeKey(t[0], t[1])]++
		count[edgeKey(t[1], t[2])]++
		count[edgeKey(t[2], t[0])]++
	}
	bad := 0
	for _, c := range count {
		if c != 2 {
			bad++
		}
	}
	return bad
}

// edgeKey is the order-independent key for an undirected edge.
func edgeKey(a, b int) [2]int {
	if a < b {
		return [2]int{a, b}
	}
	return [2]int{b, a}
}

// writeSTL writes the surface as an ASCII STL (gmsh's Merge reads this), computing a
// facet normal per triangle. The single solid is named "part".
func (m *SurfaceMesh) writeSTL(w io.Writer) error {
	bw := bufio.NewWriter(w)
	if _, err := fmt.Fprintln(bw, "solid part"); err != nil {
		return err
	}
	for _, tri := range m.Tris {
		a, b, c := m.Verts[tri[0]], m.Verts[tri[1]], m.Verts[tri[2]]
		n := triNormal(a, b, c)
		fmt.Fprintf(bw, " facet normal %g %g %g\n  outer loop\n", n[0], n[1], n[2])
		for _, v := range [3][3]float64{a, b, c} {
			fmt.Fprintf(bw, "   vertex %g %g %g\n", v[0], v[1], v[2])
		}
		fmt.Fprint(bw, "  endloop\n endfacet\n")
	}
	if _, err := fmt.Fprintln(bw, "endsolid part"); err != nil {
		return err
	}
	return bw.Flush()
}

// triNormal returns the unit normal of triangle a-b-c (zero vector for a degenerate).
func triNormal(a, b, c [3]float64) [3]float64 {
	u := [3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	v := [3]float64{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
	n := [3]float64{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
	mag := math.Sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2])
	if mag == 0 {
		return [3]float64{}
	}
	return [3]float64{n[0] / mag, n[1] / mag, n[2] / mag}
}
