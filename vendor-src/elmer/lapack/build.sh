#!/usr/bin/env bash
# Builds reference BLAS + LAPACK 3.8.0 (vendored, pure fixed-form Fortran, no CMake)
# into two static archives that the top-level build.sh hands to Elmer's CMake config
# as -DBLAS_LIBRARIES / -DLAPACK_LIBRARIES.
#
# Same recipe (source + compiler flags) as $CCX/vendor-src/ccx/build.sh's LAPACK step,
# adapted to produce librefblas.a and liblapack.a as SEPARATE archives (ccx links them
# as one combined liblapack.a; Elmer's CMake FindBLAS/FindLAPACK expects BLAS_LIBRARIES
# and LAPACK_LIBRARIES to be independently satisfiable, so we split BLAS/SRC from SRC).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FC="${FC:-$(command -v gfortran gfortran-14 gfortran-13 gfortran-12 gfortran-11 2>/dev/null | head -1)}"
FC="${FC:-gfortran}"
SRC="$HERE/lapack-3.8.0"

echo "### [1/2] reference BLAS -> librefblas.a"
( cd "$SRC" && rm -f ./*.o
  $FC -O2 -fcommon -fallow-argument-mismatch -c BLAS/SRC/*.f
  ar rcs "$HERE/librefblas.a" ./*.o && rm -f ./*.o )

echo "### [2/2] reference LAPACK -> liblapack.a"
( cd "$SRC" && rm -f ./*.o
  $FC -O2 -fcommon -fallow-argument-mismatch -c \
     SRC/*.f \
     INSTALL/dlamch.f INSTALL/slamch.f \
     INSTALL/second_INT_ETIME.f INSTALL/dsecnd_INT_ETIME.f
  ar rcs "$HERE/liblapack.a" ./*.o && rm -f ./*.o )

echo "### built $HERE/librefblas.a $HERE/liblapack.a"
