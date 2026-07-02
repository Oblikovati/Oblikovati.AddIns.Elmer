# ADR-0001 — Native mesh writing, SIF deck conventions, and ribbon placement (M0–M1)

**Status:** accepted (2026-07) · **Builds on:** the port design spec
(`docs/superpowers/specs/2026-07-02-elmer-addin-port-design.md`), itself building on the
CalculiX add-in's precedent for a subprocess FEA provider on a solid tetrahedral mesh.

## Context

This add-in integrates the reference FEM integration's Elmer multiphysics solver
(`ElmerSolver`) the same way `Oblikovati.AddIns.CalculiX` integrates its stress-analysis
solver: a vendored, headless binary driven as a subprocess over a mesh + text deck, with
results read back and flood-plotted in the viewport. Several decisions taken while
building the M0 scaffold, the MV solver vendoring, and the M1 elasticity slice are cheap
to reverse now and expensive to reverse once M2+ (heat, flow, magnetodynamics) build on
top of them, so they are recorded here before the M1 PR merges.

## Decisions

### D1 — Native mesh writer, add-in-owned ids, no grid-converter runtime dependency

The reference FEM integration hands its mesh to Elmer by writing a UNV file and
converting it with `ElmerGrid 8 2`. This add-in instead writes Elmer's native mesh
database directly — four small text files (`mesh.header`, `mesh.nodes`, `mesh.elements`,
`mesh.boundary`) produced by `elmer/meshfmt` from the welded, gmsh-tetrahedralized host
geometry (`elmer/meshexport.go`).

Body and boundary ids are **assigned and owned by the add-in**, not derived by a
converter: one body id per solid, one boundary id per bound face group recovered by
`elmer/facegroups.go`'s FaceKey→gmsh-facet geometric binding (centroid + normal, the same
technique the CalculiX add-in uses). The SIF deck's `Body`/`Boundary Condition` sections
reference these ids directly — there is no `Target Bodies`/`Target Boundaries`
renumbering step to reverse-engineer, because the numbering never left our control.

`ElmerGrid` is still built by `vendor-src/elmer/build.sh` (it ships in the same upstream
source tree at effectively no extra vendoring cost) but is kept strictly as a **debug/
oracle tool** — used during development to cross-check the mesh writer against a known-
good converter output on the smoke cube, never invoked by the running pipeline. MPI
partitioning is out of scope: the solver runs single-process with OMP threads only, so
there is no partition-mesh step to own either.

