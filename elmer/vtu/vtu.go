// SPDX-License-Identifier: GPL-2.0-only

// Package vtu reads the ASCII VTK UnstructuredGrid (.vtu) files ElmerSolver's
// ResultOutputSolver writes (deck.go's outputSolverSection sets Vtu Format = True,
// Binary Output = False): node coordinates plus every point-data field (displacement,
// stress components, von Mises, ...). It hand-rolls the small subset of the VTU schema
// this add-in needs via encoding/xml rather than pulling in a general VTK library —
// see vendor-src/elmer/test/mesh/case_t0001.vtu for the solver-validated reference dialect
// this reader was built against.
package vtu

import (
	"encoding/xml"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// Result holds one ElmerSolver VTU snapshot: node coordinates in file order, plus every
// PointData field keyed by its original DataArray Name (case preserved — Field does the
// case/separator-insensitive lookup, not storage).
type Result struct {
	Points [][3]float64
	fields map[string]dataArray
}

// dataArray is one PointData DataArray's parsed values, flattened point-major /
// component-minor (a 3-component field's point i occupies values[3*i:3*i+3]), with the
// component count needed to reshape or index into it.
type dataArray struct {
	values []float64
	comps  int
}

// vtkFile is the root element. No XMLName tag is needed: encoding/xml matches the
// top-level element to this struct regardless of its tag name, and the vendored dialect's
// leading "<!-- Elmer version: ... -->" comments are skipped automatically.
type vtkFile struct {
	Grid unstructuredGridXML `xml:"UnstructuredGrid"`
}

type unstructuredGridXML struct {
	Piece pieceXML `xml:"Piece"`
}

// pieceXML models exactly the two child blocks this reader consumes (PointData, Points);
// CellData/Cells are parsed by nothing here — this add-in's M1 slice only renders
// point-sampled fields (ADR-0001's ASCII-only, no-mesh-topology-reuse scope).
type pieceXML struct {
	NumberOfPoints int          `xml:"NumberOfPoints,attr"`
	PointData      pointDataXML `xml:"PointData"`
	Points         pointsXML    `xml:"Points"`
}

type pointDataXML struct {
	Arrays []dataArrayXML `xml:"DataArray"`
}

type pointsXML struct {
	Array dataArrayXML `xml:"DataArray"`
}

// dataArrayXML is one <DataArray>: its declared Name (empty for the Points array),
// component count (0 in the XML means 1, the VTK default for a scalar array), the
// encoding (format; only "ascii" is supported — see parseAsciiArray), and its raw
// whitespace-separated text content.
type dataArrayXML struct {
	Name       string `xml:"Name,attr"`
	Components int    `xml:"NumberOfComponents,attr"`
	Format     string `xml:"format,attr"`
	Text       string `xml:",chardata"`
}

// ReadFile parses path as an ElmerSolver-written ASCII VTU. A binary or appended
// DataArray (format != "ascii") errors, naming the offending field and the fix — this
// add-in's own deck always sets Binary Output = False, so hitting this means that setting
// regressed or the file was hand-edited/produced by a different tool.
//
// Example:
//
//	r, err := vtu.ReadFile("case_t0001.vtu")
//	vonMises, comps, ok := r.Field("von mises")
func ReadFile(path string) (*Result, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vtu: read %s: %w", path, err)
	}
	var doc vtkFile
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("vtu: parse %s as VTU XML: %w", path, err)
	}
	res, err := resultFrom(doc.Grid.Piece)
	if err != nil {
		return nil, fmt.Errorf("vtu: %s: %w", path, err)
	}
	return res, nil
}

// resultFrom converts one parsed <Piece> into a Result, surfacing the first parse error
// from either the Points array or any PointData field.
func resultFrom(p pieceXML) (*Result, error) {
	points, err := parsePoints(p.Points.Array, p.NumberOfPoints)
	if err != nil {
		return nil, err
	}
	fields, err := parseFields(p.PointData.Arrays, len(points))
	if err != nil {
		return nil, err
	}
	return &Result{Points: points, fields: fields}, nil
}

// parsePoints reshapes the Points block's flat 3-component DataArray into one [3]float64
// per node, erroring if the parsed value count isn't a multiple of 3 — a malformed or
// truncated file rather than a real VTU — or if the Piece declares points but the Points
// DataArray came back empty: a truncated/crashed-solver write leaves <Points> absent (its
// zero-valued DataArray parses to 0 values, and 0%3==0 trivially passes), which must not
// be mistaken for a legitimate zero-point mesh.
func parsePoints(arr dataArrayXML, declared int) ([][3]float64, error) {
	vals, err := parseAsciiArray(arr)
	if err != nil {
		return nil, err
	}
	if len(vals)%3 != 0 {
		return nil, fmt.Errorf("vtu: Points array has %d values, want a multiple of 3 (x,y,z per node)", len(vals))
	}
	if len(vals) == 0 && declared > 0 {
		return nil, fmt.Errorf("vtu: Points DataArray is missing or empty, but Piece declares NumberOfPoints=%d", declared)
	}
	pts := make([][3]float64, len(vals)/3)
	for i := range pts {
		pts[i] = [3]float64{vals[3*i], vals[3*i+1], vals[3*i+2]}
	}
	return pts, nil
}

