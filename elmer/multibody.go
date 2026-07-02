// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"fmt"
	"os"
	"path/filepath"

	"oblikovati.org/api/wire"
)

// Cloned + trimmed from Oblikovati.AddIns.CalculiX's ccx/multibody.go (package ccx ->
// elmer only): M1's single femmodel.MaterialObject applies uniformly to every body (see
// femmodel.MaterialObject's doc comment, "applied to the whole body"), so — unlike ccx's
// per-body host-material resolution — meshSolidBodies here needs no per-body material
// lookup, only the mesh + merge.

// solidBodies returns the active part's solid bodies — the ones a study meshes. Non-solid
// bodies (surfaces, wires) are skipped.
func (e *Engine) solidBodies() ([]wire.BodyInfo, error) {
	list, err := e.api.Body().List()
	if err != nil {
		return nil, fmt.Errorf("elmer: list bodies: %w", err)
	}
	var solids []wire.BodyInfo
	for _, b := range list.Bodies {
		if b.Solid {
			solids = append(solids, b)
		}
	}
	if len(solids) == 0 {
		return nil, fmt.Errorf("elmer: the active part has no solid bodies to analyse")
	}
	return solids, nil
}

// meshSolidBodies meshes each solid body separately (its own gmsh run in its own workdir)
// and merges the results into one tet mesh whose elements are tagged with their source
// body, so a multi-body part is analysed as one model with per-body element sets. Bodies
// are meshed independently, so coincident interfaces between bonded bodies are NOT node-
// conformal (a documented limitation, mirroring ccx's own).
func (e *Engine) meshSolidBodies(s StudySettings, solids []wire.BodyInfo, dir string) (*TetMesh, error) {
	gmshBin, err := resolveGmshBin()
	if err != nil {
		return nil, err
	}
	opts := meshOptionsFrom(s)
	meshes := make([]*TetMesh, 0, len(solids))
	for i, b := range solids {
		m, err := e.meshOneBody(gmshBin, opts, b, filepath.Join(dir, fmt.Sprintf("body%d", i)))
		if err != nil {
			return nil, err
		}
		meshes = append(meshes, m)
	}
	return mergeTetMeshes(meshes), nil
}

// meshOneBody pulls one solid body's surface and volume-meshes it in its own workdir.
func (e *Engine) meshOneBody(gmshBin string, opts MeshOptions, b wire.BodyInfo, bodyDir string) (*TetMesh, error) {
	surface, err := e.pullSurface(b.Index)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(bodyDir, 0o755); err != nil {
		return nil, fmt.Errorf("elmer: create body workdir: %w", err)
	}
	m, err := NewGmshMesher(gmshBin).Mesh(surface, opts, bodyDir)
	if err != nil {
		return nil, fmt.Errorf("elmer: mesh body %d (%s): %w", b.Index, b.Name, err)
	}
	return m, nil
}

// mergeTetMeshes offsets each body mesh's node ids, element ids, and gmsh surface tags so
// the merged mesh has one global numbering, tagging every element with its source body
// index. Offsetting the surface tags per body keeps each body's face groups distinct, so
// the face->FaceKey binding never matches a facet on the wrong body.
func mergeTetMeshes(meshes []*TetMesh) *TetMesh {
	merged := &TetMesh{}
	var nodeOff, elemOff, faceOff int
	for body, m := range meshes {
		maxNode, maxElem, maxFace := mergeOneBody(merged, m, body, nodeOff, elemOff, faceOff)
		nodeOff += maxNode
		elemOff += maxElem
		faceOff += maxFace + 1
	}
	return merged
}

// mergeOneBody appends one body mesh's nodes/elements/surface facets into merged at the
// given offsets, tagging every element with body, and returns that body's own max node/
// element/face id (for the caller to accumulate the next body's offsets from).
func mergeOneBody(merged, m *TetMesh, body, nodeOff, elemOff, faceOff int) (maxNode, maxElem, maxFace int) {
	for _, n := range m.Nodes {
		merged.Nodes = append(merged.Nodes, Node{ID: n.ID + nodeOff, X: n.X, Y: n.Y, Z: n.Z})
		maxNode = maxInt(maxNode, n.ID)
	}
	for _, el := range m.Elements {
		merged.Elements = append(merged.Elements, TetElement{ID: el.ID + elemOff, Nodes: offsetIDs(el.Nodes, nodeOff), Body: body})
		maxElem = maxInt(maxElem, el.ID)
	}
	for _, bf := range m.Surface {
		merged.Surface = append(merged.Surface, BoundaryFacet{
			Nodes:   offsetIDs(bf.Nodes, nodeOff),
			Corners: [3]int{bf.Corners[0] + nodeOff, bf.Corners[1] + nodeOff, bf.Corners[2] + nodeOff},
			Face:    bf.Face + faceOff,
		})
		maxFace = maxInt(maxFace, bf.Face)
	}
	return maxNode, maxElem, maxFace
}

// offsetIDs returns a copy of ids with off added to each (re-basing a body's local node
// numbering into the merged mesh's global numbering).
func offsetIDs(ids []int, off int) []int {
	out := make([]int, len(ids))
	for i, id := range ids {
		out[i] = id + off
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
