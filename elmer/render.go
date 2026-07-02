// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"fmt"
	"math"

	"oblikovati.org/api/wire"
	"oblikovati.org/elmer/elmer/vtu"
)

// render.go paints the panel-selected result field (M1: von Mises stress or displacement
// magnitude) as a flood plot over the tet mesh's surface — the render half of
// Oblikovati.AddIns.CalculiX's ccx/render.go + ccx/mapper.go, trimmed to Elmer's single
// elasticity result. Unlike ccx (which computes von Mises itself from the raw stress
// tensor, see ccx/vonmises.go), ElmerSolver's StressSolve module writes a scalar
// "vonmises" DataArray directly (see vendor-src/elmer/test/mesh/case_t0001.vtu), so this
// add-in reads it straight off the VTU.

// resultFieldVonMises / resultFieldDisplacement are the M1 result-field panel choices.
const (
	resultFieldVonMises     = "vonmises"
	resultFieldDisplacement = "displacement"
)

// resultClientID is the client-graphics group the result flood plot is pushed under.
const resultClientID = "elmer.result"

// resultMapperName is the registered color mapper the result flood plot uses.
const resultMapperName = "elmer.result"

// stressColorStops is the blue->cyan->green->yellow->red ramp (rgba) the field is painted
// with: low value blue, high value red (mirrors ccx/mapper.go's stressColorStops).
var stressColorStops = [][4]float32{
	{0.0, 0.0, 1.0, 1.0}, // blue
	{0.0, 1.0, 1.0, 1.0}, // cyan
	{0.0, 1.0, 0.0, 1.0}, // green
	{1.0, 1.0, 0.0, 1.0}, // yellow
	{1.0, 0.0, 0.0, 1.0}, // red
}

// rampMapper builds a color mapper spanning [lo, hi] across the blue->red ramp. A
// degenerate range is widened to a unit span so the mapper stays valid.
func rampMapper(lo, hi float64) wire.GraphicsColorMapper {
	if hi <= lo {
		hi = lo + 1
	}
	n := len(stressColorStops)
	values := make([]float64, n)
	colors := make([]float32, 0, n*4)
	for i, stop := range stressColorStops {
		values[i] = lo + (hi-lo)*float64(i)/float64(n-1)
		colors = append(colors, stop[0], stop[1], stop[2], stop[3])
	}
	return wire.GraphicsColorMapper{Values: values, Colors: colors}
}

// resultFieldFor returns the per-VTU-point scalar values (file/point order — see
// pointIndexForNodes for how a point's order is matched back to its owning mesh node) for the
// panel-selected field kind, with its human-readable label and SI unit. Any kind other than
// resultFieldDisplacement resolves to von Mises stress (M1's default result field).
func resultFieldFor(res *vtu.Result, kind string) ([]float64, string, string, error) {
	if kind == resultFieldDisplacement {
		vals, comps, ok := res.Field("displacement")
		if !ok || comps != 3 {
			return nil, "", "", fmt.Errorf("elmer: VTU has no 3-component %q field", "displacement")
		}
		return magnitudes(vals), "displacement", "m", nil
	}
	vals, comps, ok := res.Field("vonmises")
	if !ok || comps != 1 {
		return nil, "", "", fmt.Errorf("elmer: VTU has no scalar %q field", "vonmises")
	}
	return vals, "von Mises stress", "Pa", nil
}

// magnitudes reshapes a flattened 3-component field (x,y,z per point) into one magnitude
// per point.
func magnitudes(vals []float64) []float64 {
	out := make([]float64, len(vals)/3)
	for i := range out {
		x, y, z := vals[3*i], vals[3*i+1], vals[3*i+2]
		out[i] = math.Sqrt(x*x + y*y + z*z)
	}
	return out
}

// renderResult paints the selected scalar field over the mesh surface as a client-graphics
// flood plot spanning the field's actual (surface) range, and returns that range for the
// study's status report.
func (e *Engine) renderResult(mesh *TetMesh, res *vtu.Result, kind string) (StudyResult, error) {
	values, label, unit, err := resultFieldFor(res, kind)
	if err != nil {
		return StudyResult{}, err
	}
	coords, indices, scalars, err := surfaceRenderData(mesh, res, values)
	if err != nil {
		return StudyResult{}, err
	}
	lo, hi := minMaxSlice(scalars)
	mapper := rampMapper(lo, hi)
	if err := e.api.Graphics().RegisterColorMapper(resultMapperName, mapper); err != nil {
		return StudyResult{}, err
	}
	if _, err := e.api.Graphics().AddFloodPlot(resultClientID, coords, indices, scalars, mapper, 1.0); err != nil {
		return StudyResult{}, err
	}
	return StudyResult{FieldLabel: label, Unit: unit, Min: lo, Max: hi}, nil
}

