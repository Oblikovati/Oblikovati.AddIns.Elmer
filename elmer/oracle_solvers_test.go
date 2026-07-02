// SPDX-License-Identifier: GPL-2.0-only

//go:build solvers

package elmer

import (
	"math"
	"testing"

	"oblikovati.org/elmer/elmer/femmodel"
	"oblikovati.org/elmer/elmer/meshfmt"
	"oblikovati.org/elmer/elmer/vtu"
)

// oracle_solvers_test.go is Task 13: the M1 pipeline's permanent physics gate. Both tests
// drive the model-level pipeline directly (buildDeck + meshfmt.Write + runElmerSolver +
// checkSolverOutput + vtu.ReadFile) against the REAL vendored gmsh + ElmerSolver binaries —
// no Engine, no fake host — mirroring Oblikovati.AddIns.CalculiX's ccx/cantilever_test.go
// box-builder idiom (boxSurface/weldSurface from gmshrun_test.go's requireGmsh sibling) but
// stopping one layer lower than ccx's own solveStudyDeck, since this add-in's pipeline
// functions (exportMesh, buildDeck, ...) are free functions rather than one wrapped call.
// Geometry is built directly in metres (matching gmshrun_test.go's boxSurface convention,
// e.g. "a 100 mm box in metres") rather than routed through hostmesh.go's host-cm->metre
// scaling, since there is no host in this test at all — that scaling only applies to the
// real Engine.pullSurface path this test deliberately bypasses.

// oracleLengthM / oracleSideM is the brief's 80x20x20 mm steel box (L/h = 4 — not slender,
// so shear deformation measurably widens the Euler-Bernoulli gap; each test's tolerance
// accounts for this). oracleMeshSizeM is the brief's 5 mm target element size.
const (
	oracleLengthM   = 0.080
	oracleSideM     = 0.020
	oracleMeshSizeM = 0.005
)

// requireElmerAndGmsh skips the test when either vendored binary cannot be resolved.
// resolveElmerBin/resolveGmshBin each check env -> the vendored build output relative to
// the package dir -> $PATH (binresolve.go) — calling them directly (rather than checking
// only $OBK_ELMER_BIN/$OBK_GMSH_BIN) is deliberate: under `go test` with no env set at all,
// the vendored-path tier still resolves a locally built binary, so an env-only guard would
// wrongly skip a perfectly runnable local test. This mirrors requireGmsh (gmshrun_test.go)
// and ccx's requireSolver (volumemesh_test.go), extended to also require ElmerSolver.
func requireElmerAndGmsh(t *testing.T) {
	t.Helper()
	if _, err := resolveElmerBin(); err != nil {
		t.Skipf("ElmerSolver not available: %v", err)
	}
	requireGmsh(t)
}

// oracleMesh volume-meshes the shared 80x20x20 mm box (steel, quadratic tets, 5 mm) with
// the vendored gmsh, writing its scratch files into dir.
func oracleMesh(t *testing.T, dir string) *TetMesh {
	t.Helper()
	coords, idx := boxSurface(oracleLengthM, oracleSideM, oracleSideM)
	surface, err := weldSurface(coords, idx)
	if err != nil {
		t.Fatalf("weld: %v", err)
	}
	opts := MeshOptions{SizeM: oracleMeshSizeM, Order: 2}
	mesh, err := NewGmshMesher(requireGmsh(t)).Mesh(surface, opts, dir)
	if err != nil {
		t.Fatalf("mesh: %v", err)
	}
	return mesh
}

// oracleFaceGroups binds the box's two end caps (x=0 "fixed", x=L "loaded") directly by
// geometry, using the same boundary grouping buildFaceGroups uses internally
// (groupBoundaryByFace) but skipping its host-facet matching step entirely — there is no
// host facet to match against in this model-level test, only the mesh's own surface.
func oracleFaceGroups(t *testing.T, mesh *TetMesh) *FaceGroups {
	t.Helper()
	groups := groupBoundaryByFace(mesh)
	faceIndex := faceElemIndex(mesh)
	fixed := oracleEndFace(t, groups, 0)
	loaded := oracleEndFace(t, groups, oracleLengthM)
	return &FaceGroups{
		Nodes: map[string][]int{"fixed": fixed.nodeList(), "loaded": loaded.nodeList()},
		ElemFaces: map[string][]ElemFace{
			"fixed":  resolveElemFaces(fixed.facets, faceIndex),
			"loaded": resolveElemFaces(loaded.facets, faceIndex),
		},
		Normals: map[string][3]float64{"fixed": fixed.normal(), "loaded": loaded.normal()},
	}
}

