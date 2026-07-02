// SPDX-License-Identifier: GPL-2.0-only

// Package meshfmt writes Elmer's native ElmerSolver mesh-database format directly from an
// in-memory tetrahedral mesh: mesh.header, mesh.nodes, mesh.elements, mesh.boundary. This skips
// the UNV-write + ElmerGrid-convert round trip the upstream toolchain uses (see decision D3 in
// docs/superpowers/specs/2026-07-02-elmer-addin-port-design.md) — our gmsh tet mesh is written
// straight into the format ElmerSolver reads. The four-file layout, element type codes, and
// float rendering are pinned byte-for-byte against the solver-validated fixture at
// vendor-src/elmer/test/mesh (the CI smoke job solves that exact mesh to the analytic answer);
// see testdata/cube5tet and meshfmt_test.go.
package meshfmt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Mesh is the in-memory ElmerSolver-format volume mesh: node coordinates in meters (node id =
// slice index + 1, so Nodes must be dense/compact — no gaps), tetrahedral volume elements, and
// triangular boundary faces.
type Mesh struct {
	Nodes    [][3]float64
	Tets     []Tet
	Boundary []BoundaryFace
}

// Tet is one volume element: Body is the Elmer body (material region) id; Nodes holds either 4
// (linear, type 504) or 10 (quadratic, type 510) node ids in Elmer's corner+edge order — corners
// 1-4 then edges (1,2),(2,3),(3,1),(1,4),(2,4),(3,4), the same order CalculiX uses for C3D10 (so
// the gmsh tet10 9<->10 swap the CalculiX add-in applies is reused unchanged by the Task 10
// adapter).
type Tet struct {
	Body  int
	Nodes []int
}

// BoundaryFace is one boundary element: a triangular face (3 nodes, type 303) or its quadratic
// counterpart (6 nodes, type 306). Boundary is the Elmer boundary-condition target id; Parent is
// the 1-based id of the parent element (a Tet) that owns the face. Elmer's on-disk format
// reserves a second parent slot for internal (shared) boundaries between two bodies; meshfmt
// only ever writes external faces, so that slot is always emitted as 0.
type BoundaryFace struct {
	Boundary int
	Parent   int
	Nodes    []int
}

// typeCount pairs an Elmer element-type code with how many elements of that type were written; it
// backs mesh.header's "type count" lines.
type typeCount struct {
	Type  int
	Count int
}

// Write emits mesh.header, mesh.nodes, mesh.elements, and mesh.boundary into dir, overwriting any
// existing files. dir must already exist (callers typically pass a fresh t.TempDir() or scratch
// solve directory — Write does not create it).
//
// Example:
//
//	err := meshfmt.Write(dir, meshfmt.Mesh{Nodes: nodes, Tets: tets, Boundary: faces})
func Write(dir string, m Mesh) error {
	elemLines, elemTypes, err := formatElements(m.Tets)
	if err != nil {
		return err
	}
	boundLines, boundTypes, err := formatBoundary(m.Boundary)
	if err != nil {
		return err
	}

	files := map[string]string{
		"mesh.header":   formatHeader(len(m.Nodes), len(m.Tets), len(m.Boundary), elemTypes, boundTypes),
		"mesh.nodes":    formatNodes(m.Nodes),
		"mesh.elements": strings.Join(elemLines, ""),
		"mesh.boundary": strings.Join(boundLines, ""),
	}
	return writeFiles(dir, files)
}

// writeFiles writes each name->content pair into dir, wrapping any failure with the offending
// file name.
func writeFiles(dir string, files map[string]string) error {
	for _, name := range []string{"mesh.header", "mesh.nodes", "mesh.elements", "mesh.boundary"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(files[name]), 0o644); err != nil {
			return fmt.Errorf("meshfmt: write %s: %w", path, err)
		}
	}
	return nil
}

// formatHeader renders mesh.header: node/element/boundary counts, the total distinct element-type
// count, then one "type count" line per volume element type (ascending), followed by one per
// boundary type (ascending) — this group-then-sort order reproduces the solver-validated fixture
// (volume type 504 before boundary type 303) rather than a single global ascending sort.
func formatHeader(nNodes, nElems, nBoundary int, elemTypes, boundTypes []typeCount) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d %d %d\n", nNodes, nElems, nBoundary)
	fmt.Fprintf(&b, "%d\n", len(elemTypes)+len(boundTypes))
	for _, tc := range elemTypes {
		fmt.Fprintf(&b, "%d %d\n", tc.Type, tc.Count)
	}
	for _, tc := range boundTypes {
		fmt.Fprintf(&b, "%d %d\n", tc.Type, tc.Count)
	}
	return b.String()
}

