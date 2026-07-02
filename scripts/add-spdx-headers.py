#!/usr/bin/env python3
"""Insert/verify the GPL-2.0-only SPDX header on every Go file in this repo.

This is the oblikovati-elmer add-in (module oblikovati.org/elmer),
GPL-2.0-only throughout. The SHIPPED c-shared library links only the Apache-2.0
contract (oblikovati.org/api); the GPL application module (oblikovati.org) is
pulled in solely by the bridge<->host integration tests. The repo is GPL-2.0-only
regardless, so the mapping is simply "every tracked *.go -> GPL-2.0-only".

Placement rules (so Go semantics are preserved):
  * The SPDX comment is its own block followed by a blank line, so it never merges
    into a following `// Package ...` doc comment.
  * In files beginning with a build constraint (`//go:build` / `// +build`), the
    header goes AFTER the constraint block (the constraint must stay first).
  * Files that already carry an SPDX-License-Identifier are left untouched.

Usage: python3 scripts/add-spdx-headers.py [--check]
  --check exits non-zero if any file would change (for CI), without writing.
"""
import sys
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
IDENTIFIER = "GPL-2.0-only"
# Not part of the GPL Go surface: VCS, tooling, build outputs, the C ABI header
# vendored from the Apache-2.0 api module (keeps its own upstream header), and
# vendor-src — third-party gmsh/ElmerSolver sources under their OWN licenses (GPL-2,
# but stamped by their upstream, not us). See vendor-src/*/NOTICE.md.
SKIP_DIRS = {".git", "scripts", "include", "build", "vendor-src"}


def header(identifier: str) -> str:
    return f"// SPDX-License-Identifier: {identifier}\n"


def is_constraint(line: str) -> bool:
    s = line.lstrip()
    return s.startswith(("//go:build", "// +build"))


def insert_index(lines: list[str]) -> int:
    if lines and is_constraint(lines[0]):
        i = 0
        while i < len(lines) and (is_constraint(lines[i]) or lines[i].strip() == ""):
            i += 1
        return i
    return 0


def patched(text: str, identifier: str) -> str | None:
    if "SPDX-License-Identifier" in text:
        return None
    lines = text.splitlines(keepends=True)
    at = insert_index(lines)
    return "".join(lines[:at] + [header(identifier), "\n"] + lines[at:])


def go_files() -> list[Path]:
    return [
        p
        for p in sorted(ROOT.rglob("*.go"))
        if not any(part in SKIP_DIRS for part in p.relative_to(ROOT).parts)
    ]


def inside_root(path: Path) -> Path:
    """Re-anchor path under the repository root, refusing anything that escapes
    it — a symlinked .go file pointing outside the repo must never be rewritten.
    Canonicalize-then-prefix-check is the sanitization shape the path-injection
    rule (S2083) documents as compliant."""
    real = os.path.realpath(path)
    root = os.path.realpath(ROOT)
    if os.path.commonpath((real, root)) != root:
        raise ValueError(f"refusing to touch {real}: outside the repository root {root}")
    return Path(real)


def main() -> int:
    check = "--check" in sys.argv[1:]
    changed = []
    for path in go_files():
        path = inside_root(path)
        out = patched(path.read_text(), IDENTIFIER)
        if out is None:
            continue
        changed.append(path.relative_to(ROOT))
        if not check:
            # An explicit open() keeps the (root-anchored) path and the written
            # content as separate arguments — Path.write_text models the content
            # as a path in S2083's taint engine and false-positives.
            with open(path, "w", encoding="utf-8") as fh:
                fh.write(out)
    if check and changed:
        print("missing SPDX header:")
        for p in changed:
            print(f"  {p}")
        return 1
    if not check:
        print(f"added SPDX headers to {len(changed)} files")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