**Why not the UNV+ElmerGrid path:** we would still have to write a new UNV exporter (our
mesh representation is not the reference integration's mesh object), vendor an extra
conversion step, and then recover ElmerGrid's own boundary/body renumbering to keep the
FaceKey→boundary-condition mapping exact. Writing the native format directly is less
code, not more, and keeps the id space under our control end to end.

### D2 — Mesh in meters, SI deck, no Coordinate Scaling

The kernel's length unit is the centimetre. `elmer/units.go`'s `modelUnitM = 0.01`
converts host coordinates to metres once, at the mesh-export seam
(`elmer/meshexport.go`), so `elmer/meshfmt` always writes nodes already in metres and the
SIF deck is written in plain SI throughout (Pa, kg/m³, N, …). The reference integration's
own writer instead keeps its mesh in millimetres and adds `Coordinate Scaling 0.001` to
the deck's `Simulation` section to convert at solve time.

This add-in skips `Coordinate Scaling` entirely — one fewer solver-side unit transform to
get wrong, and a mesh file that is honest about the units it actually contains (useful
when cross-checking with `ElmerGrid` as a debug oracle, D1). Material properties convert
from host units (GPa, g/cm³) to SI at the deck-writing seam
(`elmer/equations/elasticity.go`'s `gPaToPa`/`gCm3ToKgM3`). Result fields come back in SI
and are converted m→cm only at the render seam (`elmer/render.go`), matching where the
CalculiX add-in does its own mm→cm conversion.

### D3 — Positive pressure = compression (`Normal Force = -p`)

The add-in's pressure boundary condition follows the user-facing convention shared with
the CalculiX add-in: a positive pressure value **pushes into** the face (compression).
Elmer's `Normal Force` SIF keyword is signed positive **outward** along the face normal,
so the writer negates the user value (`elmer/equations/elasticity.go`'s
`writePressureBCs`: `Normal Force = -pressurePa`).

This sign is not just documented — it is **pinned by an oracle**. The pressure-bar
analytic test (`elmer/oracle_solvers_test.go`,
`TestOracleCantileverPressureBarShortens`) asserts the loaded face's mean axial
displacement is **negative** (the bar shortens) under a positive compressive pressure,
through the real vendored solver, not a mocked one. A future sign regression here fails a
physical-direction assertion, not just a magnitude comparison.

### D4 — ASCII VTU only; binary deferred

`ResultOutputSolver`'s deck section (`elmer/deck.go`'s `outputSolverSection`) sets
`Vtu Format = True` and `Binary Output = False`. `elmer/vtu`'s reader hand-rolls the
ASCII `DataArray` subset of the VTK XML UnstructuredGrid schema via `encoding/xml` and
explicitly rejects a non-`ascii` `format` attribute, naming the field and the fix
(`vtu.go`'s `parseAsciiArray`). Binary/appended `DataArray` support is deferred to a
later milestone — nothing in the M1 pipeline needs it, and ASCII keeps the reader small
and dependency-free (no VTK library).

### D5 — `Fixed Mesh = Logical True` on the output solver (bug-derived, load-bearing)

**This was found live, not by inspection.** A cantilever oracle run with a 1000 N tip
load surfaced free-end corner nodes that appeared "unmatched" between the mesh file and
the VTU output — `elmer/render.go`'s `pointIndexForNodes` geometric verification (D6)
correctly flagged them, because the nodes genuinely didn't sit where the mesh said they
should. The root cause: for a non-eigen/non-harmonic (static) analysis, ElmerSolver's
`VtuOutputSolver` defaults to writing each point's **deformed** position (original +
displacement) unless told otherwise — upstream `VtuOutputSolver.F90`'s `RemoveDisp` flag,
exposed in the SIF as `Fixed Mesh`, gates the `x = x - Displacement` step that restores
original coordinates. The free end had moved ~5 mm under load, far past any coordinate
tolerance, so every node there looked desynced even though point order was never actually
scrambled.

`elmer/deck.go`'s `outputSolverSection` therefore always sets `Fixed Mesh = True`. This
is load-bearing, not cosmetic: without it, the point-order guard in D6 cannot distinguish
"solver wrote deformed coordinates as designed" from "solver wrote points in a different
order than the mesh file" — both look identical to a coordinate-matching check. No
epsilon can paper over a millimetre-scale deformation without risking a real point-order
desync going undetected, so this flag must stay set for every static-analysis deck this
add-in writes.

### D6 — VTU point-order geometric verification with a derived tolerance + remap fallback

`elmer/render.go`'s `pointIndexForNodes` does not trust that ElmerSolver's VTU points sit
in the same order as `mesh.nodes` (a positional assumption that generally holds, but has
a real code path that can break it: `Optimize Bandwidth = True`, set for solver
performance, exercises `VtuOutputSolver`'s `InvNodePerm` renumbering, which can reorder
points relative to the mesh file if it engages). Trusting position blindly would produce
a plausible-looking flood plot — same value range, same shape — with values silently
bound to the wrong nodes.

Instead, the fast path **confirms** the positional assumption geometrically (an O(n)
coordinate comparison) and only pays for a hash-based coordinate remap
(`remapPointsByCoordinate`) if that confirmation fails. The match tolerance is not an
arbitrary epsilon: it is derived from what ElmerSolver's own ASCII VTU writer can lose.
That writer prints coordinates with Fortran's `ES16.7E3` edit descriptor — 1 digit + 7
decimals in normalized scientific notation, i.e. 8 significant digits — so round-tripping
a coordinate through that text format can move it by up to half a unit in the last digit,
~5e-8 of the coordinate's own magnitude. `pointVerifyEpsRel = 5e-7` gives a 10x safety
margin above that honest text-precision drift while sitting 4-5 orders of magnitude below
any real node spacing in a solid mesh — so a genuinely desynced point still fails the
check. `pointVerifyEpsAbs = 1e-12` floors the tolerance for coordinates near the origin,
where the relative term alone would collapse toward zero.

### D7 — Booleans always carry an explicit `Logical` type word

`elmer/sif`'s writer (`write.go`'s `boolWord`) always renders boolean attrs as
`Key = Logical True`/`Key = Logical False`, deviating from the vendored, solver-validated
`case.sif`'s own dialect, which writes some booleans bare (`= True`). This is not a
stylistic choice: a live solver run rejected `Force 3 Normalize by Area = True` (a
keyword outside ElmerSolver's built-in keyword table, added by this add-in's own writer,
not present in any reference `.sif`) and only accepted it as `= Logical True`. Real/
Integer/Logical values in this add-in's decks therefore always carry their type word, and
Strings always emit `String "value"` for the same reason. The single exception is
`FileAttr` (`Procedure`, `Output File Name`-style two-token file references), which the
vendored `case.sif` itself writes untyped and which parses fine that way — matched
exactly rather than "fixed" into a form nothing upstream actually uses.