// formatNodes renders mesh.nodes: one "id -1 x y z" line per node, in index order (id = index+1).
// The "-1" column is Elmer's unused node-partition placeholder in serial format.
func formatNodes(nodes [][3]float64) string {
	var b strings.Builder
	for i, n := range nodes {
		fmt.Fprintf(&b, "%d -1 %s %s %s\n", i+1, formatCoord(n[0]), formatCoord(n[1]), formatCoord(n[2]))
	}
	return b.String()
}

// formatCoord renders one coordinate the way the solver-validated fixture does: shortest
// round-trip decimal (strconv 'g' precision -1, which is exact and precision-safe for any
// float64 — never truncates), with a trailing ".0" forced onto integer-valued results so "0"
// reads as "0.0" like the fixture. Elmer's reader parses both forms identically (C's "%le" via
// LoadElmerInput in elmergrid/src/egnative.c), so the ".0" suffix is a golden-fidelity choice,
// not a parser requirement.
func formatCoord(x float64) string {
	s := strconv.FormatFloat(x, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// formatElements renders one mesh.elements line per tet ("id body type node...") and returns the
// per-type counts (ascending by type code) for mesh.header. It errors on the first tet whose node
// count isn't 4 or 10, naming the offending element's 1-based index and its actual node count.
func formatElements(tets []Tet) ([]string, []typeCount, error) {
	lines := make([]string, len(tets))
	counts := map[int]int{}
	for i, t := range tets {
		code, err := tetTypeCode(len(t.Nodes), i)
		if err != nil {
			return nil, nil, err
		}
		counts[code]++
		lines[i] = formatIndexedLine(i+1, t.Body, code, t.Nodes)
	}
	return lines, sortedTypeCounts(counts), nil
}

// formatBoundary renders one mesh.boundary line per face ("id boundary parent 0 type node...")
// and returns the per-type counts (ascending by type code) for mesh.header. It errors on the
// first face whose node count isn't 3 or 6, naming the offending element's 1-based index and its
// actual node count.
func formatBoundary(faces []BoundaryFace) ([]string, []typeCount, error) {
	lines := make([]string, len(faces))
	counts := map[int]int{}
	for i, f := range faces {
		code, err := faceTypeCode(len(f.Nodes), i)
		if err != nil {
			return nil, nil, err
		}
		counts[code]++
		lines[i] = formatBoundaryLine(i+1, f.Boundary, f.Parent, code, f.Nodes)
	}
	return lines, sortedTypeCounts(counts), nil
}

// tetTypeCode derives the Elmer element-type code from a tet's node count (4 -> 504 linear, 10 ->
// 510 quadratic), erroring with the offending 1-based element index and node count for any other
// count.
func tetTypeCode(nodeCount, index int) (int, error) {
	switch nodeCount {
	case 4:
		return 504, nil
	case 10:
		return 510, nil
	default:
		return 0, fmt.Errorf("meshfmt: tet %d has %d node(s), want 4 (linear) or 10 (quadratic)", index+1, nodeCount)
	}
}

// faceTypeCode derives the Elmer element-type code from a boundary face's node count (3 -> 303
// linear, 6 -> 306 quadratic), erroring with the offending 1-based element index and node count
// for any other count.
func faceTypeCode(nodeCount, index int) (int, error) {
	switch nodeCount {
	case 3:
		return 303, nil
	case 6:
		return 306, nil
	default:
		return 0, fmt.Errorf("meshfmt: boundary face %d has %d node(s), want 3 (linear) or 6 (quadratic)", index+1, nodeCount)
	}
}

// sortedTypeCounts turns a type->count map into a slice ordered ascending by type code, so
// mesh.header's per-type lines are deterministic even though map iteration isn't.
func sortedTypeCounts(counts map[int]int) []typeCount {
	types := make([]int, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	sort.Ints(types)
	out := make([]typeCount, len(types))
	for i, t := range types {
		out[i] = typeCount{Type: t, Count: counts[t]}
	}
	return out
}

// formatIndexedLine renders a mesh.elements line: "id body type node...".
func formatIndexedLine(id, body, code int, nodes []int) string {
	fields := append([]int{id, body, code}, nodes...)
	return joinInts(fields) + "\n"
}

// formatBoundaryLine renders a mesh.boundary line: "id boundary parent 0 type node..." — the
// literal 0 is the unused second-parent slot (see BoundaryFace's doc comment).
func formatBoundaryLine(id, boundary, parent, code int, nodes []int) string {
	fields := append([]int{id, boundary, parent, 0, code}, nodes...)
	return joinInts(fields) + "\n"
}

// joinInts renders a slice of ints as a single space-separated line (no trailing newline).
func joinInts(vals []int) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, " ")
}
