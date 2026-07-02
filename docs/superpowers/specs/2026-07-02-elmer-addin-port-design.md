# Oblikovati.AddIns.Elmer — Elmer multiphysics FEA add-in (port design)

Date: 2026-07-02
Status: ACCEPTED — M0/MV/M1 built and validated on PR #1 against this design; §15's open
questions are answered below. Decisions D1-D6 stood as taken; D3 (mesh handoff) and D6
(result decode) are additionally pinned by ADR-0001 with the load-bearing details M1
surfaced.

## 1. Goal

Integrate the Elmer multiphysics FEM solver (`ElmerSolver`) as an Oblikovati add-in,
faithfully porting the reference FEM-workbench Elmer integration (upstream
`femsolver/elmer`: SIF writer + 10 equation writers, ~5.5k lines of Python), the same
way `Oblikovati.AddIns.CalculiX` ports the CalculiX integration.

Elmer's differentiated value over the existing CalculiX add-in — recorded as explicit
exclusions in CalculiX ADR-0003/ADR-0007 — is:

- **Flow (incompressible Navier–Stokes CFD)** on fluid bodies,
- **true magnetodynamics** (3D AV-solver and 2D), which need multi-body air/fluid
  domains that the reference implementation solves with Elmer, never CalculiX,
- **native multiphysics coupling**: several equations solved in one simulation over
  shared bodies (e.g. heat + elasticity, flow + heat).

Everything structural/thermal that CalculiX already covers gets a second, independent
solver — usable as a cross-solver oracle in both directions.

## 2. Decisions taken (flagged assumptions, user to confirm)

| # | Decision | Chosen default |
|---|----------|----------------|
| D1 | Milestone order | Validate pipeline with easy-oracle physics first (elasticity, heat), then go straight for the gap physics (flow, magnetodynamics), then the remaining equations |
| D2 | Solver acquisition | Vendor `elmerfem` source + build script, like ccx (standing rule: add-ins carry no external deps); env-var overrides for dev |
| D3 | Mesh handoff | Write Elmer's native mesh files directly from our gmsh tet mesh (skip UNV+ElmerGrid conversion) |
| D4 | Code sharing with CalculiX add-in | Structural clone (copy + adapt), no shared Go module |
| D5 | Model architecture | Start on the femmodel-aggregate end state proven by CalculiX Phase 2 — no flat-settings strangler phase |
| D6 | Result decode | Pure-Go VTU reader; ASCII output first, binary later |

## 3. Approaches considered

**A. Self-contained clone of the CalculiX add-in architecture (CHOSEN).**
New `oblikovati.org/elmer` module cloned structurally from `Oblikovati.AddIns.CalculiX`
(itself cloned from the FEMM bridge — established precedent). Reuse-by-copy of the
proven host plumbing: surface pull + weld, vendored gmsh tet meshing, FaceKey↔face
binding, constraint aids, client-graphics flood plot, browser tree/ribbon/panel
patterns. Elmer-specific parts written fresh: SIF builder, equation writers, Elmer
mesh writer, VTU reader.
*Pros:* fastest, battle-tested plumbing, each add-in stays independently shippable.
*Cons:* duplicated plumbing across repos (accepted; third copy would justify a shared
module later).

**B. Faithful-plumbing port (UNV writer + vendored ElmerGrid).**
Replicate the upstream toolchain exactly: write a UNV mesh, convert with ElmerGrid
`8 2`, keep its boundary numbering.
*Pros:* maximally faithful.
*Cons:* we'd still write a new UNV exporter (our mesh is not their FemMesh), vendor an
extra binary, and surrender control of boundary-id assignment that our FaceKey→BC
mapping depends on. The faithfulness that matters is the SIF/equation semantics, not
the mesh plumbing. Rejected.

**C. Extract a shared `femcore` module both add-ins depend on.**
*Pros:* DRY.
*Cons:* cross-repo versioning burden, destabilizes the shipped CalculiX add-in, and
the aggregate shapes genuinely differ (Elmer is equation-centric, CalculiX is
analysis-type-centric). Rejected for now; revisit if a third solid-FEA add-in appears.

## 4. Architecture

Repo `Oblikovati.AddIns.Elmer` (GPL-2.0-only), module `oblikovati.org/elmer`, a
c-shared library loaded by the host over the C ABI (ADR-0016), linking only the
Apache-2.0 `oblikovati.org/api` (go.work replace locally, siblings action in CI,
gplpurity guard — all cloned from the CalculiX repo).