// surfaceRenderData flattens the mesh surface's corner nodes into the (coords, triangle-
// indices, per-vertex scalar) arrays the flood plot expects, converting coordinates from
// the mesh's metres to host model units (cm, dividing by modelUnitM). values is indexed in
// VTU point order (res.Points' order); each surface node's VTU point index is resolved by
// pointIndexForNodes, which verifies the correspondence by GEOMETRY rather than trusting
// that a node's compact position lines up with its point's file position.
func surfaceRenderData(mesh *TetMesh, res *vtu.Result, values []float64) ([]float64, []int, []float64, error) {
	ptIdx, err := pointIndexForNodes(mesh, res.Points)
	if err != nil {
		return nil, nil, nil, err
	}
	byID := mesh.nodeByID()
	slot := make(map[int]int)
	var coords, scalars []float64
	var indices []int
	for _, bf := range mesh.Surface {
		for _, nid := range bf.Corners {
			if _, ok := slot[nid]; !ok {
				v, err := pointValue(values, ptIdx, nid)
				if err != nil {
					return nil, nil, nil, err
				}
				slot[nid] = len(coords) / 3
				n := byID[nid]
				coords = append(coords, n.X/modelUnitM, n.Y/modelUnitM, n.Z/modelUnitM)
				scalars = append(scalars, v)
			}
			indices = append(indices, slot[nid])
		}
	}
	return coords, indices, scalars, nil
}

// pointValue looks up node nid's VTU point value through ptIdx (a verified node-id ->
// 0-based point-index map from pointIndexForNodes), erroring (naming the node, the resolved
// point index, and the VTU's point count) if that index runs past what the VTU actually
// carries — a mesh/VTU that drifted out of sync, not a value silently defaulting to 0.
func pointValue(values []float64, ptIdx map[int]int, nid int) (float64, error) {
	pi, ok := ptIdx[nid]
	if !ok || pi < 0 || pi >= len(values) {
		return 0, fmt.Errorf("elmer: node %d has no matching VTU point (index %d of %d values)", nid, pi, len(values))
	}
	return values[pi], nil
}

// pointVerifyEpsAbs / pointVerifyEpsRel bound how far a VTU point may drift from its expected
// mesh-node coordinate and still count as the "same" point: this add-in's mesh is in metres,
// so 1e-9 m (1 nanometre) absolute is far tighter than any real node-coordinate difference,
// while the relative term (times the mesh's bounding-box diagonal) keeps the check meaningful
// for models much larger than a metre.
const (
	pointVerifyEpsAbs = 1e-9
	pointVerifyEpsRel = 1e-9
)

// pointIndexForNodes returns, for each mesh node id, the 0-based index into points (and
// therefore into any per-point values slice vtu.Result.Field returned) that carries that
// node's result. ElmerSolver normally writes VTU points in the same ascending-id compacted
// order meshfmt.Write wrote mesh.nodes in (see compactNodeIndex's doc comment), so the fast
// path below just CONFIRMS that positional assumption holds, geometrically, instead of
// trusting it blindly: this add-in's deck sets "Optimize Bandwidth = True", and
// VtuOutputSolver has an InvNodePerm renumbering path that can reorder points relative to the
// mesh file if it ever engages. A silent positional read in that case would still produce a
// plausible-looking flood plot (same value range, same shape) with values on the wrong
// nodes — exactly why this is checked by geometry, not trusted from position. The fast path
// keeps the common case a single O(n) scan; only a genuine mismatch pays for the hash-based
// remap.
func pointIndexForNodes(mesh *TetMesh, points [][3]float64) (map[int]int, error) {
	sorted := sortNodesByID(mesh.Nodes)
	eps := pointVerifyEpsilon(sorted)
	if positionsMatch(sorted, points, eps) {
		return identityPointIndex(sorted), nil
	}
	return remapPointsByCoordinate(sorted, points, eps)
}

// pointVerifyEpsilon returns the coordinate-match tolerance for nodes' bounding-box scale.
func pointVerifyEpsilon(nodes []Node) float64 {
	if diag := bboxDiagonal(nodes); diag*pointVerifyEpsRel > pointVerifyEpsAbs {
		return diag * pointVerifyEpsRel
	}
	return pointVerifyEpsAbs
}

