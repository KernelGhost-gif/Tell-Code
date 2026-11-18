#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LLVM_SOURCE="${LLVM_SOURCE:-$ROOT/../LLVM}"

if [[ $# -eq 0 ]]; then set -- x86_64 aarch64; fi
for arch in "$@"; do
  case "$arch" in
    x86_64|aarch64) ;;
    *) echo "usage: build-tellc-linux.sh [x86_64] [aarch64]" >&2; exit 2 ;;
  esac
done
if [[ ! -f "$LLVM_SOURCE/llvm/CMakeLists.txt" || ! -f "$LLVM_SOURCE/lld/CMakeLists.txt" ]]; then
  echo "Set LLVM_SOURCE to an llvm-project checkout containing llvm and lld." >&2
  exit 2
fi
LLVM_SOURCE="$(cd "$LLVM_SOURCE" && pwd)"
if ! ZIG="$(command -v "${ZIG:-zig}")"; then
  echo "Set ZIG to the Zig 0.16.0 executable or add it to PATH." >&2
  exit 2
fi
ZIG="$(cd "$(dirname "$ZIG")" && pwd)/$(basename "$ZIG")"
if [[ "$("$ZIG" version)" != 0.16.0 ]]; then
  echo "The standalone Linux build requires Zig 0.16.0." >&2
  exit 2
fi

mkdir -p "${TELL_BUILD_ROOT:-$ROOT/build/tellc-linux}"
BUILD_ROOT="$(cd "${TELL_BUILD_ROOT:-$ROOT/build/tellc-linux}" && pwd)"
JOBS="${JOBS:-4}"
export ZIG_GLOBAL_CACHE_DIR="$BUILD_ROOT/zig-global"
export ZIG_LOCAL_CACHE_DIR="$BUILD_ROOT/zig-local"

mkdir -p "$BUILD_ROOT/tools"
for tool in ar ranlib; do
  printf '#!/usr/bin/env bash\nexec %q %s "$@"\n' "$ZIG" "$tool" > "$BUILD_ROOT/tools/llvm-$tool"
  chmod +x "$BUILD_ROOT/tools/llvm-$tool"
done

llvm_options=(
  -DCMAKE_BUILD_TYPE=Release '-DLLVM_TARGETS_TO_BUILD=X86;AArch64'
  -DLLVM_INCLUDE_TESTS=OFF -DLLVM_INCLUDE_BENCHMARKS=OFF
  -DLLVM_ENABLE_ZLIB=OFF -DLLVM_ENABLE_ZSTD=OFF -DLLVM_ENABLE_LIBXML2=OFF
  -DLLVM_ENABLE_LIBEDIT=OFF -DLLVM_ENABLE_LIBPFM=OFF -DLLVM_ENABLE_BACKTRACES=OFF
  -DLLVM_ENABLE_Z3_SOLVER=OFF -DCMAKE_DISABLE_FIND_PACKAGE_LibLZMA=TRUE
)
HOST_TOOLS="${LLVM_NATIVE_TOOL_DIR:-$BUILD_ROOT/host-llvm/bin}"
if [[ -z "${LLVM_NATIVE_TOOL_DIR:-}" ]]; then
  cmake -S "$LLVM_SOURCE/llvm" -B "$BUILD_ROOT/host-llvm" -G Ninja "${llvm_options[@]}"
  cmake --build "$BUILD_ROOT/host-llvm" --target llvm-min-tblgen llvm-tblgen --parallel "$JOBS"
fi

for arch in "$@"; do
  target_build="$BUILD_ROOT/$arch"
  toolchain=(
    "-DCMAKE_TOOLCHAIN_FILE=$ROOT/TellC/cmake/linux-zig.cmake"
    "-DTELL_ZIG=$ZIG" "-DTELL_LINUX_ARCH=$arch"
    "-DCMAKE_AR=$BUILD_ROOT/tools/llvm-ar" "-DCMAKE_RANLIB=$BUILD_ROOT/tools/llvm-ranlib"
  )
  cmake -S "$LLVM_SOURCE/llvm" -B "$target_build/llvm" -G Ninja \
    "${llvm_options[@]}" "${toolchain[@]}" \
    "-DCMAKE_CXX_FLAGS_RELEASE=-O2 -DNDEBUG" \
    "-DLLVM_HOST_TRIPLE=$arch-unknown-linux-musl" "-DLLVM_DEFAULT_TARGET_TRIPLE=$arch-unknown-linux-musl" \
    "-DLLVM_NATIVE_TOOL_DIR=$HOST_TOOLS" -DLLVM_ENABLE_PROJECTS=lld -DLLVM_BUILD_TOOLS=OFF \
    -DBUILD_SHARED_LIBS=OFF -DLLVM_BUILD_LLVM_DYLIB=OFF -DLLVM_LINK_LLVM_DYLIB=OFF
  cmake --build "$target_build/llvm" --target lldELF --parallel "$JOBS"
  cmake -S "$ROOT/TellC" -B "$target_build/tellc" -G Ninja "${toolchain[@]}" \
    -DCMAKE_BUILD_TYPE=Release -DBUILD_TESTING=OFF -DTELL_STANDALONE_LINUX=ON \
    "-DTELL_ZIG_CACHE_DIR=$ZIG_GLOBAL_CACHE_DIR" \
    "-DLLVM_DIR=$target_build/llvm/lib/cmake/llvm" "-DLLD_DIR=$target_build/llvm/lib/cmake/lld"
  cmake --build "$target_build/tellc" --target tellc --parallel "$JOBS"
  python3 "$ROOT/TellC/tests/standalone_linux.py" "$target_build/tellc/tellc" "$arch" --elf-only
  install -m 755 "$target_build/tellc/tellc" "$BUILD_ROOT/tellc-linux-$arch"
  printf 'Standalone compiler: %s\n' "$BUILD_ROOT/tellc-linux-$arch"
done
