// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"oblikovati.org/api/wire"
	"oblikovati.org/elmer/elmer/femmodel"
)

// studyBoxHost is a fake host serving one box body and a two-face selection (a fixed face
// and a loaded face), enough to drive runStudy/runAndReport end to end through the real
// vendored gmsh mesher with a stubbed solve step. Coordinates are host cm (1 unit = 10 mm,
// matching Oblikovati.AddIns.CalculiX's ccx/hoststudy_test.go boxHost); the box is 20x1x1
// model units, i.e. a 200x10x10 mm beam after the engine's cm->m scaling.
type studyBoxHost struct {
	mu          sync.Mutex
	calls       map[string]int
	lastStatus  string
	oneFaceOnly bool // when true, ModelSelection returns only the fixed face (prereq test)
	box         [8][3]float64
}

func newStudyBoxHost() *studyBoxHost {
	const l, h = 20.0, 1.0
	return &studyBoxHost{
		calls: map[string]int{},
		box: [8][3]float64{
			{0, 0, 0}, {l, 0, 0}, {l, h, 0}, {0, h, 0},
			{0, 0, h}, {l, 0, h}, {l, h, h}, {0, h, h},
		},
	}
}

const (
	studyFixedFaceKey  = "fixed"
	studyLoadedFaceKey = "loaded"
)

func (b *studyBoxHost) Call(method string, req []byte) ([]byte, error) {
	b.mu.Lock()
	b.calls[method]++
	b.mu.Unlock()
	switch method {
	case wire.MethodBodyList:
		return json.Marshal(wire.BodyListResult{Bodies: []wire.BodyInfo{
			{Index: 0, Name: "Solid1", Solid: true, Key: "body0"},
		}})
	case wire.MethodModelSelection:
		return b.selection()
	case wire.MethodBodyCalculateFacets:
		return json.Marshal(b.bodyFacets())
	case wire.MethodFaceCalculateFacets:
		return b.faceFacets(req)
	case wire.MethodStatusSetText:
		return b.recordStatus(req)
	default:
		return []byte("{}"), nil
	}
}

// selection returns the two-face (fixed + loaded) selection, or just the fixed face when
// oneFaceOnly drives the "fewer than 2 faces" prereq test.
func (b *studyBoxHost) selection() ([]byte, error) {
	refs := []string{encodeStudyFaceRef(studyFixedFaceKey), encodeStudyFaceRef(studyLoadedFaceKey)}
	if b.oneFaceOnly {
		refs = refs[:1]
	}
	return json.Marshal(wire.SelectionResult{Count: len(refs), Refs: refs})
}

// recordStatus captures the text of the study's final status.setText call for assertion.
func (b *studyBoxHost) recordStatus(req []byte) ([]byte, error) {
	var args wire.SetStatusTextArgs
	if err := json.Unmarshal(req, &args); err == nil {
		b.mu.Lock()
		b.lastStatus = args.Text
		b.mu.Unlock()
	}
	return []byte("{}"), nil
}

func (b *studyBoxHost) status() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastStatus
}

func (b *studyBoxHost) count(method string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls[method]
}

// bodyFacets returns the whole box surface as a raw triangle soup.
func (b *studyBoxHost) bodyFacets() wire.FacetSetResult {
	quads := [6][4]int{{0, 3, 2, 1}, {4, 5, 6, 7}, {0, 1, 5, 4}, {1, 2, 6, 5}, {2, 3, 7, 6}, {3, 0, 4, 7}}
	var coords []float64
	var idx []int
	for _, q := range quads {
		coords, idx = appendStudyQuad(coords, idx, b.box, q)
	}
	return wire.FacetSetResult{VertexCoordinates: coords, VertexIndices: idx}
}

// faceFacets returns the two triangles of the requested face (the x=0 face for the fixed
// key, the x=L face for the loaded key).
func (b *studyBoxHost) faceFacets(req []byte) ([]byte, error) {
	var args wire.FaceFacetsArgs
	if err := json.Unmarshal(req, &args); err != nil {
		return nil, err
	}
	quad := [4]int{1, 2, 6, 5} // x=L (loaded)
	if args.FaceKey == studyFixedFaceKey {
		quad = [4]int{0, 3, 7, 4} // x=0 (fixed)
	}
	var coords []float64
	var idx []int
	coords, idx = appendStudyQuad(coords, idx, b.box, quad)
	return json.Marshal(wire.FacetSetResult{VertexCoordinates: coords, VertexIndices: idx})
}

