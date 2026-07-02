// SPDX-License-Identifier: GPL-2.0-only

package sif

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Write serializes b as a complete SIF deck to w: the fixed Check Keywords/Header preamble,
// then every section in the order the serialization rules define (custom, Simulation,
// Constants, per body ascending id, boundaries ascending id — stable-sorted by Priority
// descending), each numbered section kind getting a per-kind sequential id. It returns the
// first rejection error recorded by any Set call (see Section.Set) before writing anything, so
// a rejected deck never produces partial output.
//
// Example:
//
//	var buf bytes.Buffer
//	err := sif.Write(&buf, b)
func Write(w io.Writer, b *Builder) error {
	order := buildOrder(b)
	if err := firstError(order); err != nil {
		return err
	}
	ids := assignIDs(order)

	var buf bytes.Buffer
	writePreamble(&buf)
	for i, s := range order {
		writeSection(&buf, s, ids)
		if i < len(order)-1 {
			buf.WriteString("\n")
		}
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// buildOrder assembles the full emission-order section list — custom, Simulation, Constants,
// per body (ascending id), boundaries (ascending id) — deduplicated by *Section identity, then
// stable-sorted by Priority descending.
func buildOrder(b *Builder) []*Section {
	seen := make(map[*Section]bool)
	order := appendUnique(nil, seen, b.custom...)
	order = appendUnique(order, seen, b.simulation)
	order = appendUnique(order, seen, b.constants)
	for _, id := range sortedIntKeys(b.bodyByID) {
		order = appendBodyChain(order, seen, b.bodyByID[id])
	}
	for _, id := range sortedIntKeys(b.boundaries) {
		order = appendUnique(order, seen, b.boundaries[id])
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].Priority > order[j].Priority })
	return order
}

// appendUnique appends every non-nil section in secs not already in seen, in argument order.
func appendUnique(order []*Section, seen map[*Section]bool, secs ...*Section) []*Section {
	for _, s := range secs {
		if s == nil || seen[s] {
			continue
		}
		seen[s] = true
		order = append(order, s)
	}
	return order
}

// appendBodyChain appends one body's sections in the rule's fixed order: Body, Material,
// Equation (+ each Active Solver section), Body Force, Initial Condition.
func appendBodyChain(order []*Section, seen map[*Section]bool, g *bodyGroup) []*Section {
	order = appendUnique(order, seen, g.body)
	order = appendUnique(order, seen, g.material)
	order = appendUnique(order, seen, g.equation)
	order = appendUnique(order, seen, g.solvers...)
	order = appendUnique(order, seen, g.bodyForce)
	order = appendUnique(order, seen, g.initial)
	return order
}

// sortedIntKeys returns m's keys in ascending order, generic over the map's value type so it
// serves both bodyByID and boundaries.
func sortedIntKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// firstError returns the first sticky Set error recorded on any section in order, or nil if
// none of them rejected a value.
func firstError(order []*Section) error {
	for _, s := range order {
		if s.err != nil {
			return s.err
		}
	}
	return nil
}

// assignIDs walks order and gives every numbered-kind section the next id for its kind, in
// first-emission order — the same *Section pointer only ever appears once in order (buildOrder
// dedups), so two attrs referencing it resolve to the same id.
func assignIDs(order []*Section) map[*Section]int {
	ids := make(map[*Section]int, len(order))
	counters := make(map[string]int)
	for _, s := range order {
		if !numberedKinds[s.Name] {
			continue
		}
		counters[s.Name]++
		ids[s] = counters[s.Name]
	}
	return ids
}

// writePreamble writes the fixed Check Keywords/Header block every deck starts with.
func writePreamble(buf *bytes.Buffer) {
	buf.WriteString("Check Keywords \"Warn\"\n\n")
	buf.WriteString("Header\n  Mesh DB \".\" \".\"\nEnd\n\n")
}

// writeSection writes one section's header line, its attrs in sorted-key order, and its
// closing End line.
func writeSection(buf *bytes.Buffer, s *Section, ids map[*Section]int) {
	buf.WriteString(sectionHeader(s, ids))
	for _, k := range sortedKeys(s.attrs) {
		writeAttr(buf, k, s.attrs[k], ids)
	}
	buf.WriteString("End\n")
}

