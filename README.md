# Oblikovati Elmer

A host add-in that integrates **Elmer** (`ElmerSolver`) — an open-source multiphysics
finite-element solver — as a **multiphysics FEA provider** for Oblikovati. It links
**only** the Apache-2.0 public API (`oblikovati.org/api`) and reaches the running host
over the C ABI (ADR-0016) — never the GPL application internals.

> Built/versioned/shipped the same way as
> [`Oblikovati.AddIns.CalculiX`](../Oblikovati.AddIns.CalculiX): a cgo `c-shared`
> library, its own Go module pinned to a published `oblikovati.org/api` release, sibling
> repos wired by `.github/actions/siblings`, and an API-tracking release pipeline.

Elmer's differentiated value over the existing CalculiX add-in — flow (incompressible
Navier–Stokes CFD), true magnetodynamics, and native multi-equation coupling on shared
bodies — is planned for later milestones (see the spec, §11). Everything structural that
CalculiX already covers gets a second, independent solver, usable as a cross-solver
oracle in both directions; the M1 slice below proves that pipeline on linear elasticity.

## Pipeline (M1 slice: elasticity)

1. **Resolve study** — material + the selected faces that carry loads/boundary
   conditions (`Materials.List`/`Get`, `Model.Selection`).
2. **Surface mesh** — pull the body's triangulated surface (`Body.CalculateFacets`) and
   weld it to a watertight, manifold soup (`elmer/hostmesh.go`, cloned/adapted from the
   CalculiX add-in).
3. **Volume mesh** — drive a vendored **gmsh** (subprocess) to turn the surface into a
   solid tetrahedral mesh (`elmer/gmshrun.go`, `elmer/tetmesh.go`); recover
   `FaceKey` → mesh-facet groups geometrically (centroid + normal) so loads/BCs bind to
   the right element faces (`elmer/facegroups.go`).
4. **Write mesh + deck** — emit Elmer's native mesh database
   (`mesh.header`/`mesh.nodes`/`mesh.elements`/`mesh.boundary`, `elmer/meshfmt`) with
   add-in-owned body/boundary ids, and a `case.sif` solver-input deck
   (`elmer/sif` + `elmer/equations/elasticity.go`) — see
   [ADR-0001](architecture/ADR-0001-native-mesh-writing-and-deck-conventions.md) for why
   this add-in writes its own mesh format instead of converting through `ElmerGrid`.
5. **Solve** — the vendored headless `ElmerSolver` solves as a subprocess
   (`elmer/solve.go`), scratch dir per run, stdout scraped for errors.
6. **Render** — parse the ASCII VTU result (`elmer/vtu`), verify point order against the
   mesh geometrically, and push displacement/von Mises back as a `clientGraphics` flood
   plot + status-line field range (`elmer/render.go`, `elmer/panel.go`).

## Build

```sh
make build      # cgo c-shared library into build/
make install    # build + copy library + manifest into the host's add-ins dir
make test       # cgo-free elmer engine unit tests (add-in<->host integration tests are a future addition)
```

