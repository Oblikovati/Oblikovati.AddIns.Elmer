#!/usr/bin/env bash
# Build the vendored gmsh volume mesher (CLI) entirely from the in-repo sources — no
# network, no external libraries (gmsh bundles its meshing engines under contrib/:
# Hxt, TetGen, Netgen, Metis, …). Produces a self-contained `gmsh` executable in
# ./build that the add-in runs as a subprocess (OBK_GMSH_BIN / vendor-src/gmsh/build)
# to turn a watertight surface STL into a solid tetrahedral (C3D10) mesh.
#
# Minimal configuration: no GUI (FLTK), no OpenCASCADE (we feed STL, not B-rep/STEP),
# no PETSc/SLEPc/MPI. Needs a C/C++ compiler (build-time only). Tested with g++ 13.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
JOBS="${JOBS:-$( (nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null) || echo 4)}"

cmake -S "$HERE" -B "$HERE/build" \
  -DCMAKE_BUILD_TYPE=Release \
  -DENABLE_FLTK=OFF -DENABLE_OCC=OFF \
  -DENABLE_PETSC=OFF -DENABLE_SLEPC=OFF -DENABLE_MPI=OFF \
  -DENABLE_BUILD_DYNAMIC=OFF -DENABLE_BUILD_SHARED=OFF

cmake --build "$HERE/build" --target gmsh -j"$JOBS"
echo "### built $HERE/build/gmsh"