```
Oblikovati.AddIns.Elmer/
  export.go hostcaller.go manifest.go manifest.json   # c-shared shell (clone)
  include/ Makefile scripts/ gplpurity/ .github/       # scaffold (clone)
  elmer/                    # cgo-free engine package
    engine.go commands.go panel.go tree …              # host-facing (adapt from ccx)
    hostmesh.go facegroups.go glyphs …                 # mesh pull + binding (adapt)
    femmodel/               # pure domain aggregate (Elmer-shaped, see §5)
    sif/                    # SIF section builder + writer (port of sifio.py)
    equations/              # one writer per equation (port of equations/*_writer.py)
    meshfmt/                # Elmer mesh.{header,nodes,elements,boundary} writer
    vtu/                    # VTU (XML unstructured grid) result reader
  vendor-src/elmer/         # vendored elmerfem source + build.sh + NOTICE.md
  vendor-src/gmsh/          # vendored gmsh (copy of ccx harness)
  architecture/             # ADRs (Oblikovati terms only — no third-party CAD names)
  docs/superpowers/         # specs + plans
```

**Pipeline per study:** host surface facets + materials + selections → weld (cm→m) →
gmsh volume tet mesh (order 1 or 2) with per-face tags → FaceKey↔gmsh-face bind
(normal+centroid, as in ccx) → write Elmer mesh files with one boundary id per bound
face group and one body id per solid → generate `case.sif` + `ELMERSOLVER_STARTINFO`
→ run `ElmerSolver` (subprocess, scratch dir, stdout scraped for errors) → read
`FreeCAD*.vtu`-equivalent output (`Output File Name = oblikovati`) → flood-plot the
selected field as client graphics + status summary.

## 5. Domain model (`elmer/femmodel`)

Elmer-shaped aggregate, following the CalculiX femmodel idiom (pure package, neutral
strings for enums, cast at the projection seam, compile-time-testable invariants) but
mirroring the upstream Elmer object model, which is **equation-centric**:

- `Analysis` aggregate root.
- `SolverObject` — simulation-level settings: SimulationType (`steady`/`transient`/
  `scanning`), CoordinateSystem, SteadyState min/max iterations, BDF order, timestep
  intervals/sizes, output intervals, binary output.
- `EquationObject` (0..n) — Kind (`elasticity`, `deformation`, `heat`, `flow`,
  `electrostatic`, `electricforce`, `staticcurrent`, `flux`, `magnetodynamic`,
  `magnetodynamic2D`) + per-equation linear-system settings (solver type,
  direct/iterative method, preconditioner, tolerances, BiCGstabl degree, …),
  nonlinear-system settings, equation-specific switches (EigenAnalysis + mode count,
  CalculateStresses, …), and body scope (all bodies vs named bodies).
- `MaterialObject` (0..n) — body-scoped; solid props (Young, Poisson, density, α,
  k, cp, σ_elec, relative permeability/permittivity, …) or fluid props (density,
  viscosity, …). Fluid-vs-solid classing gates which equations run on which body,
  exactly as upstream (`KinematicViscosity present ⇒ fluid`).
- `ConstraintObject` (0..n) — neutral Kind + face/body references + params. Initial
  kinds map the upstream constraint set per equation family: fixed, force, pressure,
  displacement, spring; temperature, heat flux; potential, current; velocity,
  initial pressure/velocity; magnetization/current density/potential for magnetics.
- `MeshObject` — element order, size, body scope.
- `ResultObject` — primary field selection per equation.

`projectAnalysis`-style seam converts the aggregate into the solve-time model the
pipeline consumes (as in CalculiX 2.12: aggregate is the sole source of truth from
day one; no `extras`).

## 6. SIF writer (`elmer/sif` + `elmer/equations`)

- `sif`: port of `sifio.py` — `Section` (name, priority, key→typed attr), numbered
  sections (Body, Material, Body Force, Equation, Solver, Boundary Condition,
  Initial Condition, Component), Builder that assembles per-body/per-boundary maps,
  id manager that dedups identical sections, deterministic ordering (sorted keys) so
  decks are byte-stable for golden tests. Typed attrs: Real / Integer / Logical /
  String / File / arrays; `Variable`-valued entries for formula BCs come later.
- `equations`: one file per equation kind, porting the corresponding
  `*_writer.py` faithfully (solver section defaults, constants, material blocks, BC
  blocks, body forces, initial conditions). The Go registry mirrors the ccx
  ConstraintWriter idiom: each equation contributes solver sections per body and
  consumes the constraint objects it understands; unhandled constraints produce a
  visible warning (upstream behavior).
