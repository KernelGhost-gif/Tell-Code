#!/usr/bin/env bash
set -euo pipefail

# LLVM_SOURCE must point to an llvm-project checkout matching TellC's LLVM API.
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LLVM_SOURCE="${LLVM_SOURCE:-$ROOT/../LLVM}"
BUILD_ROOT="${TELL_BUILD_ROOT:-$ROOT/build/tellc-macos-arm64}"
JOBS="${JOBS:-4}"
if [[ "$(uname -s)" != Darwin || "$(uname -m)" != arm64 ]]; then
  echo "This build requires an Apple silicon Mac." >&2
  exit 1
fi
cmake -S "$LLVM_SOURCE/llvm" -B "$BUILD_ROOT/llvm" -G Ninja \
  -DCMAKE_BUILD_TYPE=Release -DCMAKE_OSX_DEPLOYMENT_TARGET=11.0 -DCMAKE_OSX_ARCHITECTURES=arm64 \
  -DLLVM_ENABLE_PROJECTS=lld -DLLVM_TARGETS_TO_BUILD=AArch64 \
  -DLLVM_INCLUDE_TESTS=OFF -DLLVM_INCLUDE_BENCHMARKS=OFF \
  -DLLVM_ENABLE_ZSTD=OFF -DLLVM_ENABLE_ZLIB=OFF -DLLVM_ENABLE_LIBXML2=OFF \
  -DCMAKE_DISABLE_FIND_PACKAGE_LibLZMA=TRUE
cmake --build "$BUILD_ROOT/llvm" --target lldMachO lldCommon LLVMAArch64CodeGen LLVMAArch64AsmParser LLVMLTO LLVMPasses LLVMOrcJIT -j "$JOBS"
cmake -S "$ROOT/TellC" -B "$BUILD_ROOT/tellc" -G Ninja \
  -DCMAKE_BUILD_TYPE=Release -DCMAKE_OSX_DEPLOYMENT_TARGET=11.0 -DCMAKE_OSX_ARCHITECTURES=arm64 \
  -DLLVM_DIR="$BUILD_ROOT/llvm/lib/cmake/llvm" \
  -DLLD_SOURCE_DIR="$LLVM_SOURCE/lld" -DLLD_BUILD_DIR="$BUILD_ROOT/llvm"
cmake --build "$BUILD_ROOT/tellc" -j "$JOBS"
ctest --test-dir "$BUILD_ROOT/tellc" --output-on-failure
printf 'Standalone compiler: %s\n' "$BUILD_ROOT/tellc/tellc"
printf 'Language server: %s\n' "$BUILD_ROOT/tellc/tell-lsp"
