// SPDX-License-Identifier: GPL-2.0-only

package elmer

import (
	"fmt"
	"os"
	"path/filepath"

	"oblikovati.org/elmer/elmer/equations"
	"oblikovati.org/elmer/elmer/femmodel"
	"oblikovati.org/elmer/elmer/sif"
)

// mpaToPa converts a pressure from MPa (femmodel.LoadDefaults' unit) to the Pascals the SIF
// dialect expects.
const mpaToPa = 1.0e6

// coordinateSystemWords maps femmodel's neutral, lowercase coordinate-system enum onto the
// exact SIF keyword casing ElmerSolver expects (Task 7's doc comment: "the pipeline layer
// maps them onto Elmer's own SIF keyword vocabulary").
var coordinateSystemWords = map[string]string{"cartesian 3D": "Cartesian 3D"}

// simulationTypeWords maps femmodel's neutral simulation-type enum onto its SIF keyword
// casing, the same convention as coordinateSystemWords.
var simulationTypeWords = map[string]string{"steady state": "Steady State"}

// resolvedConstraints groups mesh boundary ids already resolved from host face keys
// (Task 12's constraint-resolution job) by the BC kind the deck writer applies: fixed
// (zero all displacement DOFs), force (a load vector), and pressure (a scalar normal
// load). One boundary id may appear in at most one of these lists.
type resolvedConstraints struct{ Fixed, Force, Pressure []int }

// buildDeck assembles a complete elasticity SIF deck: the Simulation and Constants
// sections, the shared ResultOutput solver, and — via equations.WriteElasticity — every
// body's material, the shared stress solver, and the resolved boundary conditions. bcs
// (face key -> boundary id, exportMesh's own output) validates that every id in resolved
// was actually assigned to a mesh boundary, catching a Task-12 resolution that drifted
// from the mesh export before it reaches ElmerSolver as a dangling Target Boundaries
// reference.
//
// Example:
//
//	b, err := buildDeck(settings, []int{1}, bcs, resolvedConstraints{Fixed: []int{1}, Force: []int{2}})
func buildDeck(s StudySettings, bodies []int, bcs map[string]int, resolved resolvedConstraints) (*sif.Builder, error) {
	if err := validateResolvedIDs(bcs, resolved); err != nil {
		return nil, err
	}
	b := sif.NewBuilder()
	if err := writeSimulation(b, s.Simulation); err != nil {
		return nil, err
	}
	writeConstants(b)
	in := elasticityInputFrom(s, bodies, resolved, outputSolverSection())
	if err := equations.WriteElasticity(b, in); err != nil {
		return nil, err
	}
	return b, nil
}

// elasticityInputFrom projects StudySettings + resolved boundary ids into the
// equations.ElasticityInput WriteElasticity consumes, deriving the force load vector from
// the study's scalar load default (see forceVector) and the pressure load from its MPa
// magnitude.
func elasticityInputFrom(s StudySettings, bodies []int, resolved resolvedConstraints, outSolver *sif.Section) equations.ElasticityInput {
	return equations.ElasticityInput{
		Bodies:       bodies,
		Material:     s.Material,
		Eq:           s.Equation,
		FixedBIDs:    resolved.Fixed,
		ForceBIDs:    resolved.Force,
		ForceN:       forceVector(s.Load),
		PressBIDs:    resolved.Pressure,
		PressurePa:   s.Load.PressureMPa * mpaToPa,
		OutputSolver: outSolver,
	}
}

// forceVector projects LoadDefaults' scalar magnitude onto the M1 reference load axis:
// -Z, matching the M0/M1 plan's cantilever oracle (a downward bending load applied to the
// free end). A later milestone that adds a direction field to LoadDefaults replaces this.
func forceVector(l femmodel.LoadDefaults) [3]float64 {
	return [3]float64{0, 0, -l.LoadN}
}

// validateResolvedIDs rejects any boundary id in resolved that is not one of bcs' assigned
// values — an id Task 12's constraint resolution invented rather than looked up from
// exportMesh's faceKey -> boundary-id map.
func validateResolvedIDs(bcs map[string]int, resolved resolvedConstraints) error {
	known := knownBoundaryIDs(bcs)
	for _, id := range concatIDs(resolved) {
		if !known[id] {
			return fmt.Errorf("elmer: resolved boundary id %d is not among the %d ids exportMesh assigned "+
				"(bcs), want one of the mesh's own boundary ids", id, len(bcs))
		}
	}
	return nil
}

