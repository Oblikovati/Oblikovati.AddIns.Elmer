// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestResolveBinary exercises resolveBinary's three tiers directly (env override, default
// path, $PATH) with synthetic paths — deterministic, unlike testing resolveGmshBin /
// resolveElmerBin themselves, which depend on whether this checkout has actually run
// vendor-src/*/build.sh.
func TestResolveBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX executable bits")
	}
	dir := t.TempDir()
	binPath := filepath.Join(dir, "tool")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("env points directly at the binary", func(t *testing.T) {
		t.Setenv("OBK_TEST_BIN", binPath)
		got, err := resolveBinary("OBK_TEST_BIN", "/does/not/exist/tool", "tool")
		if err != nil || got != binPath {
			t.Errorf("resolveBinary = (%q, %v), want (%q, nil)", got, err, binPath)
		}
	})

	t.Run("env points at a directory holding the binary", func(t *testing.T) {
		t.Setenv("OBK_TEST_BIN", dir)
		got, err := resolveBinary("OBK_TEST_BIN", "/does/not/exist/tool", "tool")
		if err != nil || got != binPath {
			t.Errorf("resolveBinary = (%q, %v), want (%q, nil)", got, err, binPath)
		}
	})

	t.Run("env set but wrong", func(t *testing.T) {
		t.Setenv("OBK_TEST_BIN", filepath.Join(dir, "does-not-exist"))
		_, err := resolveBinary("OBK_TEST_BIN", "/does/not/exist/tool", "tool")
		if err == nil {
			t.Fatal("expected an error for a bad OBK_TEST_BIN")
		}
	})

	t.Run("falls back to the default path", func(t *testing.T) {
		t.Setenv("OBK_TEST_BIN", "")
		got, err := resolveBinary("OBK_TEST_BIN", binPath, "tool")
		if err != nil || got != binPath {
			t.Errorf("resolveBinary = (%q, %v), want (%q, nil)", got, err, binPath)
		}
	})

	t.Run("falls back to PATH", func(t *testing.T) {
		t.Setenv("OBK_TEST_BIN", "")
		t.Setenv("PATH", dir)
		got, err := resolveBinary("OBK_TEST_BIN", "/does/not/exist/tool", "tool")
		if err != nil || got != binPath {
			t.Errorf("resolveBinary = (%q, %v), want (%q, nil)", got, err, binPath)
		}
	})

	t.Run("nothing resolves", func(t *testing.T) {
		t.Setenv("OBK_TEST_BIN", "")
		t.Setenv("PATH", t.TempDir())
		_, err := resolveBinary("OBK_TEST_BIN", "/does/not/exist/tool", "tool")
		if err == nil {
			t.Fatal("expected an error when no tier resolves")
		}
	})
}
