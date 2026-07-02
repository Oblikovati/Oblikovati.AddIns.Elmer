# Vendored elmerfem + reference LAPACK — provenance & license

This directory vendors **elmerfem** (ElmerSolver + ElmerGrid) and **reference LAPACK**
in source form so the add-in builds a self-contained FEA solver stack with no
build-time or runtime external dependencies beyond a C/C++/Fortran toolchain + cmake
(build/install output is not committed — `build.sh` rebuilds it). `build.sh` compiles
everything under this directory into `install/bin/ElmerSolver` and `install/bin/ElmerGrid`;
the add-in runs `ElmerSolver` as a subprocess to solve a mesh + `.sif` deck.

## Components (exact upstream releases)

| Component | Version | Upstream | SHA-256 of source archive | License |
|---|---|---|---|---|
| elmerfem | release-26.2 (tag `release-26.2`, commit `43b44cf`) | https://github.com/ElmerCSC/elmerfem | `dc3a33590e480e89e563a9709260bf6b592af29038d70ddddf5070a1b9d68454` (GitHub `tarball_url` for the tag) | GPL-2.0-or-later, with LGPL-2.1 library parts (see `elmerfem/license_texts/`) |
| Reference LAPACK (incl. BLAS) | 3.8.0 | https://github.com/Reference-LAPACK/lapack (tag v3.8.0) | `deb22cc4a6120bff72621155a9917f485f96ef8319ac074a7afbc68aab88bcf6` | modified BSD-3-Clause (see `lapack/lapack-3.8.0/LICENSE`) |

elmerfem's release page does not publish a numbered "latest" GitHub Release asset list
(`gh api repos/ElmerCSC/elmerfem/releases/latest` succeeds and resolves to `release-26.2`,
the newest of the `release-*` tags). The tarball was fetched via
`gh api repos/ElmerCSC/elmerfem/tarball/release-26.2` and its SHA-256 recorded above.

The LAPACK sources are **copied verbatim from `Oblikovati.AddIns.CalculiX/vendor-src/ccx/lapack-3.8.0`**
(same reference LAPACK 3.8.0, chosen there for the same reason: pure fixed-form Fortran,
no CMake, compiles with a flat `gfortran` loop). Original netlib/Reference-LAPACK
provenance and SHA-256 are documented in that repo's `vendor-src/ccx/NOTICE.md`; nothing
was re-fetched or re-verified against upstream here, only copied and re-recorded.

## Local modifications

None — both `elmerfem/` and `lapack/lapack-3.8.0/` are unmodified upstream sources
(same "no local patches" stance as the ccx LAPACK vendor). The `lapack/build.sh` recipe
here **differs** from the ccx vendor's: ccx links BLAS+LAPACK into a single combined
`liblapack.a`, but Elmer's CMake `FindBLAS`/`FindLAPACK` expect `BLAS_LIBRARIES` and
`LAPACK_LIBRARIES` to be independently satisfiable, so `lapack/build.sh` compiles
`BLAS/SRC/*.f` into `librefblas.a` and `SRC/*.f` (+ the same `INSTALL/*.f` timer/machine
shims as the ccx recipe) into `liblapack.a` as two separate archives. Compiler flags
(`-O2 -fcommon -fallow-argument-mismatch`) are identical to the ccx recipe.

## Trimmed elmerfem tree — what was kept / dropped

Kept (required to configure+build ElmerSolver/ElmerGrid with the flags in `build.sh`):
`fem/` (minus `fem/tests/`), `elmergrid/`, `matc/`, `umfpack/`, `mathlibs/`, `fhutiter/`,
`meshgen2d/`, `cmake/`, `cpack/`, top `CMakeLists.txt`, `license_texts/`, `LICENSE.md`,
`README.adoc`.

Dropped (not needed for the `WITH_MPI=OFF -DWITH_OpenMP=ON -DWITH_ElmerIce=OFF
-DWITH_ELMERGUI=OFF -DWITH_CONTRIB=OFF` build in `build.sh`):

- `ElmerGUI/`, `ElmerGUIlogger/`, `ElmerGUItester/` — GUI, guarded by `WITH_ELMERGUI` /
  `WITH_ELMERGUITESTER` / `WITH_ELMERGUILOGGER` (all default `FALSE`, and we don't turn
  them on).
- `elmerice/` — Elmer/Ice glaciology package, guarded by `WITH_ElmerIce` (default
  `FALSE`; it also hard-requires MPI, which we build without).