// knownBoundaryIDs returns the set of boundary ids bcs assigns (its values, not its keys).
func knownBoundaryIDs(bcs map[string]int) map[int]bool {
	known := make(map[int]bool, len(bcs))
	for _, id := range bcs {
		known[id] = true
	}
	return known
}

// concatIDs flattens resolved's three id lists into one slice, for the single
// validateResolvedIDs pass.
func concatIDs(resolved resolvedConstraints) []int {
	out := make([]int, 0, len(resolved.Fixed)+len(resolved.Force)+len(resolved.Pressure))
	out = append(out, resolved.Fixed...)
	out = append(out, resolved.Force...)
	out = append(out, resolved.Pressure...)
	return out
}

// writeSimulation sets the deck's Simulation section: coordinate system and simulation
// type mapped from femmodel's neutral enums (erroring, naming the offending value, if the
// study carries one this deck writer doesn't recognize), plus the fixed M1 reference
// keys — Coordinate Mapping is always identity (1 2 3): a later milestone that supports
// non-default axis mapping revisits this.
func writeSimulation(b *sif.Builder, sim femmodel.SolverObject) error {
	coordSys, ok := coordinateSystemWords[sim.CoordinateSystem]
	if !ok {
		return fmt.Errorf("elmer: unsupported CoordinateSystem %q, want one of %v", sim.CoordinateSystem, coordinateSystemWords)
	}
	simType, ok := simulationTypeWords[sim.SimulationType]
	if !ok {
		return fmt.Errorf("elmer: unsupported SimulationType %q, want one of %v", sim.SimulationType, simulationTypeWords)
	}
	b.Simulation("Coordinate Mapping", []int{1, 2, 3})
	b.Simulation("Coordinate System", coordSys)
	b.Simulation("Max Output Level", 10)
	b.Simulation("Simulation Type", simType)
	b.Simulation("Steady State Max Iterations", sim.SteadyStateMaxIter)
	return nil
}

// writeConstants sets the deck's fixed physical-constants block: SI values ElmerSolver's
// built-in physics solvers (gravity loads, radiation, electromagnetics) read regardless of
// which equations a given study actually solves — the M1 elasticity slice doesn't use most
// of these, but Constants is a singleton the vendored smoke dialect always includes.
func writeConstants(b *sif.Builder) {
	b.Constant("Gravity", []float64{0, -1, 0, 9.81})
	b.Constant("Stefan Boltzmann", 5.670374419e-08)
	b.Constant("Permittivity of Vacuum", 8.8542e-12)
	b.Constant("Permeability of Vacuum", 1.25663706e-6)
	b.Constant("Boltzmann Constant", 1.380649e-23)
}

// outputSolverSection builds the shared ResultOutput solver section: writes the VTU field
// dump into the deck's Mesh DB directory (solve.go's checkSolverOutput globs case*.vtu
// there). ASCII output only (Binary Output = False) — vtu.ReadFile (this task) does not
// parse Elmer's binary/appended DataArray encoding.
func outputSolverSection() *sif.Section {
	s := mustSolverSection()
	s.Set("Procedure", sif.FileAttr("ResultOutputSolve/ResultOutputSolver"))
	s.Set("Output File Name", sif.FileAttr("case"))
	s.Set("Vtu Format", true)
	s.Set("Binary Output", false)
	s.Set("Save Geometry Ids", true)
	s.Set("Exec Solver", "After simulation")
	return s
}

// mustSolverSection creates an empty Solver section, panicking only if sif.Solver were ever
// not one of the package's own valid section-kind constants — a programmer error this
// file's own tests would catch immediately, never a possible runtime condition.
func mustSolverSection() *sif.Section {
	s, err := sif.NewSection(sif.Solver)
	if err != nil {
		panic(fmt.Sprintf("elmer: sif.NewSection(Solver) rejected: %v", err))
	}
	return s
}

// writeDeckFiles writes the deck's rendered SIF text to dir/case.sif — the fixed filename
// (deckName, solve.go) runElmerSolver's ELMERSOLVER_STARTINFO always points ElmerSolver at.
func writeDeckFiles(dir string, b *sif.Builder) error {
	path := filepath.Join(dir, deckName)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("elmer: create %s: %w", path, err)
	}
	defer f.Close()
	if err := sif.Write(f, b); err != nil {
		return fmt.Errorf("elmer: write %s: %w", path, err)
	}
	return nil
}
