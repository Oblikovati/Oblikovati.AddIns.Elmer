// SPDX-License-Identifier: GPL-2.0-only

// Package sif builds and writes Elmer solver-input-file (.sif) decks — pure text formatting,
// no engine or oblikovati.org/api imports (Task 8; the ADR lands in Task 14). It is a port of
// upstream Elmer's SIF dialect, hand-verified against the vendored, solver-validated
// vendor-src/elmer/test/case.sif: sections keyed by name plus a per-kind numeric id, attrs
// typed with an explicit SIF type word, all wrapped in a fixed Check Keywords/Header preamble.
//
// Type-word policy (deviates from case.sif's own dialect where the difference is provably
// safe — see the M1 solver smoke): Real/Integer/Logical values ALWAYS carry their type word.
// This is load-bearing for keywords outside ElmerSolver's built-in keyword table: a bare
// `Force 3 Normalize by Area = True` failed to parse; `= Logical True` was required. Strings
// always emit `String "value"` for the same reason, even for keywords case.sif itself writes
// bare (`Coordinate System = Cartesian 3D`). The one exception is FileAttr (`Procedure`,
// `Output File Name`-style two-token file references): case.sif's own
// `Procedure = "StressSolve" "StressSolver"` parses fine untyped, so we match it exactly and
// omit the `File` word. If a later solver run (Task 11/13) rejects the `String`-prefixed form
// for some keyword, adjust here and update this comment — the golden test pins the decision.
package sif

import (
	"fmt"
	"strings"
)

// Section name constants: the exact SIF keyword ElmerSolver expects as a section header word,
// before any per-kind numeric id is appended (e.g. Body + " 1" -> "Body 1"). Simulation and
// Constants are singletons and are never numbered.
const (
	Simulation        = "Simulation"
	Constants         = "Constants"
	Body              = "Body"
	Material          = "Material"
	BodyForce         = "Body Force"
	Equation          = "Equation"
	Solver            = "Solver"
	BoundaryCondition = "Boundary Condition"
	InitialCondition  = "Initial Condition"
	Component         = "Component"
)

// numberedKinds is the set of section names that get a per-kind sequential id at write time
// (1, 2, 3, ... in first-emission order). Simulation and Constants are deliberately absent:
// a SIF deck has exactly one of each, referenced by bare name.
var numberedKinds = map[string]bool{
	Body:              true,
	Material:          true,
	BodyForce:         true,
	Equation:          true,
	Solver:            true,
	BoundaryCondition: true,
	InitialCondition:  true,
	Component:         true,
}

// validNames is every section kind NewSection accepts: the two singletons plus every numbered
// kind, built once so a new kind only needs registering in numberedKinds (or here, for a future
// non-numbered kind).
var validNames = buildValidNames()

func buildValidNames() map[string]bool {
	m := map[string]bool{Simulation: true, Constants: true}
	for k := range numberedKinds {
		m[k] = true
	}
	return m
}

// FileAttr is a two-token file reference (an Elmer "Procedure" or "Output File Name"-shaped
// value): split on "/" into a library/routine (or path/name) pair and rendered as two quoted
// words with no type prefix, matching the solver-validated
// `Procedure = "StressSolve" "StressSolver"` form.
//
// Example:
//
//	s.Set("Procedure", sif.FileAttr("StressSolve/StressSolver"))
type FileAttr string

// sectionRefs holds an ordered list of *Section references under one array-typed key (e.g. an
// Equation section's "Active Solvers" list). It is package-internal: the public Set only
// accepts a single *Section, never a slice of them — Builder.AddSolver is the sole producer,
// via addSectionRef.
type sectionRefs []*Section

// Section is one SIF block: a name (one of the exported kind constants), an optional write-
// order override (Priority, higher first), and a set of key/value attrs written one per line
// sorted by key for deterministic output. Set is sticky-erroring: an invalid value records the
// first error and is not stored, surfaced only when Write is called (Set itself never returns
// one — see the package's Interfaces contract).
type Section struct {
	Name     string
	Priority int
	attrs    map[string]any
	err      error
}

// NewSection creates an empty section of the given kind. It errors — naming the offending
// value — when name is not one of the exported kind constants (Simulation, Constants, Body,
// Material, BodyForce, Equation, Solver, BoundaryCondition, InitialCondition, Component),
// catching typos at construction time rather than producing a malformed deck.
//
// Example:
//
//	s, err := sif.NewSection(sif.Material)
func NewSection(name string) (*Section, error) {
	if !validNames[name] {
		return nil, fmt.Errorf("sif: unknown section name %q, want one of Simulation/Constants/Body/Material/"+
			"Body Force/Equation/Solver/Boundary Condition/Initial Condition/Component", name)
	}
	return &Section{Name: name, attrs: make(map[string]any)}, nil
}