### D8 — Own "Elmer" ribbon tab on both Part and Assembly documents

Per direct user instruction (2026-07-02), Elmer's commands get their own dedicated
"Elmer" ribbon tab rather than sharing a tab with the CalculiX add-in (the port design
spec's open question 3 assumed a shared "FEA" tab; that assumption is superseded). This
mirrors `Oblikovati.AddIns.CAM`'s dedicated "CAM" tab precedent rather than the FEA-family
grouping originally floated.

The public wire contract's `commands.create` (`wire.CreateCommandArgs`) carries exactly
one `Ribbon` per call — there is no multi-ribbon registration primitive. Placing the same
logical action (`Run Study`, `Study Panel`) on both the Part and Assembly ribbons
therefore needs **two command registrations per action**, each with its own command id
(`Elmer.RunStudy` / `Elmer.RunStudy.Assembly`, `Elmer.ShowPanel` /
`Elmer.ShowPanel.Assembly`) dispatching to the same handler — `elmer/commands.go`'s
`elmerCommands` table and `commandArgs` helper. This is a deliberate small duplication
in the registration list, not a missing API feature to route around; a wire-level
multi-ribbon primitive would remove it but is not proposed here.

### D9 — MPI out of scope

The solver is vendored and run single-process with OpenMP threads only
(`vendor-src/elmer/build.sh` builds with no MPI, no MUMPS/Hypre). Partitioned-mesh
support (which D1's add-in-owned ids would also need to survive) is not implemented.

## Consequences

- The mesh and deck formats this add-in emits are **not** byte-compatible with what the
  reference FEM integration's own writer produces (different mesh units, no
  `Coordinate Scaling`, add-in-owned ids instead of `ElmerGrid`-assigned ones, explicit
  `Logical` words). A `.sif`/mesh pair from this add-in should not be assumed portable to
  a different Elmer-based tool without translation.
- `ElmerGrid` is a build artifact and a development-time oracle, not a runtime
  dependency — `OBK_ELMER_BIN` is the only solver binary path the running pipeline
  resolves (`elmer/binresolve.go`).
- The `Fixed Mesh = True` requirement (D5) is specific to non-eigen/non-harmonic static
  analyses. **Revisit trigger:** when a later milestone adds transient or eigen-analysis
  output (M5), re-derive whether `Fixed Mesh` still applies, is irrelevant (eigen modes
  don't carry a "deformed position" the same way), or needs a different flag —
  `VtuOutputSolver.F90`'s `RemoveDisp` semantics may differ per simulation type.
- D6's derived tolerance assumes ASCII `ES16.7E3` output. **Revisit trigger:** D4's
  binary-VTU follow-on will need its own precision analysis (binary doubles carry full
  IEEE-754 precision, so the current relative tolerance would likely tighten
  substantially, not just change format).
- D7's explicit-`Logical` policy was derived from one specific unlisted keyword
  (`Force N Normalize by Area`). **Revisit trigger:** formula/`Variable`-valued boundary
  conditions (spec §13, out of scope for M1) may hit similar unlisted-keyword parsing
  quirks in ElmerSolver's SIF parser; re-verify type-word requirements empirically
  against the real solver rather than assuming D7's finding generalizes.
- D8's two-registrations-per-action pattern will repeat for every Elmer command M2+
  M6 adds. If a third add-in independently needs the same per-document-type ribbon
  duplication, that is the trigger to propose a wire-level multi-ribbon primitive
  instead of continuing to hand-duplicate registrations per add-in.
