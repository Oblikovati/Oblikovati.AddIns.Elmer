// SPDX-License-Identifier: GPL-2.0-only

package sif

import "fmt"

// bodyGroup collects the sections that hang off one numbered body: the Body section itself and
// its (lazily created, at most one each) Material / Equation / Body Force / Initial Condition
// children, plus every Solver section registered against this body via AddSolver.
type bodyGroup struct {
	body      *Section
	material  *Section
	equation  *Section
	bodyForce *Section
	initial   *Section
	solvers   []*Section
}

// Builder accumulates a SIF deck section by section: a Simulation and (optional) Constants
// singleton, any number of numbered bodies (each with its Material/Equation/BodyForce/Initial
// children created lazily on first use), numbered boundary conditions, and arbitrary custom
// sections. Write walks a Builder to produce deck text; Builder itself never touches io.
type Builder struct {
	simulation *Section
	constants  *Section
	bodyByID   map[int]*bodyGroup
	boundaries map[int]*Section
	custom     []*Section
}

// NewBuilder returns an empty deck builder ready for Simulation/Material/... calls.
//
// Example:
//
//	b := sif.NewBuilder()
//	b.Simulation("Simulation Type", "Steady State")
func NewBuilder() *Builder {
	return &Builder{
		bodyByID:   make(map[int]*bodyGroup),
		boundaries: make(map[int]*Section),
	}
}

// mustSection creates a section of kind, panicking only on a programmer error — kind must
// always be one of this package's own exported constants, so NewSection can never actually
// reject it; the panic exists to catch a future typo in this file, not caller input.
func mustSection(kind string) *Section {
	s, err := NewSection(kind)
	if err != nil {
		panic(fmt.Sprintf("sif: internal section kind %q rejected: %v", kind, err))
	}
	return s
}

// Simulation sets key on the deck's single Simulation section, creating it on first use.
//
// Example:
//
//	b.Simulation("Coordinate System", "Cartesian 3D")
func (b *Builder) Simulation(key string, v any) {
	if b.simulation == nil {
		b.simulation = mustSection(Simulation)
	}
	b.simulation.Set(key, v)
}

// Constant sets key on the deck's single Constants section, creating it on first use.
//
// Example:
//
//	b.Constant("Gravity", -9.81)
func (b *Builder) Constant(key string, v any) {
	if b.constants == nil {
		b.constants = mustSection(Constants)
	}
	b.constants.Set(key, v)
}

// bodyGroupFor returns body id's group, creating it (and its Body section, seeded with
// Target Bodies) on first use.
func (b *Builder) bodyGroupFor(id int) *bodyGroup {
	if g, ok := b.bodyByID[id]; ok {
		return g
	}
	body := mustSection(Body)
	body.Set("Target Bodies", []int{id})
	g := &bodyGroup{body: body}
	b.bodyByID[id] = g
	return g
}

// Material sets key on body's Material section, creating the section (and linking it from the
// Body section) on first use.
//
// Example:
//
//	b.Material(1, "Youngs Modulus", 2.1e11)
func (b *Builder) Material(body int, key string, v any) {
	g := b.bodyGroupFor(body)
	if g.material == nil {
		g.material = mustSection(Material)
		g.body.Set("Material", g.material)
	}
	g.material.Set(key, v)
}

// ensureEquation lazily creates body's Equation section and links it from the Body section,
// shared by Equation and AddSolver so neither duplicates the other's setup.
func (g *bodyGroup) ensureEquation() {
	if g.equation == nil {
		g.equation = mustSection(Equation)
		g.body.Set("Equation", g.equation)
	}
}

// Equation sets key on body's Equation section, creating the section (and linking it from the
// Body section) on first use. Prefer AddSolver to grow "Active Solvers" — it manages the
// section-reference list Set alone cannot express.
//
// Example:
//
//	b.Equation(1, "Priority", 1)
func (b *Builder) Equation(body int, key string, v any) {
	g := b.bodyGroupFor(body)
	g.ensureEquation()
	g.equation.Set(key, v)
}

// BodyForce sets key on body's Body Force section, creating the section (and linking it from
// the Body section) on first use.
//
// Example:
//
//	b.BodyForce(1, "Stress Bx", 1.0e6)
func (b *Builder) BodyForce(body int, key string, v any) {
	g := b.bodyGroupFor(body)
	if g.bodyForce == nil {
		g.bodyForce = mustSection(BodyForce)
		g.body.Set("Body Force", g.bodyForce)
	}
	g.bodyForce.Set(key, v)
}

// Initial sets key on body's Initial Condition section, creating the section (and linking it
// from the Body section) on first use.
//
// Example:
//
//	b.Initial(1, "Displacement 1", 0.0)
func (b *Builder) Initial(body int, key string, v any) {
	g := b.bodyGroupFor(body)
	if g.initial == nil {
		g.initial = mustSection(InitialCondition)
		g.body.Set("Initial Condition", g.initial)
	}
	g.initial.Set(key, v)
}

// AddSolver registers s as one of body's Active Solvers: appended to that body's Equation
// section (created on first use) as a section reference, so Write renders
// "Active Solvers(N) = Integer id1 id2 ..." once ids are assigned.
//
// Example:
//
//	solver, _ := sif.NewSection(sif.Solver)
//	solver.Set("Procedure", sif.FileAttr("StressSolve/StressSolver"))
//	b.AddSolver(1, solver)
func (b *Builder) AddSolver(body int, s *Section) {
	g := b.bodyGroupFor(body)
	g.ensureEquation()
	g.equation.addSectionRef("Active Solvers", s)
	g.solvers = append(g.solvers, s)
}

// Boundary sets key on the numbered Boundary Condition section, creating it (seeded with
// Target Boundaries) on first use.
//
// Example:
//
//	b.Boundary(1, "Displacement 1", 0.0)
func (b *Builder) Boundary(boundary int, key string, v any) {
	s, ok := b.boundaries[boundary]
	if !ok {
		s = mustSection(BoundaryCondition)
		s.Set("Target Boundaries", []int{boundary})
		b.boundaries[boundary] = s
	}
	s.Set(key, v)
}

// AddSection appends s to the deck as a custom section, written before Simulation. Used for
// deck content Builder's convenience methods don't model directly — e.g. a hand-built Body
// section shared by two body groups (see the id-assignment dedup test).
//
// Example:
//
//	b.AddSection(customSection)
func (b *Builder) AddSection(s *Section) {
	b.custom = append(b.custom, s)
}
