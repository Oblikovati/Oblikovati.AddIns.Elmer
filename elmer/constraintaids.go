// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"math"

	"oblikovati.org/api/types"
	"oblikovati.org/api/wire"
	"oblikovati.org/elmer/elmer/femmodel"
)

// Cloned + trimmed from Oblikovati.AddIns.CalculiX's ccx/constraintaids.go (package ccx ->
// elmer only): M1 has exactly one constraint shape (a fixed support face plus force/
// pressure loaded faces — no gravity body load, no electrostatic potential faces), so the
// per-AnalysisType dispatch ccx's renderConstraints does is dropped.

// Client-graphics groups for the constraint visual aids (separate from the result so they
// can be toggled or replaced independently).
const (
	supportsClientID = "elmer.supports"
	loadsClientID    = "elmer.loads"
)

// supportColor is the cyan of the fixed-support cubes; loadColor the red of the load
// arrows — the conventional FEA support/load colours.
var (
	supportColor = []float32{0.20, 0.70, 1.0, 1.0}
	loadColor    = []float32{1.0, 0.25, 0.12, 1.0}
)

// maxConstraintGlyphs caps the glyphs drawn per face so a fine mesh does not bury the
// model in symbols; the node set is sampled evenly to this many.
const maxConstraintGlyphs = 24

// renderConstraints draws solid 3D fixed-support cubes on the first selected face and load
// arrows on the rest, mirroring the support/load symbols a dedicated FEA setup shows.
// Coordinates are converted from the mesh's metres back to host model units (cm).
func (e *Engine) renderConstraints(mesh *TetMesh, groups *FaceGroups, faces []string, load femmodel.LoadDefaults) error {
	index := mesh.nodeByID()
	length := glyphScale(mesh)
	if err := e.drawSupports(groups.Nodes[faces[0]], index, length); err != nil {
		return err
	}
	return e.drawLoads(groups, faces[1:], load, index, length)
}

// drawSupports paints a solid cyan cube at each fixed-face node.
func (e *Engine) drawSupports(nodes []int, index map[int]Node, length float64) error {
	return e.drawCubes(supportsClientID, supportColor, nodes, index, length)
}

// drawCubes paints a solid cube of the given colour at each node, under the given client
// group — the shared glyph for "this face is pinned".
func (e *Engine) drawCubes(clientID string, color []float32, nodes []int, index map[int]Node, length float64) error {
	g := &glyphMesh{}
	half := length * 0.16
	for _, nid := range sampleNodes(nodes, maxConstraintGlyphs) {
		g.cube(modelPoint(index[nid]), half)
	}
	return e.pushGlyphs(clientID, g, color)
}

// drawLoads paints a solid arrow at each loaded-face node, pointing along the load
// direction (see loadDirection).
func (e *Engine) drawLoads(groups *FaceGroups, faces []string, load femmodel.LoadDefaults, index map[int]Node, length float64) error {
	g := &glyphMesh{}
	for _, key := range faces {
		dir := loadDirection(load, groups.Normals[key])
		for _, nid := range sampleNodes(groups.Nodes[key], maxConstraintGlyphs) {
			g.arrow(modelPoint(index[nid]), dir, length)
		}
	}
	return e.pushGlyphs(loadsClientID, g, loadColor)
}

// loadDirection returns the load arrow's direction: -Z for a force load (deck.go's fixed
// reference axis, see forceVector), or the face's inward normal for a pressure load
// (matching buildDeck's "a positive pressure pushes INTO the face" sign convention).
func loadDirection(load femmodel.LoadDefaults, outwardNormal [3]float64) [3]float64 {
	if load.LoadType == "pressure" {
		return scale(outwardNormal, -1)
	}
	return [3]float64{0, 0, -1}
}

// pushGlyphs pushes a glyph mesh as a lit, OnTop client-graphics group so the aids render
// above the depth-tested geometry and the result flood-plot overlay.
func (e *Engine) pushGlyphs(clientID string, g *glyphMesh, color []float32) error {
	if len(g.idx) == 0 {
		return nil
	}
	_, err := e.api.Graphics().Set(onTopGroup(clientID, wire.GraphicsPrimitive{
		Kind:        string(types.GraphicsTriangles),
		Coordinates: g.coords,
		Indices:     g.idx,
		Normals:     g.normals,
		Color:       color,
	}))
	return err
}

// onTopGroup wraps one primitive as an OnTop client-graphics group in the persistent lane,
// so the support/load aids render above the geometry and the result overlay.
func onTopGroup(clientID string, p wire.GraphicsPrimitive) wire.SetClientGraphicsArgs {
	p.OnTop = true
	p.DepthPriority = 10
	return wire.SetClientGraphicsArgs{
		ClientId: clientID,
		Lane:     string(types.GraphicsLanePersistent),
		Nodes:    []wire.GraphicsNode{{Primitives: []wire.GraphicsPrimitive{p}}},
	}
}

// anyPerpendicular returns a unit vector orthogonal to d.
func anyPerpendicular(d [3]float64) [3]float64 {
	axis := [3]float64{1, 0, 0}
	if math.Abs(d[0]) > 0.9 {
		axis = [3]float64{0, 1, 0}
	}
	return normalize(cross(d, axis))
}

// modelPoint converts a mesh node (metres) to host model units (cm).
func modelPoint(n Node) [3]float64 {
	return [3]float64{n.X / modelUnitM, n.Y / modelUnitM, n.Z / modelUnitM}
}

// glyphScale sizes the glyphs relative to the model bounding box (host model units).
func glyphScale(mesh *TetMesh) float64 {
	lo, hi := meshBounds(mesh)
	diag := math.Sqrt((hi[0]-lo[0])*(hi[0]-lo[0]) + (hi[1]-lo[1])*(hi[1]-lo[1]) + (hi[2]-lo[2])*(hi[2]-lo[2]))
	return (diag / modelUnitM) * 0.14
}

// meshBounds returns the mesh's coordinate bounding box (metres).
func meshBounds(mesh *TetMesh) ([3]float64, [3]float64) {
	lo := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, n := range mesh.Nodes {
		for k, c := range [3]float64{n.X, n.Y, n.Z} {
			lo[k] = math.Min(lo[k], c)
			hi[k] = math.Max(hi[k], c)
		}
	}
	return lo, hi
}

// sampleNodes returns up to limit node ids spread evenly across the set.
func sampleNodes(nodes []int, limit int) []int {
	if len(nodes) <= limit {
		return nodes
	}
	step := float64(len(nodes)) / float64(limit)
	out := make([]int, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, nodes[int(float64(i)*step)])
	}
	return out
}
