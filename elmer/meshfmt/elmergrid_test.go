// SPDX-License-Identifier: GPL-2.0-only

package meshfmt

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// elmerGridPath is the vendored ElmerGrid binary Task 5 builds from source
// (vendor-src/elmer/build.sh), relative to this package directory.
const elmerGridPath = "../../vendor-src/elmer/install/bin/ElmerGrid"

// requireElmerGrid skips the test when the vendored ElmerGrid binary hasn't been built — a
// file-existence guard rather than a build tag, per the task brief: CI's solvers job always has
// it; a bare `go test` on a dev machine that hasn't run vendor-src/elmer/build.sh does not.
func requireElmerGrid(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(elmerGridPath)
	if err != nil {
		t.Fatalf("filepath.Abs(%s): %v", elmerGridPath, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("vendored ElmerGrid not found at %s (run vendor-src/elmer/build.sh first): %v", path, err)
	}
	return path
}

// assertElmerGridRoundTrips runs `ElmerGrid 2 2 <goldenDir> -out <tmp>` (ElmerSolver-format in,
// ElmerSolver-format out) in place against the committed golden and requires exit 0 — an
// independent, upstream-authored cross-check that meshfmt's byte format (including the
// quadratic node/edge order) is one the real Elmer toolchain accepts, not just something our own
// golden happens to match.
func assertElmerGridRoundTrips(t *testing.T, goldenDir string) {
	t.Helper()
	bin := requireElmerGrid(t)

	in, err := filepath.Abs(goldenDir)
	if err != nil {
		t.Fatalf("filepath.Abs(%s): %v", goldenDir, err)
	}
	out := t.TempDir()

	cmd := exec.Command(bin, "2", "2", in, "-out", out) //nolint:gosec // fixed vendored binary, test-only
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ElmerGrid 2 2 %s -out %s failed: %v\n%s", in, out, err, output)
	}
}

// TestCube5TetElmerGridRoundTrip cross-checks the linear-tet golden (type 504/303) against the
// real vendored ElmerGrid.
func TestCube5TetElmerGridRoundTrip(t *testing.T) {
	assertElmerGridRoundTrips(t, filepath.Join("testdata", "cube5tet"))
}

// TestOrder2Tet10ElmerGridRoundTrip cross-checks the quadratic-tet golden (type 510/306) —
// including the hand-computed edge-midpoint node order — against the real vendored ElmerGrid.
func TestOrder2Tet10ElmerGridRoundTrip(t *testing.T) {
	assertElmerGridRoundTrips(t, filepath.Join("testdata", "order2tet10"))
}