// Has reports whether key was successfully set on the section.
//
// Example:
//
//	if !s.Has("Density") { s.Set("Density", 7900.0) }
func (s *Section) Has(key string) bool {
	_, ok := s.attrs[key]
	return ok
}

// Set assigns key to v. Accepted types: bool, int, float64, string, FileAttr, *Section, []int,
// []float64, []string (plus []any, normalized when every element shares one of those kinds).
// An invalid v (empty array, heterogeneous []any, a "Variable"-prefixed formula string, or any
// other type) is not stored: Set records the first such error on the section and Write reports
// it — Set itself is void so Builder's convenience methods can stay thin passthroughs.
//
// Example:
//
//	s.Set("Youngs Modulus", 2.1e11)
func (s *Section) Set(key string, v any) {
	checked, err := validateValue(key, v)
	if err != nil {
		if s.err == nil {
			s.err = err
		}
		return
	}
	s.attrs[key] = checked
}

// addSectionRef appends ref to the ordered *Section list stored under key, creating the list on
// first use. Package-internal: Builder.AddSolver is the only caller, building an Equation
// section's "Active Solvers" array one referenced Solver section at a time.
func (s *Section) addSectionRef(key string, ref *Section) {
	list, _ := s.attrs[key].(sectionRefs)
	s.attrs[key] = append(list, ref)
}

// validateValue checks that v is one of the types Set documents and normalizes []any into a
// concrete typed slice when every element shares one kind. It returns the value to store and a
// nil error, or a nil value and an error naming the offending key/type/shape.
func validateValue(key string, v any) (any, error) {
	switch t := v.(type) {
	case bool, int, float64, FileAttr, *Section:
		return t, nil
	case string:
		return validateString(key, t)
	case []int:
		return validateArray(key, t)
	case []float64:
		return validateArray(key, t)
	case []string:
		return validateArray(key, t)
	case []any:
		return normalizeAnySlice(key, t)
	default:
		return nil, fmt.Errorf("sif: unsupported value type %T for key %q "+
			"(want bool, int, float64, string, FileAttr, *Section, []int, []float64, or []string)", v, key)
	}
}

// validateString rejects "Variable"-prefixed formula strings (formula BCs are out of scope for
// this deck writer) and passes every other string through unchanged.
func validateString(key, v string) (any, error) {
	if strings.HasPrefix(v, "Variable") {
		return nil, fmt.Errorf("sif: Variable-prefixed formula strings are out of scope for key %q (got %q)", key, v)
	}
	return v, nil
}

// validateArray rejects an empty array (Elmer has no notion of a zero-length attr) and passes
// every non-empty array through unchanged.
func validateArray[T any](key string, arr []T) (any, error) {
	if len(arr) == 0 {
		return nil, fmt.Errorf("sif: empty array for key %q (arrays must have at least one element)", key)
	}
	return arr, nil
}

// normalizeAnySlice converts a []any into a typed []int/[]float64/[]string when every element
// shares the first element's kind, rejecting an empty slice or one with unsupported/mismatched
// element types.
func normalizeAnySlice(key string, arr []any) (any, error) {
	if len(arr) == 0 {
		return nil, fmt.Errorf("sif: empty array for key %q (arrays must have at least one element)", key)
	}
	switch arr[0].(type) {
	case int:
		return convertHomogeneous[int](key, arr)
	case float64:
		return convertHomogeneous[float64](key, arr)
	case string:
		return convertHomogeneous[string](key, arr)
	default:
		return nil, fmt.Errorf("sif: unsupported array element type %T for key %q (want int, float64, or string elements)",
			arr[0], key)
	}
}

// convertHomogeneous converts arr into []T, rejecting the first element whose runtime type
// isn't T (a heterogeneous array).
func convertHomogeneous[T any](key string, arr []any) (any, error) {
	out := make([]T, len(arr))
	for i, e := range arr {
		v, ok := e.(T)
		if !ok {
			return nil, fmt.Errorf("sif: heterogeneous array for key %q: element %d has type %T, want %T", key, i, e, out[0])
		}
		out[i] = v
	}
	return out, nil
}
