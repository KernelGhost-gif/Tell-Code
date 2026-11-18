# TellC

A C++17 compiler for an explicitly typed variant of Tell. It parses source into an AST and generates LLVM IR and native executables for the supported subset.

Platform setup, build requirements, and test coverage are documented in [the documentation index](docs/README.md), [the build guide](docs/BUILDING.md), [the testing guide](docs/TESTING.md), and [the architecture guide](docs/ARCHITECTURE.md).

```tell
let text name be "Steve Jobs"; let number age be 56
let decimal height be 1.88
let bool alive be false
say name
```

Newlines terminate statements; semicolons separate statements on the same line. Keywords and built-in type names are case insensitive. Object type names are case sensitive.

The parser recognizes typed declarations, assignment statements, output, returns, functions, if/otherwise, while, literals, lists, calls, member and index access, and common symbolic and word operators. The executable backend intentionally implements a smaller subset. Syntax outside the parser subset is reported as a parse error; syntax represented by the parser but not yet implemented by the backend is rejected during compilation with a source-located diagnostic. Natural-language comparison phrases, classes, imports, and named call arguments are not currently supported. Strings support basic escapes; source columns count bytes.

**Compiled functions and methods are strongly typed.** Parameters must use `type: name` and every function must declare its result with `out type` (use `out void` for no result). For example:

```tell
to factorial(number: n) out number:
    if n <= 1:
        return 1
    end if
    return n * factorial(n - 1)
end function
```

The compiler checks argument, initializer, and return types. It does not silently infer or weaken function signatures to accommodate untyped code.

On Apple silicon, build the standalone compiler from the repository root:

```sh
LLVM_SOURCE=/path/to/llvm-project ./scripts/build-tellc-macos.sh
build/tellc-macos-arm64/tellc/tellc TellC/examples/objects.tell -o macbook
```

The script needs CMake, Ninja, Xcode command-line tools and the matching LLVM
24 source tree at build time. Set `TELL_BUILD_ROOT` to change its build
location and `JOBS` to control parallelism. The result is one ARM64 executable;
LLVM and the Mach-O linker are linked into it. The compiler generates its object
code and links it in memory, writing only the requested executable. It does not
need `clang`, `llc`, LLD, an SDK, or a temporary directory at runtime. The
compiler and its output target macOS 11 or later and use macOS system libraries.
The embedded linker consumes LLVM's internal Mach-O input cache, so its source
and libraries must come from the same LLVM revision as the compiler's headers.

For distribution outside your own Mac, Apple code signing and notarization may
be needed to avoid Gatekeeper warnings. A build made with newer LLVM libraries
or a newer deployment target may not run on macOS 11; use the script above to
build the dependencies and compiler together.

On Linux, build with CMake and a compatible LLVM installation. Set `LLVM_DIR` to
the directory containing `LLVMConfig.cmake`. TellC uses `llc` from that LLVM
build and Clang on `PATH` to link. For cross-built compilers, set
`-DTELL_LLC_PATH=llc-21` (or the matching tool) to find it on the target's
`PATH`. The public parser API is `tell::parse` in `include/tell/parser.hpp`. The separate [TellC language server](../lsp/README.md) provides editor diagnostics for this dialect.

Run parser and compiler integration tests with `ctest --test-dir <build-directory> --output-on-failure`.

The integration test compiles and runs the factorial and object examples, and verifies that invalid private access, parameter types, return types, and incomplete return paths fail compilation. The LLVM-backed test job runs on Linux in GitHub Actions.

## Foreign function declarations

TellC can represent declarations for functions supplied by C or Swift libraries:

```tell
extern c function add(int32 left, int32 right) returns int32 as "c_add"
extern swift function double_value(float64 value) returns float64 as "swift_double_value"
let number result be add(2, 3)
```

The optional `as` string gives the exact external symbol; without it, the Tell name is used. Both forms describe a **C ABI** function: a Swift implementation needs an explicitly exported C-compatible entry point. Accepted ABI types are `int32`, `int64`, `float32`, and `float64`, with `void` allowed as a return type. These represent C `int32_t`, `int64_t`, `float`, and `double`. Pointers, strings, structures, callbacks, variadic functions, ownership, and Swift-native calling conventions are not supported by these declarations.

This is parser and AST support only. There is no foreign function type checking, conversion policy between Tell and ABI types, foreign library linking, or foreign call code generation yet; declaring a function does not make calls executable. Those pieces must be implemented before C or Swift interoperability works at runtime.

## Executable backend

The backend currently supports typed declarations, output with `{name}` interpolation, typed functions and methods, calls, returns, object construction, field access, equality, signed integer arithmetic, and `if` blocks. `number` is a signed 64-bit integer and `decimal` is a 64-bit floating-point value. Text equality compares contents; object equality compares identity. Assignment, loops, decimal arithmetic, lists/indexing, unary operators, and foreign calls still require code generation support.

## Objects and typed function signatures

Declare fields with `let TYPE self NAME be VALUE`. Members in `own` are private to methods of that object type. Members in `others` are public.

```tell
object MacBook:
    own:
        let text self password be "12345678"
    others:
        to check password(text: Password) out bool:
            if Password = password:
                give back true
            end if
            give back false
        end function
end object

let MacBook laptop be MacBook()
say laptop.check password("12345678")
say laptop.check password("wrong")
```

This program prints `true` and then `false`. `MacBook()` constructs an instance using the field initializers. Initializers run in declaration order without a `self` receiver. Instances live until the process exits. Inside methods, use `self.password` for explicit access or `password` when a local name does not shadow it.

Parameters use `type: name` and are separated by commas. `out` declares the return type, including `out void` for functions without a result. Arguments, initializers, and return values must match the declared types. A function with a result must return a value on every path. The parser still accepts the legacy `to name with parameter:` form for parser compatibility, but the executable backend intentionally rejects untyped function signatures.
