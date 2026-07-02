// SPDX-License-Identifier: GPL-2.0-only

package sif

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestWriteGoldenMinimal builds a minimal single-body deck — one Simulation key, one Material
// key, one Solver with a FileAttr Procedure, one fixed-displacement boundary condition — and
// compares Write's output byte-for-byte against testdata/golden_minimal.sif, hand-authored to
// the solver-validated dialect (see the package doc comment for the type-word decisions).
func TestWriteGoldenMinimal(t *testing.T) {
	b := NewBuilder()
	b.Simulation("Simulation Type", "Steady State")
	b.Material(1, "Youngs Modulus", 2.1e11)

	solver, err := NewSection(Solver)
	if err != nil {
		t.Fatalf("NewSection(Solver): %v", err)
	}
	solver.Set("Procedure", FileAttr("StressSolve/StressSolver"))
	b.AddSolver(1, solver)

	b.Boundary(1, "Displacement 1", 0.0)

	var buf bytes.Buffer
	if err := Write(&buf, b); err != nil {
		t.Fatalf("Write: %v", err)
	}

	want, err := os.ReadFile("testdata/golden_minimal.sif")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if buf.String() != string(want) {
		t.Errorf("Write output mismatch\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}

// TestWritePreambleOnly pins the fixed Check Keywords/Header preamble in isolation (an empty
// deck must produce exactly the preamble, no dangling separator blank line).
func TestWritePreambleOnly(t *testing.T) {
	b := NewBuilder()
	var buf bytes.Buffer
	if err := Write(&buf, b); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := "Check Keywords \"Warn\"\n\nHeader\n  Mesh DB \".\" \".\"\nEnd\n\n"
	if buf.String() != want {
		t.Errorf("Write(empty builder) = %q, want %q", buf.String(), want)
	}
}

// TestIDAssignmentSharedMaterialSection pins the dedup rule: when the same *Section pointer is
// referenced by two Body sections (hand-built here, bypassing Builder's own one-material-per-
// body convenience), it is emitted exactly once and both bodies resolve to the same numeric id.
func TestIDAssignmentSharedMaterialSection(t *testing.T) {
	material, _ := NewSection(Material)
	material.Set("Density", 1000.0)

	body1, _ := NewSection(Body)
	body1.Set("Target Bodies", []int{1})
	body1.Set("Material", material)

	body2, _ := NewSection(Body)
	body2.Set("Target Bodies", []int{2})
	body2.Set("Material", material)

	b := NewBuilder()
	b.AddSection(body1)
	b.AddSection(material)
	b.AddSection(body2)

	var buf bytes.Buffer
	if err := Write(&buf, b); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	if got := strings.Count(out, "Material = Integer 1"); got != 2 {
		t.Errorf("want both bodies referencing Material id 1 (2 occurrences), got %d in:\n%s", got, out)
	}
	if got := strings.Count(out, "Material 1\n"); got != 1 {
		t.Errorf("want exactly one emitted Material section header, got %d in:\n%s", got, out)
	}
}

// TestArrayFormatting pins the Key(N) = Type v1 v2 ... shape for each supported array element
// type.
func TestArrayFormatting(t *testing.T) {
	s, _ := NewSection(Constants)
	s.Set("Foo Ints", []int{1, 2, 3})
	s.Set("Foo Floats", []float64{1.5, 2.5})
	s.Set("Foo Strings", []string{"a", "b"})

	b := NewBuilder()
	b.AddSection(s)
	var buf bytes.Buffer
	if err := Write(&buf, b); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"  Foo Floats(2) = Real 1.5 2.5\n",
		"  Foo Ints(3) = Integer 1 2 3\n",
		"  Foo Strings(2) = String \"a\" \"b\"\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
}

// TestBooleanFormatting pins the load-bearing rule: booleans always carry the explicit
// `Logical` type word (a bare `True` was proven to break ElmerSolver's parser on a keyword
// outside its built-in table — see the package doc comment).
func TestBooleanFormatting(t *testing.T) {
	s, _ := NewSection(Constants)
	s.Set("Flag True", true)
	s.Set("Flag False", false)

	b := NewBuilder()
	b.AddSection(s)
	var buf bytes.Buffer
	if err := Write(&buf, b); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"  Flag False = Logical False\n", "  Flag True = Logical True\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
}

// TestSectionHas pins Has's contract: false before Set, true after a successful Set.
func TestSectionHas(t *testing.T) {
	s, _ := NewSection(Constants)
	if s.Has("Gravity") {
		t.Fatalf("Has(unset key) = true, want false")
	}
	s.Set("Gravity", -9.81)
	if !s.Has("Gravity") {
		t.Fatalf("Has(set key) = false, want true")
	}
}

// TestNewSectionUnknownName pins the rejection contract: an unrecognized section name errors,
// naming the offending value.
func TestNewSectionUnknownName(t *testing.T) {
	_, err := NewSection("Bogus Section")
	if err == nil || !strings.Contains(err.Error(), "Bogus Section") {
		t.Fatalf("NewSection(%q) error = %v, want mention of the offending name", "Bogus Section", err)
	}
}

// TestRejectEmptyArray pins the empty-array rejection: Set no-ops (sticky error), surfaced only
// when Write is called.
func TestRejectEmptyArray(t *testing.T) {
	s, _ := NewSection(Constants)
	s.Set("Empty", []int{})

	b := NewBuilder()
	b.AddSection(s)
	err := Write(io.Discard, b)
	if err == nil || !strings.Contains(err.Error(), "empty array") {
		t.Fatalf("Write error = %v, want mention of empty array", err)
	}
}

// TestRejectHeterogeneousArray pins the heterogeneous-[]any-array rejection.
func TestRejectHeterogeneousArray(t *testing.T) {
	s, _ := NewSection(Constants)
	s.Set("Mixed", []any{1, "two"})

	b := NewBuilder()
	b.AddSection(s)
	err := Write(io.Discard, b)
	if err == nil || !strings.Contains(err.Error(), "heterogeneous") {
		t.Fatalf("Write error = %v, want mention of heterogeneous array", err)
	}
}

// TestRejectVariableString pins the out-of-scope-formula-BC rejection: a string value starting
// with "Variable" is rejected rather than silently emitted as an unsupported formula BC.
func TestRejectVariableString(t *testing.T) {
	s, _ := NewSection(Constants)
	s.Set("Formula", "Variable Coordinate 1")

	b := NewBuilder()
	b.AddSection(s)
	err := Write(io.Discard, b)
	if err == nil || !strings.Contains(err.Error(), "Variable") {
		t.Fatalf("Write error = %v, want mention of Variable-prefixed strings", err)
	}
}

// TestBuilderConstantAndBodyChildren exercises the Builder convenience methods the golden deck
// doesn't reach — Constant, Equation, BodyForce, and Initial — pinning that each creates its
// section lazily, links it from the owning Body section, and renders with a scalar int/float
// attr formatted correctly.
func TestBuilderConstantAndBodyChildren(t *testing.T) {
	b := NewBuilder()
	b.Constant("Gravity", -9.81)
	b.Equation(1, "Priority", 2)
	b.BodyForce(1, "Stress Bx", 1.0e6)
	b.Initial(1, "Displacement 1", 0.0)

	var buf bytes.Buffer
	if err := Write(&buf, b); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Constants\n  Gravity = Real -9.81\nEnd\n",
		"  Priority = Integer 2\n",
		"  Stress Bx = Real 1e+06\n",
		"  Displacement 1 = Real 0\n",
		"  Body Force = Integer 1\n",
		"  Initial Condition = Integer 1\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
}

// TestAcceptHomogeneousAnySlice pins normalizeAnySlice's success path: a []any whose elements
// all share one supported kind (float64 here) is normalized into a typed array and formatted
// exactly like a native []float64 would be.
func TestAcceptHomogeneousAnySlice(t *testing.T) {
	s, _ := NewSection(Constants)
	s.Set("Nums", []any{1.0, 2.0})

	b := NewBuilder()
	b.AddSection(s)
	var buf bytes.Buffer
	if err := Write(&buf, b); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := "  Nums(2) = Real 1 2\n"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("output missing %q; got:\n%s", want, buf.String())
	}
}

// TestRejectUnsupportedAnyElementType pins normalizeAnySlice's default branch: a []any whose
// first element isn't int/float64/string is rejected even though it's non-empty.
func TestRejectUnsupportedAnyElementType(t *testing.T) {
	s, _ := NewSection(Constants)
	s.Set("Flags", []any{true, false})

	b := NewBuilder()
	b.AddSection(s)
	err := Write(io.Discard, b)
	if err == nil || !strings.Contains(err.Error(), "unsupported array element type") {
		t.Fatalf("Write error = %v, want mention of unsupported array element type", err)
	}
}

// TestRejectUnknownType pins the unknown-value-type rejection: the error names the Go type
// (%T) and the offending key.
func TestRejectUnknownType(t *testing.T) {
	s, _ := NewSection(Constants)
	s.Set("Weird", complex(1, 2))

	b := NewBuilder()
	b.AddSection(s)
	err := Write(io.Discard, b)
	if err == nil || !strings.Contains(err.Error(), "complex128") || !strings.Contains(err.Error(), "Weird") {
		t.Fatalf("Write error = %v, want mention of complex128 and key %q", err, "Weird")
	}
}