// sectionHeader renders a section's opening line: "<Name> <id>" for a numbered kind, bare
// "<Name>" for a singleton (Simulation, Constants).
func sectionHeader(s *Section, ids map[*Section]int) string {
	if numberedKinds[s.Name] {
		return fmt.Sprintf("%s %d\n", s.Name, ids[s])
	}
	return s.Name + "\n"
}

// sortedKeys returns m's keys in ascending order, giving Write's attr lines a deterministic
// order independent of Go's randomized map iteration.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// writeAttr dispatches key/v to the array or scalar renderer based on v's runtime type.
func writeAttr(buf *bytes.Buffer, key string, v any, ids map[*Section]int) {
	switch v.(type) {
	case []int, []float64, []string, sectionRefs:
		writeArray(buf, key, v, ids)
	default:
		writeScalar(buf, key, v, ids)
	}
}

// writeScalar renders one "  Key = Type value" line for every scalar value type Set accepts.
func writeScalar(buf *bytes.Buffer, key string, v any, ids map[*Section]int) {
	switch t := v.(type) {
	case bool:
		fmt.Fprintf(buf, "  %s = Logical %s\n", key, boolWord(t))
	case int:
		fmt.Fprintf(buf, "  %s = Integer %d\n", key, t)
	case float64:
		fmt.Fprintf(buf, "  %s = Real %s\n", key, formatFloat(t))
	case string:
		fmt.Fprintf(buf, "  %s = String %q\n", key, t)
	case FileAttr:
		writeFileAttr(buf, key, t)
	case *Section:
		fmt.Fprintf(buf, "  %s = Integer %d\n", key, ids[t])
	}
}

// boolWord renders Elmer's Logical literal spelling.
func boolWord(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

// formatFloat renders a Real value with Go's shortest round-tripping representation, per the
// serialization rules (strconv.FormatFloat(v, 'g', -1, 64)).
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// writeFileAttr renders a FileAttr as its "/"-split parts, each quoted, with no type word —
// the one deviation from "always emit a type word" (see the package doc comment).
func writeFileAttr(buf *bytes.Buffer, key string, fa FileAttr) {
	parts := strings.Split(string(fa), "/")
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = strconv.Quote(p)
	}
	fmt.Fprintf(buf, "  %s = %s\n", key, strings.Join(quoted, " "))
}

// writeArray dispatches key/v to the per-element-type array renderer.
func writeArray(buf *bytes.Buffer, key string, v any, ids map[*Section]int) {
	switch t := v.(type) {
	case []int:
		writeIntArray(buf, key, t)
	case []float64:
		writeFloatArray(buf, key, t)
	case []string:
		writeStringArray(buf, key, t)
	case sectionRefs:
		writeSectionRefArray(buf, key, t, ids)
	}
}

// writeIntArray renders "  Key(N) = Integer v1 v2 ...".
func writeIntArray(buf *bytes.Buffer, key string, vals []int) {
	strs := make([]string, len(vals))
	for i, v := range vals {
		strs[i] = strconv.Itoa(v)
	}
	fmt.Fprintf(buf, "  %s(%d) = Integer %s\n", key, len(vals), strings.Join(strs, " "))
}

// writeFloatArray renders "  Key(N) = Real v1 v2 ...".
func writeFloatArray(buf *bytes.Buffer, key string, vals []float64) {
	strs := make([]string, len(vals))
	for i, v := range vals {
		strs[i] = formatFloat(v)
	}
	fmt.Fprintf(buf, "  %s(%d) = Real %s\n", key, len(vals), strings.Join(strs, " "))
}

// writeStringArray renders "  Key(N) = String "v1" "v2" ...".
func writeStringArray(buf *bytes.Buffer, key string, vals []string) {
	strs := make([]string, len(vals))
	for i, v := range vals {
		strs[i] = strconv.Quote(v)
	}
	fmt.Fprintf(buf, "  %s(%d) = String %s\n", key, len(vals), strings.Join(strs, " "))
}

// writeSectionRefArray renders "  Key(N) = Integer id1 id2 ...", resolving each referenced
// section's per-kind id from ids.
func writeSectionRefArray(buf *bytes.Buffer, key string, refs sectionRefs, ids map[*Section]int) {
	strs := make([]string, len(refs))
	for i, r := range refs {
		strs[i] = strconv.Itoa(ids[r])
	}
	fmt.Fprintf(buf, "  %s(%d) = Integer %s\n", key, len(refs), strings.Join(strs, " "))
}
