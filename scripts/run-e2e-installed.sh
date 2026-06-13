#!/usr/bin/env bash
# Run the E2E test suite against an already-installed ShibuDB runtime.
#
# A ShibuDB install (Linux package or scripts/install-linux.sh) ships only the
# FAISS *runtime* shared libraries under <prefix>/lib -- it does NOT install the
# FAISS development headers. The E2E suite imports go-faiss, which compiles via
# CGO, so `go test ./E2ETests` must be told where the FAISS headers and shared
# libraries live. We point CGO at the headers bundled in the source tree
# (resources/lib/include) and link against the installed shared libraries.
#
# Usage: scripts/run-e2e-installed.sh [source_dir]
#   source_dir defaults to the current directory.
set -euo pipefail

SOURCE_DIR="${1:-$(pwd)}"
SOURCE_DIR="$(cd "$SOURCE_DIR" && pwd)"
PREFIX="${SHIBUDB_PREFIX:-/usr/local}"
LIB_DIR="$PREFIX/lib"
INCLUDE_DIR="$SOURCE_DIR/resources/lib/include"

if [[ ! -f "$INCLUDE_DIR/faiss/c_api/AutoTune_c.h" ]]; then
	echo "FAISS development headers not found under $INCLUDE_DIR" >&2
	echo "Run this from a ShibuDB source checkout that bundles resources/lib/include." >&2
	exit 1
fi
if [[ ! -f "$LIB_DIR/libfaiss_c.so" ]]; then
	echo "Installed FAISS runtime library not found at $LIB_DIR/libfaiss_c.so" >&2
	echo "Install ShibuDB first (package or scripts/install-linux.sh)." >&2
	exit 1
fi

cd "$SOURCE_DIR"
echo "Running E2E tests from $SOURCE_DIR against FAISS libraries in $LIB_DIR..."

# -lgomp/-lstdc++ are dependencies of the bundled libfaiss.so and are provided by
# the GCC toolchain; OpenBLAS is intentionally omitted because the bundled
# libfaiss.so does not link it directly (resolved transitively at runtime).
CGO_ENABLED=1 \
CGO_CFLAGS="-I$INCLUDE_DIR" \
CGO_CXXFLAGS="-I$INCLUDE_DIR" \
CGO_LDFLAGS="-L$LIB_DIR -lfaiss -lfaiss_c -lstdc++ -lm -lgomp" \
LD_LIBRARY_PATH="$LIB_DIR:${LD_LIBRARY_PATH:-}" \
	go test -v -buildvcs=false ./E2ETests