// bboxDiagonal returns the diagonal length of nodes' axis-aligned bounding box (0 for an
// empty or single-node set).
func bboxDiagonal(nodes []Node) float64 {
	if len(nodes) == 0 {
		return 0
	}
	minX, minY, minZ := nodes[0].X, nodes[0].Y, nodes[0].Z
	maxX, maxY, maxZ := minX, minY, minZ
	for _, n := range nodes[1:] {
		minX, maxX = math.Min(minX, n.X), math.Max(maxX, n.X)
		minY, maxY = math.Min(minY, n.Y), math.Max(maxY, n.Y)
		minZ, maxZ = math.Min(minZ, n.Z), math.Max(maxZ, n.Z)
	}
	dx, dy, dz := maxX-minX, maxY-minY, maxZ-minZ
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// positionsMatch reports whether points, taken in file order, line up 1:1 with sorted (the
// mesh's ascending-id node order) within eps — the fast path pointIndexForNodes hopes for.
func positionsMatch(sorted []Node, points [][3]float64, eps float64) bool {
	if len(sorted) != len(points) {
		return false
	}
	for i, n := range sorted {
		if !coordsClose(points[i], n, eps) {
			return false
		}
	}
	return true
}

// coordsClose reports whether p and n agree within eps on every axis.
func coordsClose(p [3]float64, n Node, eps float64) bool {
	return math.Abs(p[0]-n.X) <= eps && math.Abs(p[1]-n.Y) <= eps && math.Abs(p[2]-n.Z) <= eps
}

// identityPointIndex builds the node-id -> point-index map for the (already-confirmed)
// positional case: point i is node sorted[i].
func identityPointIndex(sorted []Node) map[int]int {
	idx := make(map[int]int, len(sorted))
	for i, n := range sorted {
		idx[n.ID] = i
	}
	return idx
}

// coordBucket is a spatial hash key: coordinates quantized to eps-sized cells, so points
// within eps of each other land in the same or an adjacent cell.
type coordBucket [3]int64

// quantizeCoord maps a point to its bucket at the given cell size.
func quantizeCoord(p [3]float64, eps float64) coordBucket {
	return coordBucket{int64(math.Round(p[0] / eps)), int64(math.Round(p[1] / eps)), int64(math.Round(p[2] / eps))}
}

// remapPointsByCoordinate matches each mesh node (sorted, ascending id) to its VTU point by
// coordinate, in one pass building a bucket index over points, then one lookup per node — the
// "only pay for it on the mismatch path" remap pointIndexForNodes falls back to. It errors
// naming the first node (by compact index, id, and coordinates) that has no matching point
// within eps, so a genuinely desynced mesh/VTU pair fails loudly instead of painting a wrong
// or default value.
func remapPointsByCoordinate(sorted []Node, points [][3]float64, eps float64) (map[int]int, error) {
	buckets := make(map[coordBucket][]int, len(points))
	for i, p := range points {
		k := quantizeCoord(p, eps)
		buckets[k] = append(buckets[k], i)
	}
	idx := make(map[int]int, len(sorted))
	for i, n := range sorted {
		pi, ok := findMatchingPoint(n, points, buckets, eps)
		if !ok {
			return nil, fmt.Errorf(
				"elmer: VTU point order does not match the mesh's node order and no matching "+
					"VTU point was found for node index %d (id %d) at (%.9g, %.9g, %.9g) within tolerance %.3g",
				i, n.ID, n.X, n.Y, n.Z, eps)
		}
		idx[n.ID] = pi
	}
	return idx, nil
}

// findMatchingPoint searches the 27 buckets around n's own cell (n itself may have quantized
// into a neighboring cell from its true match due to rounding at a cell boundary) for a point
// within eps of n, returning its index into points.
func findMatchingPoint(n Node, points [][3]float64, buckets map[coordBucket][]int, eps float64) (int, bool) {
	base := quantizeCoord([3]float64{n.X, n.Y, n.Z}, eps)
	for dx := int64(-1); dx <= 1; dx++ {
		for dy := int64(-1); dy <= 1; dy++ {
			for dz := int64(-1); dz <= 1; dz++ {
				k := coordBucket{base[0] + dx, base[1] + dy, base[2] + dz}
				for _, pi := range buckets[k] {
					if coordsClose(points[pi], n, eps) {
						return pi, true
					}
				}
			}
		}
	}
	return 0, false
}

// minMaxSlice returns the minimum and maximum of vals (0, 0 for an empty slice).
func minMaxSlice(vals []float64) (float64, float64) {
	if len(vals) == 0 {
		return 0, 0
	}
	lo, hi := vals[0], vals[0]
	for _, v := range vals[1:] {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	return lo, hi
}
