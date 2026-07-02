// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// resolveBinary resolves a vendored solver binary shared by gmshrun.go (OBK_GMSH_BIN) and
// solve.go (OBK_ELMER_BIN). Unlike Oblikovati.AddIns.CalculiX's ccx/solve.go
// findSolverBinaries (env-or-vendored-dir, error if neither exists), this add-in adds a
// third tier: a same-named binary on $PATH — gmsh and ElmerSolver are common enough in
// HPC/CAE toolchains that a system install is an acceptable fallback when the repo hasn't
// been built locally (see task-10-brief.md's load-bearing note).
//
// Resolution order: env (a direct path to the binary, or a directory holding name) ->
// defaultPath (relative to the process cwd, e.g. "../vendor-src/gmsh/build/gmsh" from the
// elmer/ package dir under `go test`) -> the first name on $PATH. Every resolved path is
// returned ABSOLUTE (see absPath): a relative defaultPath is only valid for the existence
// check here — os.Stat and exec.Command both resolve a relative path against the CURRENT
// process's cwd, but exec.Cmd.Dir changes a *child's* cwd before it execs, so a relative
// binary path that resolved fine under `go test` (cwd = the elmer/ package dir) silently
// fails to launch once the caller sets cmd.Dir to something else (solve.go's runElmerSolver
// runs ElmerSolver with cmd.Dir = the study's scratch dir) — and inside the host process,
// cwd is not the repo checkout at all. Absolutizing here is the fix for both.
func resolveBinary(env, defaultPath, name string) (string, error) {
	if v := os.Getenv(env); v != "" {
		return resolveFromEnv(env, v, name)
	}
	if _, err := os.Stat(defaultPath); err == nil {
		return absPath(defaultPath), nil
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf(
		"%s binary not found: set %s, build it (make build-solvers), or install %s on PATH",
		name, env, name)
}

// resolveFromEnv interprets an env override as either a direct path to the binary or a
// directory holding it, erroring with the offending env value when neither resolves.
func resolveFromEnv(env, value, name string) (string, error) {
	if fi, err := os.Stat(value); err == nil && !fi.IsDir() {
		return absPath(value), nil
	}
	candidate := filepath.Join(value, name)
	if _, err := os.Stat(candidate); err == nil {
		return absPath(candidate), nil
	}
	return "", fmt.Errorf("%s=%q does not hold the %s binary (expected a direct path or a directory containing it)", env, value, name)
}

// absPath converts path to an absolute path, falling back to the original string if
// os.Getwd fails — a resolved-but-still-relative path is strictly better than dropping the
// binary this function just found.
func absPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
