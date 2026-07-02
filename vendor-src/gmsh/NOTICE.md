# Vendored gmsh — provenance & license

This directory vendors **gmsh** in source form so the add-in builds a self-contained
volume mesher with no build-time or runtime external dependencies. `build.sh` compiles it
into `build/gmsh`; the add-in runs that binary as a subprocess to turn a watertight
surface STL into a solid tetrahedral (C3D10) mesh.

| Component | Version | Upstream | SHA-256 of source archive | License |
|---|---|---|---|---|
| gmsh | 4.13.1 | https://gmsh.info/src/gmsh-4.13.1-source.tgz | `77972145f431726026d50596a6a44fb3c1c95c21255218d66955806b86edbe8d` | GPL-2.0-or-later (see `LICENSE.txt`, `CREDITS.txt`) |

gmsh is **GPL-2.0-or-later**, compatible with this GPL-2.0-only repository. It bundles its
own meshing engines and helper libraries under `contrib/` (Hxt, TetGen, Netgen, Metis,
ANN, Eigen, …), each under its own license documented in `CREDITS.txt` / the respective
`contrib/*/` subdirectories — these are **not** governed by this repo's GPL.

## Vendored subtree (what was kept / dropped)

Kept: `src/`, `contrib/` (the meshing engines), `api/`, `utils/`, `doc/` (referenced by
the CMake configure step), `cmake/`, `CMakeLists.txt`, and the license/credits/readme
files. Dropped to save space: `examples/`, `tutorials/`, `benchmarks/` (not needed to
build the CLI).

## Build configuration

`build.sh` configures a minimal CLI: **no GUI (FLTK), no OpenCASCADE** (we feed gmsh an
STL surface, not a B-rep/STEP solid), no PETSc/SLEPc/MPI. The bundled 3D Delaunay (Hxt) /
TetGen / Netgen engines provide the tetrahedral meshing the add-in needs. Needs a C/C++
compiler + cmake (build-time only). Tested with g++ 13.

## Re-vendoring

Download the upstream archive, record its SHA-256 above, extract, copy the kept subtrees
here, and re-run `build.sh`. Validate by meshing a box STL into a watertight tet mesh
(`gmsh box.geo -3`).
