// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// solve.go is NEW for this add-in (no ccx equivalent — CalculiX's ccx/solve.go runs ccx
// itself, but its binary-resolution shape and the "trust the log over the exit code"
// idiom, mirrored here from ccx/errcheck.go, are the pattern this file follows).

// elmerDefaultPath is the vendored ElmerSolver binary, relative to this package
// directory (elmer/) — see gmshDefaultPath in gmshrun.go for the same convention.
const elmerDefaultPath = "../vendor-src/elmer/install/bin/ElmerSolver"

// startInfoName is the fixed filename ElmerSolver reads on startup to find its SIF deck.
const startInfoName = "ELMERSOLVER_STARTINFO"

// deckName is the fixed SIF deck filename this add-in always writes (Task 11's sif
// writer), so ELMERSOLVER_STARTINFO's content never varies.
const deckName = "case.sif"

// resolveElmerBin resolves the ElmerSolver binary: $OBK_ELMER_BIN -> the vendored install
// tree -> the first "ElmerSolver" on $PATH.
func resolveElmerBin() (string, error) {
	return resolveBinary("OBK_ELMER_BIN", elmerDefaultPath, "ElmerSolver")
}

// runElmerSolver writes ELMERSOLVER_STARTINFO into dir and runs the resolved ElmerSolver
// binary with cwd=dir, returning its combined stdout+stderr. The binary is rpath'd into
// its own install tree (vendor-src/elmer/install/lib/elmersolver) — it must run from
// there, never be copied elsewhere (vendor-src/elmer/NOTICE.md), which is exactly what
// running it in place via exec.Command achieves. The returned output is handed to
// checkSolverOutput regardless of err: ElmerSolver's exit code alone is not a reliable
// failure signal (see checkSolverOutput's doc comment), so the caller inspects both.
func runElmerSolver(dir string) (string, error) {
	bin, err := resolveElmerBin()
	if err != nil {
		return "", err
	}
	if err := writeStartInfo(dir); err != nil {
		return "", err
	}
	cmd := exec.Command(bin)
	cmd.Dir = dir
	cmd.Env = solverEnv()
	out, err := cmd.CombinedOutput()
	return string(out), launchError(err)
}

// launchError filters exec.CombinedOutput's error down to failures that mean the solver
// never ran at all (binary missing, not executable, ...). A non-zero exit code
// (*exec.ExitError) is NOT surfaced here — checkSolverOutput judges success from the
// captured stdout instead, because ElmerSolver's own exit-code convention is unreliable.
func launchError(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	return fmt.Errorf("launch ElmerSolver: %w", err)
}

// writeStartInfo writes ELMERSOLVER_STARTINFO into dir: a single line naming the SIF
// deck ElmerSolver should load.
func writeStartInfo(dir string) error {
	path := filepath.Join(dir, startInfoName)
	if err := os.WriteFile(path, []byte(deckName+"\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// solverEnv returns the subprocess environment for ElmerSolver: the current environment
// plus ELMER_HOME when OBK_ELMER_HOME is set. ElmerSolver looks up its shared
// solver-library search path from ELMER_HOME; the vendored build's baked-in default may
// not match wherever this repo was checked out, so a caller that knows the real
// vendor-src/elmer/install location can override it.
func solverEnv() []string {
	env := os.Environ()
	if home := os.Getenv("OBK_ELMER_HOME"); home != "" {
		env = append(env, "ELMER_HOME="+home)
	}
	return env
}

// checkSolverOutput inspects the solver's combined stdout for the failure signatures its
// exit code alone doesn't reliably surface (ElmerSolver's own error handling routinely
// keeps running, and exits 0, after printing an ERROR:: line): an "ERROR::" line, a
// missing "ALL DONE" completion marker, or no case*.vtu result file in dir.
// ResultOutputSolver writes its VTU into the "Mesh DB" directory; this add-in's decks set
// Mesh DB "." "." so results land in dir itself — do NOT copy the vendored smoke test's
// "mesh" subdirectory glob (vendor-src/elmer/test/mesh/case_t0001.vtu uses a different
// Mesh DB layout).
func checkSolverOutput(stdout, dir string) error {
	if err := checkForErrorLines(stdout); err != nil {
		return err
	}
	if !strings.Contains(stdout, "ALL DONE") {
		return fmt.Errorf("ElmerSolver did not report ALL DONE; output:\n%s", firstLines(stdout, 3))
	}
	matches, err := filepath.Glob(filepath.Join(dir, "case*.vtu"))
	if err != nil {
		return fmt.Errorf("glob %s/case*.vtu: %w", dir, err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("no case*.vtu result file found in %s", dir)
	}
	return nil
}

// checkForErrorLines returns an error quoting the first three lines starting at the
// first "ERROR::" line in stdout, or nil if none is present.
func checkForErrorLines(stdout string) error {
	lines := strings.Split(stdout, "\n")
	for i, line := range lines {
		if strings.Contains(line, "ERROR::") {
			end := min(i+3, len(lines))
			return fmt.Errorf("ElmerSolver reported an error:\n%s", strings.Join(lines[i:end], "\n"))
		}
	}
	return nil
}

// firstLines returns the first n lines of s, joined back with newlines.
func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
