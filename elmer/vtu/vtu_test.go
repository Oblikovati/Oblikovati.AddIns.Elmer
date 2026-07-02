// SPDX-License-Identifier: GPL-2.0-only

package vtu

import (
	"strings"
	"testing"
)

// TestReadFileParsesPoints pins the Points parse: mini.vtu's 4-point tetrahedron corners
// come back in file order, reshaped from the flat Points DataArray into [3]float64 rows.
func TestReadFileParsesPoints(t *testing.T) {
	r, err := ReadFile("testdata/mini.vtu")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(r.Points) != 4 {
		t.Fatalf("len(Points) = %d, want 4", len(r.Points))
	}
	want := [3]float64{1, 0, 0}
	if r.Points[1] != want {
		t.Errorf("Points[1] = %v, want %v", r.Points[1], want)
	}
}

// TestFieldCaseAndSeparatorInsensitiveLookup pins Field's matching rule: mini.vtu stores
// the field as "VonMises", but "von mises", "VONMISES", and "von_mises" must all resolve
// to it.
func TestFieldCaseAndSeparatorInsensitiveLookup(t *testing.T) {
	r, err := ReadFile("testdata/mini.vtu")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, query := range []string{"von mises", "VONMISES", "von_mises", "VonMises"} {
		vals, comps, ok := r.Field(query)
		if !ok {
			t.Fatalf("Field(%q) not found", query)
		}
		if comps != 1 {
			t.Errorf("Field(%q) comps = %d, want 1", query, comps)
		}
		want := []float64{1.0e6, 2.0e6, 3.0e6, 4.0e6}
		if len(vals) != len(want) || vals[0] != want[0] || vals[3] != want[3] {
			t.Errorf("Field(%q) values = %v, want %v", query, vals, want)
		}
	}
}

// TestFieldReportsComponentCount pins the 3-component displacement field's shape: 4 points
// x 3 components = 12 flattened values, comps == 3.
func TestFieldReportsComponentCount(t *testing.T) {
	r, err := ReadFile("testdata/mini.vtu")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	vals, comps, ok := r.Field("displacement")
	if !ok {
		t.Fatal("Field(displacement) not found")
	}
	if comps != 3 {
		t.Errorf("comps = %d, want 3", comps)
	}
	if len(vals) != 12 {
		t.Errorf("len(vals) = %d, want 12 (4 points x 3 comps)", len(vals))
	}
}

// TestFieldNotFoundReturnsFalse pins the miss case: an unknown field name returns ok=false
// rather than a zero-valued slice masquerading as a real (empty) field.
func TestFieldNotFoundReturnsFalse(t *testing.T) {
	r, err := ReadFile("testdata/mini.vtu")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if _, _, ok := r.Field("bogus_field"); ok {
		t.Error("Field(bogus_field) ok = true, want false")
	}
}

// TestReadFileMissingFileErrors pins the file-not-found path: ReadFile surfaces os.ReadFile's
// error rather than panicking on a nil/empty read.
func TestReadFileMissingFileErrors(t *testing.T) {
	if _, err := ReadFile("testdata/does-not-exist.vtu"); err == nil {
		t.Fatal("ReadFile(does-not-exist.vtu): want an error, got nil")
	}
}

// TestReadFileMalformedXMLErrors pins the XML-parse-failure path: an unclosed element
// surfaces xml.Unmarshal's error rather than a zero-valued, silently-wrong Result.
func TestReadFileMalformedXMLErrors(t *testing.T) {
	if _, err := ReadFile("testdata/malformed.vtu"); err == nil {
		t.Fatal("ReadFile(malformed.vtu): want an error, got nil")
	}
}

// TestReadFilePointsNotMultipleOfThreeErrors pins the malformed-Points guard: a Points
// DataArray whose value count isn't divisible by 3 (here 2 values for 1 declared point)
// cannot be reshaped into [3]float64 rows and must error rather than silently truncate.
func TestReadFilePointsNotMultipleOfThreeErrors(t *testing.T) {
	_, err := ReadFile("testdata/bad_points.vtu")
	if err == nil || !strings.Contains(err.Error(), "multiple of 3") {
		t.Fatalf("ReadFile(bad_points.vtu) error = %v, want mention of \"multiple of 3\"", err)
	}
}

// TestFieldDefaultsComponentsToOneWhenAttributeOmitted pins componentsOf's VTK-default
// rule: a DataArray with no NumberOfComponents attribute at all (legal VTK, unlike this
// add-in's own deck output which always sets it explicitly) is treated as a 1-component
// scalar field.
func TestFieldDefaultsComponentsToOneWhenAttributeOmitted(t *testing.T) {
	r, err := ReadFile("testdata/scalar_no_comps.vtu")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	_, comps, ok := r.Field("pressure")
	if !ok {
		t.Fatal("Field(pressure) not found")
	}
	if comps != 1 {
		t.Errorf("comps = %d, want 1 (VTK default for an omitted NumberOfComponents)", comps)
	}
}

// TestReadFileRejectsBinaryFormat pins the binary/appended rejection: a DataArray whose
// format attribute isn't "ascii" errors, telling the caller the deck must set Binary
// Output = False (deck.go's outputSolverSection already does — this is a defense-in-depth
// check for a hand-edited or upstream-changed deck).
func TestReadFileRejectsBinaryFormat(t *testing.T) {
	_, err := ReadFile("testdata/binary_format.vtu")
	if err == nil {
		t.Fatal("ReadFile(binary_format.vtu): want an error, got nil")
	}
	if !containsAll(err.Error(), "displacement", "binary", "Binary Output") {
		t.Errorf("error %q should name the field, the offending format, and the fix", err)
	}
}

// TestNaNCheckNamesOffendingField pins NaNCheck's contract: a field containing a NaN value
// fails, and the error names the field by its original (non-normalized) DataArray Name so
// the message is directly greppable in the SIF/VTU dialect.
func TestNaNCheckNamesOffendingField(t *testing.T) {
	r, err := ReadFile("testdata/nan_field.vtu")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	err = r.NaNCheck()
	if err == nil {
		t.Fatal("NaNCheck: want an error for a NaN-containing field, got nil")
	}
	if !containsAll(err.Error(), "VonMises") {
		t.Errorf("NaNCheck error %q should name the offending field VonMises", err)
	}
}

// TestNaNCheckPassesOnCleanResult pins the negative case: mini.vtu carries no NaN/Inf, so
// NaNCheck must return nil.
func TestNaNCheckPassesOnCleanResult(t *testing.T) {
	r, err := ReadFile("testdata/mini.vtu")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := r.NaNCheck(); err != nil {
		t.Errorf("NaNCheck: %v, want nil", err)
	}
}

// containsAll reports whether s contains every one of wants, for assembling one multi-part
// assertion instead of chained single-substring checks.
func containsAll(s string, wants ...string) bool {
	for _, w := range wants {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}
