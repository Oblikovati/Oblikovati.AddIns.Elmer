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
// elmer/ package dir under `go test`) -> the first name on $PATH.
func resolveBinary(env, defaultPath, name string) (string, error) {
	if v := os.Getenv(env); v != "" {
		return resolveFromEnv(env, v, name)
	}
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath, nil
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
		return value, nil
	}
	candidate := filepath.Join(value, name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("%s=%q does not hold the %s binary (expected a direct path or a directory containing it)", env, value, name)
}
