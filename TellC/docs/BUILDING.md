# Building TellC

TellC has two build modes. The Apple silicon standalone build embeds LLVM code
generation and LLD. Linux and Intel macOS use an installed LLVM toolchain plus
Clang. Standalone mode is enabled by default only on Apple silicon.

## Requirements

All build modes require a C++17 compiler, CMake, Python 3, and an LLVM
development package containing its CMake configuration (LLVMConfig.cmake).

The external-toolchain mode also requires a matching llc and Clang on PATH.
Use the same LLVM release for the headers, libraries, and llc executable.

The Apple silicon standalone mode additionally requires:

- an Apple silicon Mac (arm64);
- Xcode command-line tools and the macOS SDK for building;
- CMake 3.31 or newer, Ninja, Python 3, and Git;
- an llvm-project source checkout containing both llvm/ and lld/.

The LLVM and LLD source tree and build artifacts must come from the same
revision. The build script disables optional compression and documentation
dependencies to keep the build smaller and avoid runtime shared-library
dependencies.

## Linux and Intel macOS

Install LLVM and Clang for your platform, then configure and build:

~~~sh
cmake -S TellC -B build/tellc \
  -DLLVM_DIR=/path/to/lib/cmake/llvm \
  -DTELL_LLC_PATH=/path/to/bin/llc
cmake --build build/tellc --parallel
ctest --test-dir build/tellc --output-on-failure
~~~

On Intel macOS, TELL_STANDALONE_MACOS defaults to OFF and the compiler uses the
external-toolchain path. Set -DTELL_STANDALONE_MACOS=OFF explicitly when
configuring an Intel Mac if the option was previously cached as ON.

## Apple silicon standalone compiler

From the repository root, point LLVM_SOURCE to a matching llvm-project
checkout:

~~~sh
LLVM_SOURCE=/path/to/llvm-project ./scripts/build-tellc-macos.sh
build/tellc-macos-arm64/tellc/tellc TellC/examples/factorial.tell -o factorial
./factorial
~~~

The factorial example prints 120. The script builds the AArch64 LLVM backend
and Mach-O linker, then compiles TellC against those exact build artifacts.
It accepts:

- LLVM_SOURCE: llvm-project source directory (default: ../LLVM beside this repo);
- TELL_BUILD_ROOT: build directory (default: build/tellc-macos-arm64);
- JOBS: number of parallel build jobs (default: 2).

The compiler and generated executable target macOS 11 or later. At runtime,
the standalone compiler does not invoke Clang, llc, LLD, a shell, or a
temporary directory. It writes only the requested executable.

The embedded Mach-O linker currently uses an in-memory libSystem stub for
the C ABI symbols emitted by the current backend. When the backend introduces
new system imports, update the stub and its tests in the same change. Code
signing and notarization may be needed when distributing the compiler outside
your own Mac.

## Troubleshooting

**CMake cannot find LLVM.** Set LLVM_DIR to the directory containing
LLVMConfig.cmake, not to the LLVM installation root.

**LLVM and LLD libraries cannot be linked.** Ensure LLD_SOURCE_DIR and
LLD_BUILD_DIR point to the same llvm-project revision used for LLVM_DIR.
Rebuild with the repository script rather than mixing package-manager
libraries with a source build.

**The standalone build rejects the host.** It is currently supported only on
Apple silicon. Intel Macs should use the external-toolchain instructions.

**A generated program fails to link after adding a runtime call.** Check the
symbols referenced by the LLVM IR and update the embedded libSystem stub and
the standalone test before adding the new dependency to TellC.

## CI coverage

The TellC workflow exercises Linux with LLVM 21, Apple silicon with the
embedded LLVM/LLD build, and Intel macOS with the external-toolchain build.
The Apple silicon job checks out one pinned llvm-project revision for both
LLVM and LLD, then runs the standalone no-toolchain smoke test.
