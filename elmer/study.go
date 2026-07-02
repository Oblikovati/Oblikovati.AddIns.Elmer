// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"oblikovati.org/elmer/elmer/femmodel"
	"oblikovati.org/elmer/elmer/meshfmt"
	"oblikovati.org/elmer/elmer/vtu"
)

// study.go is the M1 study orchestrator: it wires the mesh (Task 10), deck (Task 11), solve
// (Task 10), and render (this task) stages into one pipeline, mirroring
// Oblikovati.AddIns.CalculiX's ccx/study.go shape with CalculiX-specific machinery (multi-
// analysis dispatch, .frd/.dat parsing) dropped — M1 is a single elasticity equation read
// back from one VTU file.

// StudyResult summarizes one Elmer study: the rendered field's label/unit and the value
// range painted over the model.
type StudyResult struct {
	FieldLabel string
	Unit       string
	Min, Max   float64
}

// Summary renders the one-line status message for the run.
func (r StudyResult) Summary() string {
	return fmt.Sprintf("Elmer: %s %.4g..%.4g %s", r.FieldLabel, r.Min, r.Max, r.Unit)
}

// faceRefPrefix is how the host's selection encodes a face: "face/" + URL-safe base64 of
// the raw reference key (cloned verbatim from Oblikovati.AddIns.CalculiX's
// ccx/selection.go — face.calculateFacets resolves the RAW key bytes, so a selection
// reference must be decoded before it is used to address a face).
const faceRefPrefix = "face/"

// mmToM converts a length from millimetres (femmodel.MeshObject's unit) to the metres
// gmsh/meshfmt expect.
const mmToM = 0.001

// runStudy is the end-to-end study flow for the active part: project the study settings,
// validate them and the selection, then mesh -> bind -> deck -> solve -> render in a fresh
// scratch directory (constraint: `os.MkdirTemp("elmer-study-")`). The scratch dir is
// removed on success; a failure keeps it (named in the returned error) so a stuck deck/mesh
// is inspectable.
func (e *Engine) runStudy() (StudyResult, error) {
	s := e.study()
	if err := validateStudySettings(s); err != nil {
		return StudyResult{}, err
	}
	faces, err := e.selectedFaces()
	if err != nil {
		return StudyResult{}, err
	}
	dir, err := os.MkdirTemp("", "elmer-study-")
	if err != nil {
		return StudyResult{}, fmt.Errorf("elmer: create study scratch dir: %w", err)
	}
	result, err := e.runStudyIn(dir, s, faces)
	if err != nil {
		return StudyResult{}, fmt.Errorf("%w (scratch dir kept for inspection: %s)", err, dir)
	}
	_ = os.RemoveAll(dir)
	return result, nil
}

// runStudyIn executes the mesh -> export -> deck -> solve -> render pipeline inside dir.
func (e *Engine) runStudyIn(dir string, s StudySettings, faces []string) (StudyResult, error) {
	solids, err := e.solidBodies()
	if err != nil {
		return StudyResult{}, err
	}
	mesh, err := e.meshSolidBodies(s, solids, dir)
	if err != nil {
		return StudyResult{}, err
	}
	groups, err := e.buildFaceGroups(faces, mesh, solids)
	if err != nil {
		return StudyResult{}, err
	}
	if err := writeDeckAndMesh(dir, s, mesh, groups, faces, len(solids)); err != nil {
		return StudyResult{}, err
	}
	res, err := e.solveAndRead(dir)
	if err != nil {
		return StudyResult{}, err
	}
	return e.renderStudy(mesh, groups, faces, s.Load, res)
}

// writeDeckAndMesh exports the tet mesh + face bindings into Elmer's native mesh-database
// format and writes the matching SIF deck into dir.
func writeDeckAndMesh(dir string, s StudySettings, mesh *TetMesh, groups *FaceGroups, faces []string, bodyCount int) error {
	bound := boundFacesFrom(faces)
	meshOut, bcs, err := exportMesh(mesh, groups, bound)
	if err != nil {
		return err
	}
	if err := meshfmt.Write(dir, meshOut); err != nil {
		return err
	}
	resolved := resolveConstraints(s.Load, bcs, faces)
	b, err := buildDeck(s, bodyIDs(bodyCount), bcs, resolved)
	if err != nil {
		return err
	}
	return writeDeckFiles(dir, b)
}

// solveAndRead runs the solver (or the injected test stub, see Engine.solve), checks its
// output for the known ElmerSolver failure signatures, and parses + NaN-checks the newest
// case*.vtu it wrote into dir.
func (e *Engine) solveAndRead(dir string) (*vtu.Result, error) {
	stdout, err := e.solve(dir)
	if err != nil {
		return nil, err
	}
	if err := checkSolverOutput(stdout, dir); err != nil {
		return nil, err
	}
	path, err := latestVTUFile(dir)
	if err != nil {
		return nil, err
	}
	res, err := vtu.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := res.NaNCheck(); err != nil {
		return nil, fmt.Errorf("elmer: study diverged: %w", err)
	}
	return res, nil
}

