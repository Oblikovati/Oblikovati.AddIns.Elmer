// SPDX-License-Identifier: GPL-2.0-only

package elmer

import "math"

// Cloned verbatim from Oblikovati.AddIns.CalculiX's ccx/glyphmesh.go (package ccx -> elmer
// only). dot/distance/normalize already live in elmer/facegroups.go (cloned in Task 10) and
// triNormal already lives in elmer/surfacemesh.go, so this file defines only the symbols
// unique to the glyph builder itself.

// glyphSegments is the radial tessellation of the round glyph parts (shafts, heads).
const glyphSegments = 12

// glyphMesh accumulates a lit triangle mesh (coordinates + per-vertex normals + indices)
// for the 3D constraint glyphs — solid arrows and cubes that read as real geometry rather
// than flat lines.
type glyphMesh struct {
	coords  []float64
	normals []float64
	idx     []int
}

// tri appends one triangle with a flat face normal.
func (g *glyphMesh) tri(a, b, c [3]float64) {
	n := triNormal(a, b, c)
	base := len(g.coords) / 3
	for _, p := range [3][3]float64{a, b, c} {
		g.coords = append(g.coords, p[0], p[1], p[2])
		g.normals = append(g.normals, n[0], n[1], n[2])
	}
	g.idx = append(g.idx, base, base+1, base+2)
}

// arrow appends a solid 3D arrow whose head sits at tip and which points along dir: a
// cylindrical shaft and a conical head, sized from length.
func (g *glyphMesh) arrow(tip, dir [3]float64, length float64) {
	d := normalize(dir)
	headLen := length * 0.4
	shaftR, headR := length*0.05, length*0.13
	neck := add(tip, scale(d, -headLen)) // shaft/head junction
	tail := add(tip, scale(d, -length))  // shaft start
	g.cylinder(tail, neck, shaftR)
	g.cone(neck, tip, headR)
}

// cylinder appends a closed cylinder between p0 and p1 with the given radius.
func (g *glyphMesh) cylinder(p0, p1 [3]float64, r float64) {
	axis := normalize(sub(p1, p0))
	u, v := basis(axis)
	for s := 0; s < glyphSegments; s++ {
		a0, a1 := ringPoint(p0, u, v, r, s), ringPoint(p0, u, v, r, s+1)
		b0, b1 := ringPoint(p1, u, v, r, s), ringPoint(p1, u, v, r, s+1)
		g.tri(a0, b0, b1)
		g.tri(a0, b1, a1)
	}
}

// cone appends a cone with its circular base at base and its apex at apex.
func (g *glyphMesh) cone(base, apex [3]float64, r float64) {
	axis := normalize(sub(apex, base))
	u, v := basis(axis)
	for s := 0; s < glyphSegments; s++ {
		p0, p1 := ringPoint(base, u, v, r, s), ringPoint(base, u, v, r, s+1)
		g.tri(p0, p1, apex) // side
		g.tri(p1, p0, base) // base cap
	}
}

// cube appends an axis-aligned cube centred at c with the given half-extent.
func (g *glyphMesh) cube(c [3]float64, half float64) {
	v := func(sx, sy, sz float64) [3]float64 {
		return [3]float64{c[0] + sx*half, c[1] + sy*half, c[2] + sz*half}
	}
	corners := [8][3]float64{
		v(-1, -1, -1), v(1, -1, -1), v(1, 1, -1), v(-1, 1, -1),
		v(-1, -1, 1), v(1, -1, 1), v(1, 1, 1), v(-1, 1, 1),
	}
	faces := [6][4]int{{0, 3, 2, 1}, {4, 5, 6, 7}, {0, 1, 5, 4}, {1, 2, 6, 5}, {2, 3, 7, 6}, {3, 0, 4, 7}}
	for _, f := range faces {
		g.tri(corners[f[0]], corners[f[1]], corners[f[2]])
		g.tri(corners[f[0]], corners[f[2]], corners[f[3]])
	}
}

// ringPoint returns a point on a circle of radius r about centre, in the (u, v) plane.
func ringPoint(center, u, v [3]float64, r float64, s int) [3]float64 {
	a := 2 * math.Pi * float64(s) / float64(glyphSegments)
	cos, sin := math.Cos(a)*r, math.Sin(a)*r
	return [3]float64{
		center[0] + u[0]*cos + v[0]*sin,
		center[1] + u[1]*cos + v[1]*sin,
		center[2] + u[2]*cos + v[2]*sin,
	}
}

// basis returns two unit vectors spanning the plane orthogonal to axis.
func basis(axis [3]float64) ([3]float64, [3]float64) {
	u := anyPerpendicular(axis)
	return u, normalize(cross(axis, u))
}

func add(a, b [3]float64) [3]float64           { return [3]float64{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func sub(a, b [3]float64) [3]float64           { return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func scale(a [3]float64, s float64) [3]float64 { return [3]float64{a[0] * s, a[1] * s, a[2] * s} }
func cross(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