- Simulation section: SI units; `Coordinate Scaling` handles our mesh units (§8).

## 7. Mesh handoff (`elmer/meshfmt`) — D3

Write Elmer's native mesh format directly (four small text files:
`mesh.header`, `mesh.nodes`, `mesh.elements`, `mesh.boundary`):

- body ids = per-solid element groups (multi-body studies keep per-body materials),
- boundary ids = our bound face groups; the SIF references them via
  `Use Mesh Names`-style numbering kept under our control — the FaceKey→BC mapping
  stays exact, no ElmerGrid renumbering to reverse-engineer,
- element types: 504 (linear tet) / 510 (quadratic tet); gmsh tet10 node order maps
  per Elmer convention (verify against ElmerGrid output on a golden cube during M1).

ElmerGrid is still built by the vendor script (it ships in the same source tree) and
kept as a **debug/oracle tool** (e.g. cross-check our mesh writer), not a runtime
dependency. MPI partitioning is out of scope (single-process + OMP threads only).

## 8. Units

Kernel unit is cm. The mesh writer emits nodes in **meters** (×0.01) and the SIF is
written in plain SI throughout (upstream writes SI and scales mm meshes with
`Coordinate Scaling 0.001`; we scale at the writer seam instead and skip coordinate
scaling entirely — one fewer solver-side transform). Material properties convert
from host units (GPa, g/cm³, W/(m·K), …) to SI at the projection seam. Result fields
come back in SI; displacement rendering converts m→cm at the flood-plot seam.

## 9. Solver execution & vendoring — D2

- `vendor-src/elmer/`: vendored `elmerfem` release source + `build.sh` producing
  self-contained `ElmerSolver` + `ElmerGrid` (CMake + gfortran; BLAS/LAPACK from the
  proven reference-LAPACK recipe used for ccx; no MPI, no MUMPS/Hypre, no GUI).
  `NOTICE.md` with provenance + sha256, following the ccx vendor precedent.
- Runtime resolution: `OBK_ELMER_BIN` (and `OBK_ELMERGRID_BIN` for the debug tool)
  override → else the vendored build. Same pattern as `OBK_CCX_BIN`/`OBK_GMSH_BIN`.
- CI: `build` job (add-in .so on all 3 OSes) + `solvers` job Linux-only building
  ElmerSolver from source and running a cube smoke solve (ccx CI precedent).
- Error surface: scrape ElmerSolver stdout/stderr for `ERROR::`/convergence failures
  (`errcheck` idiom from ccx), guard on missing result file, prereq checks before
  launch (mesh, material, per-equation required constraints).

## 10. Results (`elmer/vtu`) — D6

ResultOutputSolver writes VTU (XML UnstructuredGrid). Pure-Go reader supporting
ASCII `DataArray`s first (`Binary Output = False` in the deck), extended to
base64/appended binary later. Extract point fields (displacement, temperature,
potential, velocity, pressure, magnetic flux density, von Mises when
`CalculateStresses`) → same flood-plot/legend/status rendering path as ccx
(rampMapper, scalar field result, deformation warp later with the 0b graphics API).
Transient runs read the last timestep first slice (`.pvd`/time-collection later).

## 11. Milestones (each oracle-gated, PR-per-milestone, auto-merge-when-green)

- **M0 scaffold** — repo skeleton cloned from ccx: c-shared shell, manifest
  (`com.oblikovati.elmer`), Makefile, CI, gplpurity, SPDX, go.work. Loads in host,
  registers a stub command.
- **MV vendor** — vendor elmerfem + gmsh + build.sh + NOTICE; CI solvers job; cube
  smoke solve through real ElmerSolver.
- **M1 elasticity slice (pipeline proof)** — host pull → gmsh → meshfmt → SIF
  (Elasticity equation; fixed/force/pressure BCs) → ElmerSolver → VTU → von Mises +
  displacement flood plot + constraint aids + grouped panel. Oracles: cantilever vs
  Euler–Bernoulli (ccx M1 twin) **and cross-solver vs the CalculiX add-in on the
  identical model** (target: displacement within a few %). Live MCP test.
- **M2 scalar-field family** — Heat (temperature BC, flux, convection), then
  StaticCurrent + Electrostatic (+ Flux/Electricforce post-solvers). Oracles: Fourier
  wall (exact in ccx twin), Laplace mid-plane potential, Ohm bar; cross-solver checks.