// renderStudy paints the panel-selected result field plus the support/load aids, and
// returns the field's range summary.
func (e *Engine) renderStudy(mesh *TetMesh, groups *FaceGroups, faces []string, load femmodel.LoadDefaults, res *vtu.Result) (StudyResult, error) {
	result, err := e.renderResult(mesh, res, e.resultFieldKind())
	if err != nil {
		return StudyResult{}, fmt.Errorf("elmer: render result: %w", err)
	}
	if err := e.renderConstraints(mesh, groups, faces, load); err != nil {
		return StudyResult{}, fmt.Errorf("elmer: render constraints: %w", err)
	}
	return result, nil
}

// validateStudySettings rejects a degraded (e.g. zero-value) projected StudySettings before
// it reaches the mesher/solver, naming the offending field, its value, and the expected
// shape.
func validateStudySettings(s StudySettings) error {
	if s.Mesh.Order != 1 && s.Mesh.Order != 2 {
		return fmt.Errorf("elmer: mesh element order %d is invalid, want 1 (linear) or 2 (quadratic)", s.Mesh.Order)
	}
	if s.Mesh.MaxSizeMM <= 0 {
		return fmt.Errorf("elmer: mesh element size %v mm is invalid, want a positive value", s.Mesh.MaxSizeMM)
	}
	if s.Material.YoungGPa <= 0 {
		return fmt.Errorf("elmer: material Young's modulus %v GPa is invalid, want a positive value", s.Material.YoungGPa)
	}
	if s.Material.Poisson < 0 || s.Material.Poisson >= 0.5 {
		return fmt.Errorf("elmer: material Poisson's ratio %v is invalid, want 0 <= v < 0.5", s.Material.Poisson)
	}
	return nil
}

// selectedFaces returns the picked faces' raw reference keys, decoded from the host's
// "face/<base64>" selection form. M1's implicit convention needs at least 2: the first is
// the fixed support, the rest carry the load (ccx M1 convention; no constraint builder yet).
func (e *Engine) selectedFaces() ([]string, error) {
	sel, err := e.api.Model().Selection()
	if err != nil {
		return nil, fmt.Errorf("elmer: read selection: %w", err)
	}
	faces := decodeSelectedFaces(sel.Refs)
	if len(faces) < 2 {
		return nil, fmt.Errorf("elmer: select at least 2 faces (the first is fixed, the rest carry the load); "+
			"got %d face(s) among %d selected entities", len(faces), len(sel.Refs))
	}
	return faces, nil
}

// decodeSelectedFaces keeps only the face references in a selection and decodes each into
// the raw reference key face.calculateFacets resolves (cloned verbatim from
// Oblikovati.AddIns.CalculiX's ccx/selection.go). Non-face references (edges, vertices,
// work geometry, ...) are dropped.
func decodeSelectedFaces(refs []string) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if key, ok := decodeFaceRef(ref); ok {
			out = append(out, key)
		}
	}
	return out
}

// decodeFaceRef turns a "face/<url-base64>" selection reference into its raw key, or
// reports false for a non-face / malformed reference.
func decodeFaceRef(ref string) (string, bool) {
	if !strings.HasPrefix(ref, faceRefPrefix) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ref, faceRefPrefix))
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// boundFacesFrom wraps every selected face key as a BoundFace for exportMesh, so both the
// fixed and loaded faces get a mesh boundary id regardless of which BC kind they resolve to.
func boundFacesFrom(faces []string) []BoundFace {
	out := make([]BoundFace, len(faces))
	for i, key := range faces {
		out[i] = BoundFace{Key: key}
	}
	return out
}

// resolveConstraints applies M1's implicit convention: the first face is fixed, the rest
// carry the study's load — as a force or a pressure BC depending on Load.LoadType.
func resolveConstraints(load femmodel.LoadDefaults, bcs map[string]int, faces []string) resolvedConstraints {
	resolved := resolvedConstraints{Fixed: []int{bcs[faces[0]]}}
	for _, key := range faces[1:] {
		if load.LoadType == "pressure" {
			resolved.Pressure = append(resolved.Pressure, bcs[key])
		} else {
			resolved.Force = append(resolved.Force, bcs[key])
		}
	}
	return resolved
}

// bodyIDs returns the Elmer body ids 1..n mergeTetMeshes assigns for n meshed solid bodies.
func bodyIDs(n int) []int {
	ids := make([]int, n)
	for i := range ids {
		ids[i] = i + 1
	}
	return ids
}

// meshOptionsFrom projects a study's mesh settings into gmsh's MeshOptions, converting the
// element size from femmodel's millimetres to gmsh/meshfmt's metres.
func meshOptionsFrom(s StudySettings) MeshOptions {
	return MeshOptions{SizeM: s.Mesh.MaxSizeMM * mmToM, Order: s.Mesh.Order}
}

// latestVTUFile returns the lexicographically last case*.vtu written into dir —
// ElmerSolver names multi-step output case0001.vtu, case0002.vtu, ...; M1's steady-state
// single-step solve writes exactly one, but picking the last keeps this correct if that
// ever changes. ResultOutputSolver writes into the deck's Mesh DB directory, which this
// add-in's decks always set to "." "." (deck.go), so results land in dir itself.
func latestVTUFile(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "case*.vtu"))
	if err != nil {
		return "", fmt.Errorf("elmer: glob %s/case*.vtu: %w", dir, err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("elmer: no case*.vtu result file found in %s", dir)
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}