// oracleEndFace returns the boundary group whose centroid sits at x == wantX, within a
// mesh-size slack that safely separates the two end caps (80 mm apart) from the four side
// faces (whose centroid x sits at L/2, 40 mm from either end).
func oracleEndFace(t *testing.T, groups map[int]*faceAgg, wantX float64) *faceAgg {
	t.Helper()
	for _, agg := range groups {
		if math.Abs(agg.centroid()[0]-wantX) < oracleMeshSizeM {
			return agg
		}
	}
	t.Fatalf("no boundary face group found with centroid x ~= %g", wantX)
	return nil
}

// oracleSettings projects the femmodel default analysis (steel, order-2/5mm mesh — already
// exactly the brief's material and mesh, see femmodel.newDefaultMaterial/newDefaultMesh)
// with load swapped to the caller's.
func oracleSettings(load femmodel.LoadDefaults) StudySettings {
	a := femmodel.NewDefaultAnalysis()
	a.SetLoadDefaults(load)
	return projectAnalysis(a)
}

// oracleSolve runs the model-level pipeline (export -> meshfmt.Write -> buildDeck ->
// runElmerSolver -> checkSolverOutput -> vtu.ReadFile) in dir and returns the parsed,
// NaN-checked result.
func oracleSolve(t *testing.T, dir string, s StudySettings, mesh *TetMesh, groups *FaceGroups) *vtu.Result {
	t.Helper()
	bound := []BoundFace{{Key: "fixed"}, {Key: "loaded"}}
	meshOut, bcs, err := exportMesh(mesh, groups, bound)
	if err != nil {
		t.Fatalf("exportMesh: %v", err)
	}
	if err := meshfmt.Write(dir, meshOut); err != nil {
		t.Fatalf("meshfmt.Write: %v", err)
	}
	writeOracleDeck(t, dir, s, bcs)
	return readOracleResult(t, dir)
}

// writeOracleDeck resolves the fixed/loaded constraints and writes the SIF deck into dir.
func writeOracleDeck(t *testing.T, dir string, s StudySettings, bcs map[string]int) {
	t.Helper()
	resolved := resolveConstraints(s.Load, bcs, []string{"fixed", "loaded"})
	b, err := buildDeck(s, bodyIDs(1), bcs, resolved)
	if err != nil {
		t.Fatalf("buildDeck: %v", err)
	}
	if err := writeDeckFiles(dir, b); err != nil {
		t.Fatalf("writeDeckFiles: %v", err)
	}
}

// readOracleResult runs the real vendored ElmerSolver in dir, checks its output for the
// known failure signatures, and parses + NaN-checks the newest case*.vtu it wrote.
func readOracleResult(t *testing.T, dir string) *vtu.Result {
	t.Helper()
	stdout, err := runElmerSolver(dir)
	if err != nil {
		t.Fatalf("runElmerSolver: %v", err)
	}
	if err := checkSolverOutput(stdout, dir); err != nil {
		t.Fatalf("checkSolverOutput: %v\nsolver stdout:\n%s", err, stdout)
	}
	path, err := latestVTUFile(dir)
	if err != nil {
		t.Fatalf("latestVTUFile: %v", err)
	}
	res, err := vtu.ReadFile(path)
	if err != nil {
		t.Fatalf("vtu.ReadFile: %v", err)
	}
	if err := res.NaNCheck(); err != nil {
		t.Fatalf("NaNCheck: %v", err)
	}
	return res
}

// peakDisplacementM returns the largest displacement-vector magnitude across every point
// in the result's "displacement" field.
func peakDisplacementM(t *testing.T, res *vtu.Result) float64 {
	t.Helper()
	vals, comps, ok := res.Field("displacement")
	if !ok || comps != 3 {
		t.Fatalf("no 3-component displacement field in the VTU")
	}
	peak := 0.0
	for i := 0; i < len(vals)/3; i++ {
		x, y, z := vals[3*i], vals[3*i+1], vals[3*i+2]
		if m := math.Sqrt(x*x + y*y + z*z); m > peak {
			peak = m
		}
	}
	return peak
}

// meanAxialDisplacement returns the mean X-component ("axial", along the bar's length) of
// the displacement field over nodeIDs, resolved to their VTU point index through the same
// geometry-verified node<->point correspondence render.go's pointIndexForNodes relies on
// (this add-in's deck sets "Fixed Mesh = True", so VTU points sit at the mesh's own
// undeformed coordinates — see deck.go's outputSolverSection doc comment).
func meanAxialDisplacement(t *testing.T, mesh *TetMesh, res *vtu.Result, nodeIDs []int) float64 {
	t.Helper()
	vals, comps, ok := res.Field("displacement")
	if !ok || comps != 3 {
		t.Fatalf("no 3-component displacement field in the VTU")
	}
	ptIdx, err := pointIndexForNodes(mesh, res.Points)
	if err != nil {
		t.Fatalf("pointIndexForNodes: %v", err)
	}
	sum := 0.0
	for _, id := range nodeIDs {
		sum += vals[3*ptIdx[id]]
	}
	return sum / float64(len(nodeIDs))
}

