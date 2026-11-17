# TellC

A C++17 compiler for an explicitly typed variant of Tell. It parses source into an AST and generates LLVM IR and native executables for the supported subset.

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

Build from the repository root with CMake and a compatible LLVM build. TellC builds with LLVM 24 development sources on Apple Silicon and LLVM 21 on Debian x86-64. The current command-line driver targets macOS and Linux and requires Clang plus a matching LLVM `llc` on `PATH`. Set `LLVM_DIR` to the directory that contains `LLVMConfig.cmake`.

```sh
cmake -S TellC -B TellC/build -DLLVM_DIR=/path/to/llvm-build/lib/cmake/llvm
cmake --build TellC/build
TellC/build/tellc TellC/examples/objects.tell -o macbook
```

The default output is `a.out`. TellC uses `llc` from the configured LLVM build for object generation and Clang on `PATH` to link. The LLVM build needs an `llc` binary with a backend for the target machine. The public parser API is `tell::parse` in `include/tell/parser.hpp`.

On Debian x86-64 with LLVM 21 installed, configure CMake with
`-DLLVM_DIR=/usr/lib/llvm-21/lib/cmake/llvm`. A native installation defaults
to the `llc` next to that LLVM build. For a cross-built compiler, set
`-DTELL_LLC_PATH=llc-21` so it finds the matching tool on the Debian machine's
`PATH`. TellC uses `clang` on `PATH` for linking on both macOS and Linux.

Run parser and compiler integration tests with:

```sh
ctest --test-dir TellC/build --output-on-failure
```

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
