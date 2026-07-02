#!/usr/bin/env bash
# Builds a self-contained ElmerSolver + ElmerGrid from the vendored source.
# Deps: gfortran, gcc/g++, cmake, make. No MPI, no MUMPS/Hypre, no GUI.
set -euo pipefail
cd "$(dirname "$0")"

# 1. reference BLAS/LAPACK (same recipe as the ccx vendor, split into two archives)
./lapack/build.sh   # produces lapack/librefblas.a lapack/liblapack.a

# 2. Elmer
# NOTE: elmerfem/CMakeLists.txt does `include(CTest)`, which defaults BUILD_TESTING to
# ON. We trimmed fem/tests/ out of the vendored tree (see NOTICE.md), so BUILD_TESTING
# must be forced OFF or the configure step fails on a missing add_subdirectory target.
cmake -B build -S elmerfem \
  -DCMAKE_BUILD_TYPE=Release \
  -DBUILD_TESTING=OFF \
  -DWITH_MPI=OFF -DWITH_OpenMP=ON \
  -DWITH_ElmerIce=OFF -DWITH_ELMERGUI=OFF \
  -DWITH_CONTRIB=OFF \
  -DBLAS_LIBRARIES="$PWD/lapack/librefblas.a" \
  -DLAPACK_LIBRARIES="$PWD/lapack/liblapack.a;$PWD/lapack/librefblas.a" \
  -DCMAKE_INSTALL_PREFIX="$PWD/install"
cmake --build build -j"$(nproc)"
cmake --install build
