// SPDX-License-Identifier: GPL-2.0-only

// Package gplpurity holds the add-in license-boundary guard (Oblikovati#1614, audit B3).
// It is test-only: the guard runs in the ordinary `go test ./...` sweep.
package gplpurity_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestShippedCodeLinksOnlyApacheAPI fails when any NON-TEST package of this module
// transitively imports the GPL application module. The licensing architecture
// (ADR-0016 C-ABI boundary, ADR-0018 Apache/GPL split) rests on the shipped add-in
// linking only oblikovati.org/api (Apache-2.0) — a single non-test GPL import makes the
// shipped library a GPL derivative and revokes the add-in author's license freedom.
// `go list -deps ./...` covers exactly the non-test import graph, so _test.go-only GPL
// imports (live-test drivers against the host) are deliberately out of scope.
func TestShippedCodeLinksOnlyApacheAPI(t *testing.T) {
	self := strings.TrimSpace(goOutput(t, "list", "-m"))
	for _, dep := range strings.Fields(goOutput(t, "list", "-deps", "./...")) {
		if isOblikovati(dep) && !inModule(dep, self) && !inModule(dep, "oblikovati.org/api") {
			t.Errorf("shipped (non-test) code depends on %q — an add-in links ONLY the Apache-2.0 "+
				"api module; reach the host over api/client instead (ADR-0016/0018, Oblikovati#1614).", dep)
		}
	}
}

// goOutput runs go with args at the module root and returns stdout.
func goOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = ".."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go %v: %v", args, err)
	}
	return string(out)
}

// isOblikovati reports whether dep belongs to any oblikovati.org module (the GPL
// application module is the bare `oblikovati.org` path, so match it exactly too).
func isOblikovati(dep string) bool {
	return dep == "oblikovati.org" || strings.HasPrefix(dep, "oblikovati.org/")
}

// inModule reports whether dep is module mod itself or a package under it.
func inModule(dep, mod string) bool {
	return dep == mod || strings.HasPrefix(dep, mod+"/")
}