// encodeStudyFaceRef mirrors the host's selection encoding: "face/" + url-base64(raw key).
func encodeStudyFaceRef(rawKey string) string {
	return faceRefPrefix + base64.RawURLEncoding.EncodeToString([]byte(rawKey))
}

// appendStudyQuad appends a quad's two triangles to the coordinate/index soup.
func appendStudyQuad(coords []float64, idx []int, v [8][3]float64, q [4]int) ([]float64, []int) {
	base := len(coords) / 3
	for _, c := range q {
		coords = append(coords, v[c][0], v[c][1], v[c][2])
	}
	return coords, append(idx, base, base+1, base+2, base, base+2, base+3)
}

// TestRunAndReportDrivesFullPipelineWithStubbedSolve is the Task-12 end-to-end test: it
// drives Notify(RunStudyCommandID) -> launchStudy -> runAndReport through the REAL
// selection/mesh (vendored gmsh) plumbing, stubbing only the solve step (per the brief's
// stubbable e.solve seam) with a fake that (a) asserts the deck + mesh files already exist
// in the scratch dir when it runs, then (b) synthesizes a matching-size case0001.vtu and
// reports "ALL DONE". It asserts the reported status carries the field range.
func TestRunAndReportDrivesFullPipelineWithStubbedSolve(t *testing.T) {
	requireGmsh(t)

	h := newStudyBoxHost()
	e := NewEngine(h)

	var sawFiles, solveRan bool
	e.solve = func(dir string) (string, error) {
		sawFiles = deckAndMeshFilesExist(t, dir)
		solveRan = true
		coords, err := meshNodeCoordsFrom(dir)
		if err != nil {
			return "", fmt.Errorf("read mesh node coords: %w", err)
		}
		if err := writeFakeVTU(filepath.Join(dir, "case0001.vtu"), coords); err != nil {
			return "", err
		}
		return "MAIN: solving...\nALL DONE\n", nil
	}

	e.Notify(commandStartedEvent(RunStudyCommandID))
	waitIdle(t, e)

	if !solveRan {
		t.Fatal("the solve stub never ran — the study pipeline did not reach the solve step")
	}
	if !sawFiles {
		t.Fatal("deck.sif / mesh.* were not all written to the scratch dir before solve ran")
	}
	status := h.status()
	if !strings.Contains(status, "Elmer:") {
		t.Fatalf("status %q does not report the Elmer study outcome", status)
	}
	if !strings.Contains(status, "..") {
		t.Errorf("status %q does not carry a field range (expected a \"lo..hi\" span)", status)
	}
}

// deckAndMeshFilesExist reports (recording a test error for anything missing) whether the
// deck + all four native mesh-database files are present in dir.
func deckAndMeshFilesExist(t *testing.T, dir string) bool {
	t.Helper()
	ok := true
	for _, name := range []string{"case.sif", "mesh.header", "mesh.nodes", "mesh.elements", "mesh.boundary"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("solve stub: expected %s to already be written: %v", name, err)
			ok = false
		}
	}
	return ok
}

// meshNodeCoordsFrom reads mesh.nodes' coordinates in id order ("id -1 x y z" per line,
// already ascending — see meshfmt.formatNodes) — the exact point order/positions
// render.go's pointIndexForNodes fast path expects a real ElmerSolver VTU to carry, so the
// synthesized VTU fixture below must reuse them rather than inventing placeholder
// coordinates (finding 1, task-12 review: a VTU whose points don't geometrically match the
// mesh now errors instead of being trusted positionally).
func meshNodeCoordsFrom(dir string) ([][3]float64, error) {
	data, err := os.ReadFile(filepath.Join(dir, "mesh.nodes"))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	coords := make([][3]float64, len(lines))
	for i, line := range lines {
		var id, part int
		if _, err := fmt.Sscanf(line, "%d %d %g %g %g", &id, &part, &coords[i][0], &coords[i][1], &coords[i][2]); err != nil {
			return nil, fmt.Errorf("parse mesh.nodes line %d (%q): %w", i+1, line, err)
		}
	}
	return coords, nil
}

// writeFakeVTU synthesizes a minimal valid ElmerSolver-shaped VTU whose points are the mesh's
// own node coordinates (mirroring a real solve's case0001.vtu, which writes points in that
// same ascending-id order — see meshNodeCoordsFrom): a "vonmises" scalar ramping 1..n (so
// Min/Max are distinct and predictable) and a zero-valued "displacement" vector field.
func writeFakeVTU(path string, coords [][3]float64) error {
	var pts, disp, vm strings.Builder
	for i, c := range coords {
		fmt.Fprintf(&pts, " %g %g %g", c[0], c[1], c[2])
		fmt.Fprintf(&disp, " %g %g %g", 0.0, 0.0, 0.0)
		fmt.Fprintf(&vm, " %g", float64(i+1))
	}
	content := fmt.Sprintf(fakeVTUTemplate, len(coords), disp.String(), vm.String(), pts.String())
	return os.WriteFile(path, []byte(content), 0o644)
}

