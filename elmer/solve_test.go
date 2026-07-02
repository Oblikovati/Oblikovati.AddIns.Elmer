// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// solve.go is NEW for this add-in: ElmerSolver's own exit-code behaviour is inconsistent
// about signalling failure, so checkSolverOutput scrapes its combined stdout instead (the
// same "trust the log, not just the exit code" idiom Oblikovati.AddIns.CalculiX's
// ccx/errcheck.go uses for CalculiX's *ERROR lines).

func TestCheckSolverOutput(t *testing.T) {
	cases := []struct {
		name    string
		stdout  string
		touch   string // file to create in dir before checking, "" for none
		wantOK  bool
		wantMsg string
	}{
		{
			name:    "error line",
			stdout:  "MAIN: solving...\nERROR:: LoadMesh: mesh.header not found\nMAIN: done\n",
			touch:   "case0001.vtu",
			wantMsg: "LoadMesh: mesh.header not found",
		},
		{
			name:    "missing ALL DONE",
			stdout:  "MAIN: solving...\nMAIN: done\n",
			touch:   "case0001.vtu",
			wantMsg: "ALL DONE",
		},
		{
			name:    "no vtu file",
			stdout:  "MAIN: solving...\nALL DONE\n",
			touch:   "",
			wantMsg: "case*.vtu",
		},
		{
			name:   "happy path",
			stdout: "MAIN: solving...\nALL DONE\n",
			touch:  "case0001.vtu",
			wantOK: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.touch != "" {
				if err := os.WriteFile(filepath.Join(dir, c.touch), nil, 0o644); err != nil {
					t.Fatalf("touch %s: %v", c.touch, err)
				}
			}
			err := checkSolverOutput(c.stdout, dir)
			if c.wantOK {
				if err != nil {
					t.Fatalf("checkSolverOutput: unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("checkSolverOutput: expected an error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("error %q does not contain %q", err, c.wantMsg)
			}
		})
	}
}

// TestCheckSolverOutputQuotesFirstThreeErrorLines pins the "quote the first three lines"
// requirement precisely: the error should include the ERROR:: line and the two lines that
// follow it, not the whole (potentially huge) solver log.
func TestCheckSolverOutputQuotesFirstThreeErrorLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "case0001.vtu"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := "MAIN: solving...\nERROR:: Solver: singular matrix\ndetail line\nnoise line 1\nnoise line 2\nALL DONE\n"
	err := checkSolverOutput(stdout, dir)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"ERROR:: Solver: singular matrix", "detail line", "noise line 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing expected line %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "noise line 2") {
		t.Errorf("error %q should quote only the first three lines starting at the ERROR:: line, not %q", err, "noise line 2")
	}
}

// fakeSolverScript writes an executable shell script standing in for ElmerSolver: it
// records its cwd + the presence of ELMERSOLVER_STARTINFO into a file in that same cwd,
// echoes the requested stdout, and touches resultFile so checkSolverOutput-style
// assertions can run against it without a real solve.
func fakeSolverScript(t *testing.T, stdout string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake solver script is a POSIX shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-elmersolver.sh")
	script := fmt.Sprintf("#!/bin/sh\ncat ELMERSOLVER_STARTINFO > startinfo.seen\nprintf '%%s' \"$ELMER_HOME\" > elmer_home.seen\nprintf %%s %q\n", stdout)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake solver script: %v", err)
	}
	return path
}

func TestRunElmerSolverWritesStartInfoRunsInDirAndCapturesOutput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OBK_ELMER_BIN", fakeSolverScript(t, "ALL DONE\n"))
	t.Setenv("OBK_ELMER_HOME", "/opt/fake-elmer-home")

	stdout, err := runElmerSolver(dir)
	if err != nil {
		t.Fatalf("runElmerSolver: %v", err)
	}
	if !strings.Contains(stdout, "ALL DONE") {
		t.Errorf("captured stdout = %q, want it to contain %q", stdout, "ALL DONE")
	}

	startinfo, err := os.ReadFile(filepath.Join(dir, "ELMERSOLVER_STARTINFO"))
	if err != nil {
		t.Fatalf("ELMERSOLVER_STARTINFO was not written: %v", err)
	}
	if string(startinfo) != "case.sif\n" {
		t.Errorf("ELMERSOLVER_STARTINFO = %q, want %q", startinfo, "case.sif\n")
	}

	seenStartinfo, err := os.ReadFile(filepath.Join(dir, "startinfo.seen"))
	if err != nil {
		t.Fatalf("fake solver did not see ELMERSOLVER_STARTINFO in its cwd: %v", err)
	}
	if string(seenStartinfo) != "case.sif\n" {
		t.Errorf("fake solver's cwd ELMERSOLVER_STARTINFO = %q, want %q", seenStartinfo, "case.sif\n")
	}

	seenHome, err := os.ReadFile(filepath.Join(dir, "elmer_home.seen"))
	if err != nil {
		t.Fatalf("read elmer_home.seen: %v", err)
	}
	if string(seenHome) != "/opt/fake-elmer-home" {
		t.Errorf("ELMER_HOME seen by the solver = %q, want %q", seenHome, "/opt/fake-elmer-home")
	}
}

// TestRunElmerSolverSwallowsNonZeroExit pins launchError's whole reason for existing:
// ElmerSolver's own exit-code convention is unreliable (it can print ALL DONE and still
// exit non-zero, or vice versa), so runElmerSolver must NOT treat a non-zero exit as a
// launch failure — checkSolverOutput is what judges success, from the captured stdout.
func TestRunElmerSolverSwallowsNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	script := fakeSolverScript(t, "ALL DONE\n")
	failingScript := script + "-failing"
	original, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failingScript, append(original, []byte("\nexit 1\n")...), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OBK_ELMER_BIN", failingScript)

	stdout, err := runElmerSolver(dir)
	if err != nil {
		t.Fatalf("runElmerSolver: a non-zero solver exit must not itself be an error, got: %v", err)
	}
	if !strings.Contains(stdout, "ALL DONE") {
		t.Errorf("captured stdout = %q, want it to still contain %q", stdout, "ALL DONE")
	}
}

// TestLaunchErrorSurfacesNonExitFailures confirms the other half of launchError's
// contract: a failure that means the solver never ran at all (not executable) IS
// surfaced — unlike a non-zero exit, this is not something checkSolverOutput can judge
// from stdout, because there is no stdout. resolveElmerBin resolves the path fine (a
// regular file passes its existence check); it is the OS exec() call itself that fails.
func TestLaunchErrorSurfacesNonExitFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX executable-bit semantics")
	}
	dir := t.TempDir()
	notExecutable := filepath.Join(dir, "not-executable")
	if err := os.WriteFile(notExecutable, []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OBK_ELMER_BIN", notExecutable)

	if _, err := runElmerSolver(t.TempDir()); err == nil {
		t.Fatal("runElmerSolver: expected an error when the resolved binary is not executable")
	}
}

func TestRunElmerSolverErrorsWhenBinaryUnresolved(t *testing.T) {
	t.Setenv("OBK_ELMER_BIN", "")
	t.Setenv("PATH", t.TempDir()) // hide any real ElmerSolver on PATH
	if _, err := os.Stat(elmerDefaultPath); err == nil {
		t.Skip("vendored ElmerSolver is present at the repo-relative default path; resolution would succeed")
	}
	if _, err := runElmerSolver(t.TempDir()); err == nil {
		t.Fatal("runElmerSolver: expected an error when no ElmerSolver binary resolves")
	}
}