- **M3 Flow (CFD — first gap physics)** — fluid material classing, Flow equation,
  velocity/no-slip/outlet BCs, initial conditions; steady laminar first. Oracles:
  plane Poiseuille / pipe Hagen–Poiseuille profile + Couette; mesh-refinement sanity.
- **M4 Magnetodynamics (second gap physics)** — 3D AV magnetodynamic + 2D variant;
  air domain modeled as an explicit CAD body (multi-body meshing already proven in
  ccx); magnet/coil body forces, vector potential BCs. Oracles: infinite-wire /
  finite solenoid axial B (analytic), 2D vs 3D cross-check.
- **M5 remaining parity** — Deformation (nonlinear elasticity), eigen-analysis
  (elasticity EigenAnalysis + frequencies from solver output), transient simulation
  type (time stepping), multi-equation coupled studies (heat+elasticity thermal
  stress vs ccx coupled twin; flow+heat conjugate later).
- **M6 UI parity pass** — browser Analysis tree, Elmer ribbon tab, per-object task
  panels, result-field selector — direct reuse of the CalculiX Phase-2 patterns and
  the modal TaskPanelSpec API (v0.101.0+).

## 12. Testing

- Unit: golden SIF decks per equation writer (byte-stable builder), meshfmt golden
  files cross-checked once against ElmerGrid output, VTU reader fixtures.
- fakeHost engine tests (ccx idiom) for panel/tree/commands.
- Real-solver integration tests behind the vendored-binary presence check; every
  milestone lands ≥1 analytic oracle through the real ElmerSolver.
- Cross-solver oracles vs the CalculiX add-in (same host model driven by both).
- Live MCPBridge test + screenshot verification before each PR (standing rule).
- Coverage >80%, duplication <3%, golangci-lint + funlen(20/30) locally before PRs.

## 13. Out of scope (initial program)

MPI-parallel runs and partitioned meshes; GUI ElmerGUI anything; formula/`Variable`
expression BCs; FSI coupling; 2D/1D meshes (solid tets only, like ccx — the
magnetodynamic2D equation runs on a thin solid or is deferred if Elmer strictly
requires a 2D mesh: to be verified in M4 spike); result persistence in `.obk`
(tracked as the shared M5-persistence item with ccx).

## 14. Repo policies carried over

ADRs in `architecture/` use Oblikovati terms only (no third-party CAD names in
committed docs/filenames); upstream mapping notes live untracked at
`oblikovati-workspace/elmer-port-reference/`. Every exported `.go` file carries
`SPDX-License-Identifier: GPL-2.0-only`. Never re-declare wire DTOs; host access
only via `api/client`. Close issues on PR merge; no `git add -A`.

## 15. Open questions for the user — ANSWERED

1. **Milestone order (D1):** confirmed as taken — M1 elasticity (easy-oracle pipeline
   proof) landed first, gap physics (flow M3, magnetodynamics M4) stay after the scalar-
   field family (M2), no pull-forward requested.
2. **Native mesh writer (D3):** confirmed — Elmer's native mesh database
   (`mesh.header`/`mesh.nodes`/`mesh.elements`/`mesh.boundary`) is written directly by
   `elmer/meshfmt`, with add-in-owned body/boundary ids. `ElmerGrid` is vendored anyway
   (ships in the same upstream source tree) and kept strictly as a debug/oracle tool, not
   a runtime dependency. Full rationale: ADR-0001 D1.
3. **Ribbon placement: SUPERSEDED by user directive (2026-07-02).** Not the "shared FEA
   tab" option this question floated — Elmer gets its own dedicated **"Elmer" ribbon
   tab**, on both the Part and Assembly ribbons, mirroring
   `Oblikovati.AddIns.CAM`'s dedicated "CAM" tab precedent. Implemented via two command
   registrations per action (`Elmer.RunStudy` / `Elmer.RunStudy.Assembly`, etc.) since
   the wire contract has no multi-ribbon registration primitive. Full rationale:
   ADR-0001 D8.
4. **elmerfem release to vendor:** **release-26.2** (tag `release-26.2`, commit
   `43b44cf`) — the newest `release-*` tag as of vendoring (MV), not a numbered "9.x"
   GitHub Release (elmerfem doesn't publish one; confirmed via
   `gh api repos/ElmerCSC/elmerfem/releases/latest` resolving to this tag). Provenance
   and source-archive SHA-256 recorded in `vendor-src/elmer/NOTICE.md`.