const fakeVTUTemplate = `<?xml version="1.0"?>
<VTKFile type="UnstructuredGrid" version="0.1" byte_order="LittleEndian">
  <UnstructuredGrid>
    <Piece NumberOfPoints="%d" NumberOfCells="0">
      <PointData>
        <DataArray type="Float64" Name="displacement" NumberOfComponents="3" format="ascii">
%s
        </DataArray>
        <DataArray type="Float64" Name="vonmises" NumberOfComponents="1" format="ascii">
%s
        </DataArray>
      </PointData>
      <Points>
        <DataArray type="Float64" NumberOfComponents="3" format="ascii">
%s
        </DataArray>
      </Points>
    </Piece>
  </UnstructuredGrid>
</VTKFile>
`

// TestRunStudyErrorsWhenFewerThanTwoFacesSelected pins the prereq check: fewer than 2
// selected faces errors cleanly, before any mesh/solve work starts (no host calls beyond
// the selection read).
func TestRunStudyErrorsWhenFewerThanTwoFacesSelected(t *testing.T) {
	h := newStudyBoxHost()
	h.oneFaceOnly = true
	e := NewEngine(h)

	_, err := e.runStudy()
	if err == nil || !strings.Contains(err.Error(), "at least 2 faces") {
		t.Fatalf("runStudy error = %v, want a message about needing at least 2 faces", err)
	}
}

// TestRunStudyErrorsOnInvalidMaterialBeforeTouchingTheHost pins the material-sanity prereq
// (Young <= 0): validateStudySettings runs before selectedFaces, so a bad material fails
// with no host calls at all.
func TestRunStudyErrorsOnInvalidMaterialBeforeTouchingTheHost(t *testing.T) {
	h := newStudyBoxHost()
	e := NewEngine(h)
	mat := e.analysis.DefaultMaterial()
	mat.YoungGPa = 0
	e.analysis.SetDefaultMaterial(mat)

	_, err := e.runStudy()
	if err == nil || !strings.Contains(err.Error(), "Young") {
		t.Fatalf("runStudy error = %v, want mention of the invalid Young's modulus", err)
	}
	if got := h.count(wire.MethodModelSelection); got != 0 {
		t.Errorf("runStudy called selection.get %d times before failing validation, want 0", got)
	}
}

// TestValidateStudySettingsRejectsDegradedValues pins validateStudySettings' four guards
// (mesh order, mesh size, Young's modulus, Poisson's ratio), each naming the offending
// value.
func TestValidateStudySettingsRejectsDegradedValues(t *testing.T) {
	good := projectAnalysis(femmodel.NewDefaultAnalysis())
	if err := validateStudySettings(good); err != nil {
		t.Fatalf("default settings should validate cleanly: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*StudySettings)
		want   string
	}{
		{"bad mesh order", func(s *StudySettings) { s.Mesh.Order = 3 }, "order"},
		{"zero mesh size", func(s *StudySettings) { s.Mesh.MaxSizeMM = 0 }, "size"},
		{"zero young's modulus", func(s *StudySettings) { s.Material.YoungGPa = 0 }, "Young"},
		{"poisson at 0.5", func(s *StudySettings) { s.Material.Poisson = 0.5 }, "Poisson"},
		{"negative poisson", func(s *StudySettings) { s.Material.Poisson = -0.1 }, "Poisson"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := good
			c.mutate(&s)
			err := validateStudySettings(s)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("validateStudySettings(%s) = %v, want an error mentioning %q", c.name, err, c.want)
			}
		})
	}
}

// TestDecodeSelectedFacesKeepsOnlyFaceRefsAndDecodesRawKey pins the RAW-key decode gotcha:
// only "face/<base64>" refs survive, decoded back to their raw key; other reference kinds
// are dropped.
func TestDecodeSelectedFacesKeepsOnlyFaceRefsAndDecodesRawKey(t *testing.T) {
	refs := []string{
		encodeStudyFaceRef("faceA"),
		"edge/" + base64.RawURLEncoding.EncodeToString([]byte("edgeB")),
		encodeStudyFaceRef("faceC"),
	}
	got := decodeSelectedFaces(refs)
	want := []string{"faceA", "faceC"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("decodeSelectedFaces(%v) = %v, want %v", refs, got, want)
	}
}