// parseFields parses every PointData DataArray into the fields map, keyed by its original
// Name attribute, erroring if any array's value count doesn't match its declared component
// count times nPoints (the point count parsePoints already established) — a truncated
// field write must not be silently misattributed to a different point/component shape.
func parseFields(arrs []dataArrayXML, nPoints int) (map[string]dataArray, error) {
	fields := make(map[string]dataArray, len(arrs))
	for _, a := range arrs {
		vals, err := parseAsciiArray(a)
		if err != nil {
			return nil, err
		}
		comps := componentsOf(a)
		if err := checkFieldShape(a.Name, len(vals), comps, nPoints); err != nil {
			return nil, err
		}
		fields[a.Name] = dataArray{values: vals, comps: comps}
	}
	return fields, nil
}

// checkFieldShape errors unless a DataArray's parsed value count matches comps*nPoints,
// naming the field and both the got and want counts so a truncated write is diagnosable
// from the error alone.
func checkFieldShape(name string, gotLen, comps, nPoints int) error {
	want := comps * nPoints
	if gotLen != want {
		return fmt.Errorf("vtu: DataArray %q has %d values, want %d (%d comps x %d points)",
			name, gotLen, want, comps, nPoints)
	}
	return nil
}

// componentsOf returns a DataArray's component count, defaulting to 1 (VTK's own default
// for an omitted NumberOfComponents attribute — a scalar field like "vonmises").
func componentsOf(a dataArrayXML) int {
	if a.Components == 0 {
		return 1
	}
	return a.Components
}

// parseAsciiArray parses one DataArray's whitespace-separated Text into float64s,
// rejecting a non-ascii format (binary/appended) up front rather than trying to parse raw
// bytes or base64 as decimal text.
func parseAsciiArray(a dataArrayXML) ([]float64, error) {
	if a.Format != "" && a.Format != "ascii" {
		return nil, fmt.Errorf("vtu: DataArray %q has format %q, want \"ascii\" "+
			"(the deck's ResultOutput solver must set Binary Output = False)", a.Name, a.Format)
	}
	tokens := strings.Fields(a.Text)
	vals := make([]float64, len(tokens))
	for i, s := range tokens {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("vtu: DataArray %q value %d (%q) is not a float: %w", a.Name, i, s, err)
		}
		vals[i] = v
	}
	return vals, nil
}

// Field looks up a point-data field by name, matching case- and space/underscore-
// insensitively so "vonmises", "VonMises", and "von mises" all resolve to the same stored
// field. It returns the field's flattened (point-major, component-minor) values, its
// component count, and whether it was found.
//
// Example:
//
//	vals, comps, ok := r.Field("von mises")
func (r *Result) Field(name string) ([]float64, int, bool) {
	want := normalizeFieldName(name)
	for storedName, d := range r.fields {
		if normalizeFieldName(storedName) == want {
			return d.values, d.comps, true
		}
	}
	return nil, 0, false
}

// normalizeFieldName folds a field name onto a comparable key: lowercase with every space
// and underscore removed, so "VonMises", "von mises", and "von_mises" all normalize to
// "vonmises".
func normalizeFieldName(name string) string {
	stripped := strings.NewReplacer(" ", "", "_", "").Replace(name)
	return strings.ToLower(stripped)
}

// NaNCheck reports an error naming the first field (in Go's randomized map order — a
// non-finite result is a solver-divergence bug regardless of which offending field is
// reported first) containing a NaN or +-Inf value, so a diverged study is diagnosable from
// the field name alone rather than a bare "found a NaN somewhere" message.
//
// Example:
//
//	if err := r.NaNCheck(); err != nil { return fmt.Errorf("study diverged: %w", err) }
func (r *Result) NaNCheck() error {
	for name, d := range r.fields {
		if i, bad := firstNonFinite(d.values); bad {
			return fmt.Errorf("vtu: field %q has a non-finite value at index %d: %v", name, i, d.values[i])
		}
	}
	return nil
}

// firstNonFinite returns the index of the first NaN or Inf value in vals, or (0, false) if
// every value is finite.
func firstNonFinite(vals []float64) (int, bool) {
	for i, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return i, true
		}
	}
	return 0, false
}
