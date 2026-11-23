# Building TellC

The standalone macOS and Linux builds embed LLVM code generation and LLD.
The external-toolchain build uses an installed LLVM and Clang. Standalone mode
is enabled by default only on Apple silicon macOS.

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

## External toolchain on Linux and macOS

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

## Standalone Linux

Build from macOS or Linux with [Zig 0.16.0](https://ziglang.org/download/),
CMake 3.31 or later, Ninja, Python 3, a native C++ compiler, and an LLVM 24
source tree. The builds here used LLVM revision
`7090701baa47d03b1e9877eff3b4e8f032b99946`. The script builds TableGen for
the build host and cross-compiles LLVM, LLD, and TellC directly for Linux.

```sh
LLVM_SOURCE=/path/to/llvm-project ./scripts/build-tellc-linux.sh
```

This writes `build/tellc-linux/tellc-linux-x86_64` and
`build/tellc-linux/tellc-linux-aarch64`. Pass `x86_64` or `aarch64` to build
one binary. Set `ZIG` to the Zig executable, `TELL_BUILD_ROOT` to change the
output directory, or `JOBS` to control parallelism. If you already have
`llvm-tblgen` and `llvm-min-tblgen` built on the host from the same LLVM
revision, set `LLVM_NATIVE_TOOL_DIR` to their directory.

Copy the compiler for the machine's architecture to Linux and run it directly.
Both compilers embed LLVM, LLD, and static musl and C++ runtime inputs for both
targets. The compiler defaults to its own architecture. Use `--target` to
select the other architecture:

```sh
./tellc-linux-x86_64 TellC/examples/factorial.tell -o factorial
./tellc-linux-x86_64 TellC/examples/factorial.tell --target aarch64 -o factorial-arm64
```

The compiler and its outputs are static ELF executables. Compiling uses
anonymous files through `memfd_create` (available since Linux 3.17) and
`/proc/self/fd`, which requires procfs mounted. Compilation requires no installed
LLVM, Clang, system libraries, SDK, or writable temporary directory.
With `--link`, supply ELF objects or static archives built for the selected
architecture and musl ABI. C++ archives should use Zig's libc++ ABI.
When redistributing the compiler, include Tell's license, LLVM's
`LICENSE.TXT`, and the license notices for the Zig and musl runtimes.

Run the standalone smoke test on matching Linux hardware:

```sh
python3 TellC/tests/standalone_linux.py ./tellc-linux-x86_64 x86_64
```

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

## Native Windows x64 build

TellC can be built and run natively on 64-bit Windows with the external LLVM
toolchain. This build produces a Windows PE compiler executable and uses
installed LLVM tools plus the Microsoft C++ linker; unlike the standalone Linux
and Apple silicon builds, it does not embed LLVM or LLD.

Install:

- Visual Studio 2022 or Build Tools with the Desktop development with C++
  workload and a Windows SDK;
- CMake and PowerShell;
- an x64 LLVM installation containing LLVMConfig.cmake, clang.exe, and llc.exe.

From the repository root, run:

~~~powershell
choco install llvm --no-progress -y
.\scripts\build-tellc-windows.ps1
~~~

The script discovers LLVM from LLVM_ROOT or llc.exe on PATH. To select a
specific LLVM installation or build directory:

~~~powershell
$env:LLVM_ROOT = "C:\Program Files\LLVM"
.\scripts\build-tellc-windows.ps1 -LLVMRoot $env:LLVM_ROOT -BuildRoot build\tellc-windows-x64 -Jobs 4
~~~

It configures the Visual Studio 2022 x64 generator, builds TellC and the language
server, runs the full CTest suite, and compiles and executes the factorial
example as a native Windows smoke test. The compiler is produced at
build/tellc-windows-x64/Release/tellc.exe by default. Keep the matching LLVM
installation, Clang, llc, and Visual Studio linker available when using the
external-toolchain compiler.

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