// TestResolveConstraintsForceVsPressure pins resolveConstraints' implicit convention: the
// first face is always fixed; the rest resolve to Force or Pressure boundary ids depending
// on Load.LoadType.
func TestResolveConstraintsForceVsPressure(t *testing.T) {
	bcs := map[string]int{"a": 1, "b": 2, "c": 3}

	force := resolveConstraints(femmodel.LoadDefaults{LoadType: "force"}, bcs, []string{"a", "b", "c"})
	if len(force.Fixed) != 1 || force.Fixed[0] != 1 {
		t.Fatalf("force.Fixed = %v, want [1]", force.Fixed)
	}
	if len(force.Force) != 2 || len(force.Pressure) != 0 {
		t.Fatalf("force resolution = %+v, want 2 Force ids and 0 Pressure ids", force)
	}

	pressure := resolveConstraints(femmodel.LoadDefaults{LoadType: "pressure"}, bcs, []string{"a", "b"})
	if len(pressure.Pressure) != 1 || pressure.Pressure[0] != 2 || len(pressure.Force) != 0 {
		t.Fatalf("pressure resolution = %+v, want Pressure=[2] Force=[]", pressure)
	}
}

// TestLoadDirectionForceVsPressure pins the load arrow direction rule: -Z for a force
// load (deck.go's fixed reference axis), the reversed face normal for a pressure load
// (matching buildDeck's "positive pressure pushes into the face" sign convention).
func TestLoadDirectionForceVsPressure(t *testing.T) {
	force := loadDirection(femmodel.LoadDefaults{LoadType: "force"}, [3]float64{0, 0, 1})
	if force != [3]float64{0, 0, -1} {
		t.Errorf("loadDirection(force) = %v, want [0 0 -1]", force)
	}
	pressure := loadDirection(femmodel.LoadDefaults{LoadType: "pressure"}, [3]float64{1, 0, 0})
	if pressure != [3]float64{-1, 0, 0} {
		t.Errorf("loadDirection(pressure) = %v, want the reversed outward normal [-1 0 0]", pressure)
	}
}

// TestBodyIDsReturnsOneBasedRange pins bodyIDs' contract: n bodies map to Elmer body ids
// 1..n, the same numbering mergeTetMeshes assigns.
func TestBodyIDsReturnsOneBasedRange(t *testing.T) {
	got := bodyIDs(3)
	want := []int{1, 2, 3}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bodyIDs(3) = %v, want %v", got, want)
		}
	}
}

// TestMeshOptionsFromConvertsMillimetresToMetres pins the mm->m unit conversion the
// load-bearing constraint calls out explicitly.
func TestMeshOptionsFromConvertsMillimetresToMetres(t *testing.T) {
	s := StudySettings{Mesh: femmodel.MeshObject{MaxSizeMM: 5, Order: 2}}
	opts := meshOptionsFrom(s)
	if want := 0.005; opts.SizeM < want-1e-12 || opts.SizeM > want+1e-12 {
		t.Errorf("meshOptionsFrom(5mm).SizeM = %v, want %v (5mm -> 0.005m)", opts.SizeM, want)
	}
	if opts.Order != 2 {
		t.Errorf("meshOptionsFrom.Order = %d, want 2", opts.Order)
	}
}

// TestLatestVTUFilePicksLexicographicallyLast pins the "pick the last case*.vtu" rule.
func TestLatestVTUFilePicksLexicographicallyLast(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"case0001.vtu", "case0003.vtu", "case0002.vtu"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := latestVTUFile(dir)
	if err != nil {
		t.Fatalf("latestVTUFile: %v", err)
	}
	if filepath.Base(got) != "case0003.vtu" {
		t.Errorf("latestVTUFile = %q, want .../case0003.vtu", got)
	}
}

// TestLatestVTUFileErrorsWhenNoneFound pins the empty-glob failure path.
func TestLatestVTUFileErrorsWhenNoneFound(t *testing.T) {
	if _, err := latestVTUFile(t.TempDir()); err == nil {
		t.Fatal("latestVTUFile: want an error when no case*.vtu exists")
	}
}

// TestEngineStudyProjectsDefaults proves study() on a freshly constructed Engine matches
// the femmodel package's own default projection — the behavior-preserving guard mirroring
// Oblikovati.AddIns.CalculiX's ccx/engine_study_test.go.
func TestEngineStudyProjectsDefaults(t *testing.T) {
	e := NewEngine(nil)
	got := e.study()
	want := projectAnalysis(femmodel.NewDefaultAnalysis())
	if got != want {
		t.Fatalf("study() drifted from defaults:\n got=%+v\nwant=%+v", got, want)
	}
}
