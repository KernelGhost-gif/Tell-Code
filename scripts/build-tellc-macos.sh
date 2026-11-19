#!/usr/bin/env bash
set -euo pipefail

# Build LLVM + LLD and TellC from one matching llvm-project checkout.
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LLVM_SOURCE="${LLVM_SOURCE:-$ROOT/../LLVM}"
BUILD_ROOT="${TELL_BUILD_ROOT:-$ROOT/build/tellc-macos-arm64}"
JOBS="${JOBS:-2}"

fail() {
  echo "build-tellc-macos: $*" >&2
  exit 1
}

require_tool() {
  command -v "$1" >/dev/null 2>&1 || fail "required tool '$1' was not found"
}

if [[ "$(uname -s)" != Darwin || "$(uname -m)" != arm64 ]]; then
  fail "this build requires an Apple silicon Mac (Darwin arm64)"
fi

require_tool cmake
require_tool ninja
require_tool python3
require_tool xcrun

if [[ ! "$JOBS" =~ ^[1-9][0-9]*$ ]]; then
  fail "JOBS must be a positive integer"
fi

if [[ ! -f "$LLVM_SOURCE/llvm/CMakeLists.txt" || ! -f "$LLVM_SOURCE/lld/CMakeLists.txt" ]]; then
  fail "LLVM_SOURCE must point to an llvm-project checkout containing llvm/ and lld/"
fi

CMAKE_VERSION="$(cmake --version | sed -n '1s/^cmake version //p')"
python3 - "$CMAKE_VERSION" <<'PY'
import sys

try:
    version = tuple(int(part) for part in sys.argv[1].split(".")[:3])
except ValueError:
    raise SystemExit("build-tellc-macos: could not parse the CMake version")
if version < (3, 31, 0):
    raise SystemExit(
        "build-tellc-macos: LLVM 24 requires CMake 3.31 or newer; "
        f"found {sys.argv[1]}"
    )
PY

mkdir -p "$BUILD_ROOT"
echo "LLVM source: $LLVM_SOURCE"
if git -C "$LLVM_SOURCE" rev-parse --short HEAD >/dev/null 2>&1; then
  echo "LLVM revision: $(git -C "$LLVM_SOURCE" rev-parse --short HEAD)"
else
  echo "LLVM revision: source checkout has no Git metadata"
fi
echo "Build directory: $BUILD_ROOT"
echo "Parallel jobs: $JOBS"

cmake -S "$LLVM_SOURCE/llvm" -B "$BUILD_ROOT/llvm" -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_OSX_DEPLOYMENT_TARGET=11.0 \
  -DCMAKE_OSX_ARCHITECTURES=arm64 \
  -DLLVM_ENABLE_PROJECTS=lld \
  -DLLVM_TARGETS_TO_BUILD=AArch64 \
  -DLLVM_INCLUDE_TESTS=OFF \
  -DLLVM_INCLUDE_BENCHMARKS=OFF \
  -DLLVM_INCLUDE_EXAMPLES=OFF \
  -DLLVM_INCLUDE_DOCS=OFF \
  -DLLVM_ENABLE_ZSTD=OFF \
  -DLLVM_ENABLE_ZLIB=OFF \
  -DLLVM_ENABLE_LIBXML2=OFF \
  -DLLVM_ENABLE_LIBEDIT=OFF \
  -DLLVM_ENABLE_TERMINFO=OFF \
  -DCMAKE_DISABLE_FIND_PACKAGE_LibLZMA=TRUE

cmake --build "$BUILD_ROOT/llvm" --parallel "$JOBS" --target \
  lldMachO lldCommon LLVMAArch64CodeGen LLVMAArch64AsmParser \
  LLVMLTO LLVMPasses LLVMOrcJIT

cmake -S "$ROOT/TellC" -B "$BUILD_ROOT/tellc" -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_OSX_DEPLOYMENT_TARGET=11.0 \
  -DCMAKE_OSX_ARCHITECTURES=arm64 \
  -DLLVM_DIR="$BUILD_ROOT/llvm/lib/cmake/llvm" \
  -DLLD_SOURCE_DIR="$LLVM_SOURCE/lld" \
  -DLLD_BUILD_DIR="$BUILD_ROOT/llvm" \
  -DTELL_STANDALONE_MACOS=ON

cmake --build "$BUILD_ROOT/tellc" --parallel "$JOBS"
ctest --test-dir "$BUILD_ROOT/tellc" --output-on-failure

printf 'Standalone compiler: %s\n' "$BUILD_ROOT/tellc/tellc"
printf 'Language server: %s\n' "$BUILD_ROOT/tellc/tell-lsp"