`make install` copies into `../Oblikovati/head/addins` by default (the app repo is
expected as a sibling checkout, matching `go.work`'s `use ../Oblikovati`); override with
`ADDINS_DIR=/path/to/head/addins make install` if the host lives elsewhere, or
`OBK_ADDINS_DIR` at host-launch time to point a running host at a different add-ins
directory without reinstalling.

## Vendored solvers

Both the volume mesher and the FEM solver build headless via CMake from vendored source
— no network, no system libraries, no build-time dependency on the host toolchain beyond
a C/C++/Fortran compiler + cmake:

```sh
make build-solvers   # runs vendor-src/gmsh/build.sh + vendor-src/elmer/build.sh
```

| Component | Purpose | Built to |
|---|---|---|
| `vendor-src/gmsh/build.sh` | gmsh 4.13.1 CLI — surface-to-tet volume mesher | `vendor-src/gmsh/build/gmsh` |
| `vendor-src/elmer/build.sh` | elmerfem `release-26.2` — `ElmerSolver` (+ `ElmerGrid`, kept as a debug oracle only, see ADR-0001) | `vendor-src/elmer/install/bin/ElmerSolver` |

Provenance, exact upstream versions, source-archive SHA-256s, and license notes for both
are recorded in `vendor-src/gmsh/NOTICE.md` and `vendor-src/elmer/NOTICE.md`.

### Environment variables

The engine resolves each solver binary in the same three-tier order
(`elmer/binresolve.go`): a direct env override → the vendored build path → a same-named
binary on `$PATH`.

| Variable | Meaning | Default (if unset) |
|---|---|---|
| `OBK_ELMER_BIN` | Path to `ElmerSolver`, or a directory containing it | `vendor-src/elmer/install/bin/ElmerSolver`, else `ElmerSolver` on `$PATH` |
| `OBK_ELMER_HOME` | `ELMER_HOME` passed through to the `ElmerSolver` subprocess (its own runtime needs this to find its solver library set) | unset — relies on the vendored install layout being self-contained |
| `OBK_GMSH_BIN` | Path to the `gmsh` CLI, or a directory containing it | `vendor-src/gmsh/build/gmsh`, else `gmsh` on `$PATH` |

## Tests

```sh
go test ./...                 # pure-Go engine tests; skip real-solver tests if binaries absent
go test ./... -tags solvers   # also run the real-solver analytic oracles (needs OBK_ELMER_BIN/OBK_GMSH_BIN, or the vendored build)
```

The `solvers`-tagged tests (`elmer/oracle_solvers_test.go`) drive the actual vendored
`ElmerSolver` through the full pipeline and assert against closed-form analytic results —
not mocks. As of the M1 elasticity slice:

- **Cantilever bending** vs. Euler–Bernoulli: 4.60% relative error (budget 5%; the gap is
  the expected shear-deformation correction on a non-slender beam, cross-checked against
  the CalculiX add-in's own cantilever oracle, which sees the same effect).
- **Pressure-loaded bar** (axial compression) vs. closed-form `σ = F/A`, `δ = σL/E`:
  0.68% relative error (budget 2%), plus a physical-direction assertion that the loaded
  face shortens under positive (compressive) pressure — see ADR-0001 D3.

Unit tests without the `solvers` tag use golden fixtures (byte-stable SIF decks, mesh
files, a solver-validated VTU sample) and a `fakeHost` (no real host process, no real
solver), so `go test ./...` is fast and runs everywhere, while `-tags solvers` is the
CI-gated, environment-dependent proof that the pipeline works against the real binaries.

## Layout

```
export.go / hostcaller.go / manifest.go   C-ABI c-shared shell (the only cgo)
manifest.json                              add-in manifest (capabilities)
elmer/                                     cgo-free FEM engine + pipeline
  engine.go        Notify/launch/status orchestration + HostCaller
  commands.go       Elmer.RunStudy / Elmer.ShowPanel commands + ribbon registration
  panel.go          dockable study-parameters window
  study.go          surface -> volume -> mesh+deck -> solve -> render orchestration
  hostmesh.go       host surface pull + weld
  gmshrun.go / tetmesh.go / mshparse.go   vendored-gmsh volume meshing
  facegroups.go / elementfaces.go / multibody.go   FaceKey <-> mesh-facet binding
  meshexport.go     host mesh -> meshfmt.Mesh (add-in-owned body/boundary ids)
  solve.go          vendored ElmerSolver subprocess runner
  render.go         VTU point-order verification + flood-plot rendering
  constraintaids.go / glyphmesh.go   viewport constraint markers
  femmodel/         pure domain aggregate (Analysis, EquationObject, MaterialObject, ...)
  sif/              SIF section builder + deterministic writer
  equations/        one writer per equation kind (elasticity.go; more per milestone)
  meshfmt/          native Elmer mesh.{header,nodes,elements,boundary} writer
  vtu/              ASCII VTK UnstructuredGrid (.vtu) result reader
architecture/                              ADRs (Oblikovati terms only — no third-party CAD names)
docs/superpowers/                          specs + plans
vendor-src/                                vendored elmerfem + gmsh + build.sh + NOTICE.md
gplpurity/                                  guard: this repo never imports the GPL app module
```

## License

GPL-2.0-only (see `LICENSE`). This add-in vendors third-party source under their own
licenses — see `vendor-src/elmer/NOTICE.md` (elmerfem: GPL-2.0-or-later with LGPL-2.1
library parts; reference LAPACK: modified BSD-3-Clause) and `vendor-src/gmsh/NOTICE.md`
(gmsh: GPL-2.0-or-later, bundling several independently-licensed meshing engines under
`contrib/`, documented per-component in that NOTICE). It links only the Apache-2.0
`oblikovati.org/api` module; the GPL application module is a test-scope-only sibling
dependency (`go.work`), never linked into the shipped `.so`/`.dll`/`.dylib`.
