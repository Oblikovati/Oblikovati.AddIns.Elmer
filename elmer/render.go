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

// resultFieldFor returns the per-VTU-point scalar values (file/point order, matching the
// mesh's node order — see compactNodeIndex) for the panel-selected field kind, with its
// human-readable label and SI unit. Any kind other than resultFieldDisplacement resolves
// to von Mises stress (M1's default result field).
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
	coords, indices, scalars, err := surfaceRenderData(mesh, values)
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
// VTU point order; each surface node's VTU point index comes from compactNodeIndex, the
// same original-id -> compact-index mapping exportMesh used to write the mesh ElmerSolver
// solved, so a node's position in values lines up with the mesh row ElmerSolver read it
// from.
func surfaceRenderData(mesh *TetMesh, values []float64) ([]float64, []int, []float64, error) {
	nodeIdx := compactNodeIndex(mesh.Nodes)
	byID := mesh.nodeByID()
	slot := make(map[int]int)
	var coords, scalars []float64
	var indices []int
	for _, bf := range mesh.Surface {
		for _, nid := range bf.Corners {
			if _, ok := slot[nid]; !ok {
				v, err := pointValue(values, nodeIdx, nid)
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

// pointValue looks up node nid's VTU point value, erroring (naming the node, the computed
// point index, and the VTU's point count) if the mesh's own compact numbering runs past
// what the VTU actually carries — a mesh/VTU that drifted out of sync, not a value silently
// defaulting to 0.
func pointValue(values []float64, nodeIdx map[int]int, nid int) (float64, error) {
	pi := nodeIdx[nid] - 1
	if pi < 0 || pi >= len(values) {
		return 0, fmt.Errorf("elmer: node %d has no matching VTU point (index %d of %d values)", nid, pi, len(values))
	}
	return values[pi], nil
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