- `post/` — ElmerPost, guarded by `WITH_ELMERPOST` (default `FALSE`, deprecated upstream).
- `ElmerWorkflows/` — example workflow scripts, not referenced by any `CMakeLists.txt`.
- `fem/tests/` — the upstream CTest suite (~128 MB). Its `add_subdirectory` in
  `fem/CMakeLists.txt` is guarded by `BUILD_TESTING`; because `elmerfem/CMakeLists.txt`
  does a bare `include(CTest)`, which defaults `BUILD_TESTING` to `ON`, `build.sh`
  **explicitly passes `-DBUILD_TESTING=OFF`** — otherwise cmake configure fails looking
  for the directory we removed. (This is the one "restore or force the flag" adjustment
  the trim required; documented here per the task brief's instruction to record it.)
- `elmergrid/tests/` — ElmerGrid's own test fixtures (~1.2 MB); not referenced by
  `elmergrid/CMakeLists.txt` (`ADD_SUBDIRECTORY(src)` only), safe to drop.
- top-level `contrib/` (`lua-5.1.5/`, `Zoltan_v3.83/`) — guarded by `WITH_LUA` /
  `WITH_Zoltan` (both default `FALSE`).
- `ci/`, `docker/`, `.github/`, `.vscode/`, `misc/`, `pics/`, `ReleaseNotes/`,
  `compilation_instructions/` — CI/dev tooling and docs, not referenced by any
  `CMakeLists.txt`.
- `default.nix`, `flake.nix`, `flake.lock`, `.gitmodules`, `.gitignore` (upstream's),
  `CITATION.cff`, `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md` — repo metadata, not needed
  to build.
- Bundled docs/PDFs: `license_texts/ElmerIndividualCLA.pdf`,
  `license_texts/ElmerCorporateCLA.pdf` (contributor license agreements, not license
  texts governing the vendored code), `matc/doc/` (HTML+GIF manual, ~412 KB),
  `elmergrid/src/metis-5.1.0/manual/manual.pdf`,
  `fem/src/modules/contrib/ShellMultiSolver/ShellMutiSolverUserGuide.pdf`.

**Adjustment vs. the original trim plan:** `meshgen2d/` was not on the initial keep-list
but `elmerfem/CMakeLists.txt` calls `ADD_SUBDIRECTORY(meshgen2d)` unconditionally (no
guarding `WITH_*` flag), so it is a hard build requirement and was kept. Likewise
`cpack/` is included unconditionally by `INCLUDE(${CMAKE_CURRENT_SOURCE_DIR}/cpack/ElmerCPack.cmake)`
unless `-DBYPASS_CPACK` is set (which `build.sh` does not do), so it was kept rather than
adding an extra flag not in the original recipe. Both are small (360 KB and 20 KB).

Trimmed `elmerfem/` size: ~58 MB (from ~370 MB unpacked upstream, dominated by
`fem/tests/` at 128 MB and `elmerice/` at 67 MB).

## Build configuration

`build.sh` configures: no MPI (`WITH_MPI=OFF`), OpenMP on, no ElmerIce, no ElmerGUI, no
contributed solvers, `BUILD_TESTING=OFF` (see trim note above), reference BLAS/LAPACK
from `lapack/` (not system BLAS/LAPACK — `FindBLAS`/`FindLAPACK` are satisfied by the
explicit `-DBLAS_LIBRARIES`/`-DLAPACK_LIBRARIES` cache vars). The `WITH_*` CMake flag
names in `build.sh` were verified against the vendored tree's `CMakeLists.txt`
(`grep -n "SET(WITH_" CMakeLists.txt`) — all match the brief's assumed names, no renames
were needed. Needs gfortran + gcc/g++ + cmake + make (build-time only). Tested with
gfortran/gcc/g++ 13, cmake system version.

## Smoke fixture

`test/mesh/` is a hand-written 8-node, 5-tetrahedron unit cube (positively-oriented
linear tets, element type 504; 4 triangular boundary faces, type 303) — not extracted
from any upstream example. `test/case.sif` drives `StressSolver` (linear elasticity,
`ν = 0`) with the bottom face (z=0) fully fixed and a uniform traction on the top face
(z=1) normalized by area, so the coarse mesh is *exact* (mesh-independent) for the
uniaxial-pull analytic solution `u_z = σL/E = 1e6·1/1e9 = 1e-3 m`. This case is also the
golden fixture for the Go native mesh writer (Task 9).

One deviation from the brief's verbatim `case.sif`: `Force 3 Normalize by Area = True`
failed to parse (`ERROR:: LoadInputFile: Unknown specifier:[true]`). Root cause: Elmer's
SIF parser (`fem/src/ModelDescription.F90`, `SectionContents`/`CheckKeyWord`) only
infers a bare `= True`/`= False` value's type from ElmerSolver's built-in keyword
database; `"<keyword> Normalize by Area"` is a generic, unlisted companion-keyword
mechanism (`ListTagKeywords` in `fem/src/Lists.F90`, described in
`fem/src/MainUtils.F90`), so its type must be given explicitly. Fixed by writing
`Force 3 Normalize by Area = Logical True`. Confirmed via a clean solve that the BC then
applies correctly (`BC weight: 2  0.99999999999999989` printed at solve time, i.e. area
normalization by ~1.0, as expected for a 1×1 unit face) and the peak result matches the
analytic value exactly.

`test/run.sh` also required a path fix (not present in the original brief's script):
`ResultOutputSolver` writes `case_t0001.vtu` under the mesh directory
(`Header > Mesh DB "mesh" "."`, i.e. `test/mesh/`), not next to `case.sif`. `run.sh`
globs `mesh/case*.vtu` accordingly. The VTU `displacement` DataArray name matched the
brief's assumed `Name="displacement"` exactly — no regex change needed there.

**Verified smoke result:** `smoke OK: peak displacement 0.001` (exact match to
`1.0e-3`, well inside the ±1e-5 tolerance — in fact bit-for-bit `0.001` given ν=0
uniaxial exactness on this mesh).

**Verified ElmerGrid round-trip:**
`install/bin/ElmerGrid 2 2 test/mesh -out /tmp/eg-roundtrip` exits 0 and re-emits
`mesh.{header,nodes,elements,boundary}`, confirming the hand-written mesh files are
valid ElmerSolver-format input.

## Re-vendoring

Download the upstream elmerfem release tarball (`gh api
repos/ElmerCSC/elmerfem/tarball/<tag>`), record its SHA-256, extract, copy the kept
subtrees listed above here, and re-run `build.sh`. LAPACK only needs updating if the ccx
vendor's LAPACK is updated (re-copy `Oblikovati.AddIns.CalculiX/vendor-src/ccx/lapack-3.8.0`).
Validate by running `test/run.sh` (expects `smoke OK: peak displacement 0.001`) and the
ElmerGrid round-trip command above.