// TestOracleCantileverMatchesEulerBernoulli is oracle 1: an 80x20x20 mm steel cantilever,
// fixed at x=0, a 1000 N -Z force on the x=L end face, tet order 2, 5 mm mesh. The analytic
// tip deflection is Euler-Bernoulli beam theory:
//
//	delta = F*L^3 / (3*E*I),  I = b*h^3/12
//	      = 1000 * 0.08^3 / (3 * 210e9 * (0.02^4/12)) = 6.095e-5 m
//
// Measured live against the vendored solvers (2026-07-03): peak |displacement| =
// 6.37549e-05 m, 4.60% above the analytic value — within the brief's 5% tolerance and in
// the expected direction (shear deformation on this non-slender L/h=4 box, plus the
// clamped-face stiffening at the fixed end, makes the FE answer larger than the pure-
// bending Euler-Bernoulli prediction; ccx's own cantilever_test.go documents the same
// effect on an L/h=20 beam and budgets a much wider 12% tolerance for it — this box is 5x
// less slender, so the measured 4.6% gap is consistent with, not surprising given, the
// brief's 5% budget).
func TestOracleCantileverMatchesEulerBernoulli(t *testing.T) {
	requireElmerAndGmsh(t)
	const (
		youngPa = 210e9
		forceN  = 1000.0
	)
	dir := t.TempDir()
	mesh := oracleMesh(t, dir)
	groups := oracleFaceGroups(t, mesh)
	settings := oracleSettings(femmodel.LoadDefaults{LoadType: "force", LoadN: forceN})
	res := oracleSolve(t, dir, settings, mesh, groups)

	peak := peakDisplacementM(t, res)
	inertia := oracleSideM * oracleSideM * oracleSideM * oracleSideM / 12.0
	want := forceN * oracleLengthM * oracleLengthM * oracleLengthM / (3.0 * youngPa * inertia)
	relErr := math.Abs(peak-want) / want
	t.Logf("cantilever peak displacement: FE=%.6g m, Euler-Bernoulli=%.6g m, rel err=%.2f%%", peak, want, relErr*100)
	if relErr > 0.05 {
		t.Errorf("peak displacement %.6g m differs from analytic %.6g m by %.1f%% (>5%%)", peak, want, relErr*100)
	}
}

// TestOraclePressureBarShortensUnderCompression is oracle 2: the same 80x20x20 mm steel
// box, fixed at x=0, a 10 MPa compressive pressure on the x=L end face. The analytic axial
// shortening is delta = p*L/E = 10e6 * 0.08 / 210e9 = 3.8095e-6 m. This also pins the sign
// convention writePressureBCs relies on (equations/elasticity.go: "Normal Force = -p"): a
// positive user-facing pressure must physically compress (shorten), not stretch, the bar.
//
// Measured live against the vendored solvers (2026-07-03): peak |displacement| =
// 3.78374e-06 m (0.68% below analytic, well within the brief's 2% tolerance), mean axial
// (X) displacement of the loaded face = -3.77822e-06 m — negative, i.e. the loaded face
// moved toward the fixed support: the bar shortened, confirming the Normal Force = -p
// sign convention (equations/elasticity.go's writePressureBCs) is physically correct.
func TestOraclePressureBarShortensUnderCompression(t *testing.T) {
	requireElmerAndGmsh(t)
	const (
		youngPa     = 210e9
		pressureMPa = 10.0
		pressurePa  = pressureMPa * 1.0e6
	)
	dir := t.TempDir()
	mesh := oracleMesh(t, dir)
	groups := oracleFaceGroups(t, mesh)
	settings := oracleSettings(femmodel.LoadDefaults{LoadType: "pressure", PressureMPa: pressureMPa})
	res := oracleSolve(t, dir, settings, mesh, groups)

	peak := peakDisplacementM(t, res)
	want := pressurePa * oracleLengthM / youngPa
	relErr := math.Abs(peak-want) / want
	t.Logf("pressure bar peak displacement: FE=%.6g m, analytic=%.6g m, rel err=%.2f%%", peak, want, relErr*100)
	if relErr > 0.02 {
		t.Errorf("peak displacement %.6g m differs from analytic %.6g m by %.1f%% (>2%%)", peak, want, relErr*100)
	}

	mean := meanAxialDisplacement(t, mesh, res, groups.Nodes["loaded"])
	t.Logf("pressure bar loaded-face mean axial displacement: %.6g m", mean)
	if mean >= 0 {
		t.Errorf("loaded face mean axial displacement = %.6g m, want negative (bar shortens under "+
			"compression; a positive value would mean the Normal Force = -p sign convention is inverted)", mean)
	}
}
