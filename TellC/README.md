# TellC

A C++17 compiler for an explicitly typed variant of Tell. It parses source into an AST and generates LLVM IR and native executables for the supported subset.

Platform setup, build requirements, and test coverage are documented in [the documentation index](docs/README.md), [the build guide](docs/BUILDING.md), [the testing guide](docs/TESTING.md), and [the architecture guide](docs/ARCHITECTURE.md).

```tell
let text service_name be "Customer Portal"; let number active_users be 250
let decimal availability_target be 99.95
let bool release_approved be false
say service_name
```

Newlines terminate statements; semicolons separate statements on the same line. Keywords and built-in type names are case insensitive. Object type names are case sensitive.

The parser recognizes typed declarations, assignment statements, output, returns, functions, if/otherwise, while, repeat, for each, literals, lists, calls, member and index access, and common symbolic and word operators. The executable backend intentionally implements a smaller subset. Syntax outside the parser subset is reported as a parse error; syntax represented by the parser but not yet implemented by the backend is rejected during compilation with a source-located diagnostic. The parser also accepts the equality phrases listed below. Classes, imports, and named call arguments are not currently supported. Strings support basic escapes; source columns count bytes.

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
build/tellc-macos-arm64/tellc/tellc TellC/examples/objects.tell -o release-policy
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

The [standalone Linux build](docs/BUILDING.md#standalone-linux) embeds LLVM,
LLD, and the static runtimes for x86-64 and ARM64. Each compiler can generate
Linux executables for both architectures.

To use an installed LLVM and Clang on Linux, build with CMake. Set `LLVM_DIR` to
the directory containing `LLVMConfig.cmake`. TellC uses `llc` from that LLVM
build and Clang on `PATH` to link. For cross-built compilers, set
`-DTELL_LLC_PATH=llc-21` (or the matching tool) to find it on the target's
`PATH`. The public parser API is `tell::parse` in `include/tell/parser.hpp`. The separate [TellC language server](../lsp/README.md) provides editor diagnostics for this dialect.

Run parser and compiler integration tests with `ctest --test-dir <build-directory> --output-on-failure`.

The integration test compiles and runs the factorial and object examples, and verifies that invalid private access, parameter types, return types, and incomplete return paths fail compilation. The LLVM-backed test job runs on Linux in GitHub Actions.

## Calling C++ libraries

Tell can call functions exported with the C ABI by a prebuilt C or C++ library.
For example, wrap the C++ function in an `extern "C"` entry point:

```cpp
#include <cstdint>

extern "C" int64_t library_double(int64_t value) {
    return value * 2;
}
```

Declare the entry point in Tell and pass the library to `tellc`:

```tell
extern cpp function twice(int64 value) returns int64 as "library_double"
say twice(21)
```

```sh
tellc program.tell --link /path/to/library.a -o program
```

Repeat `--link` for additional libraries. On Apple silicon, the embedded linker
accepts ARM64 Mach-O object files, static archives (`.a`), and dynamic libraries
(`.dylib`). The library must already be built for a compatible macOS and CPU;
tellc does not compile C++ source. Static libraries are incorporated into the
executable. Dynamic libraries remain runtime dependencies, so their install
names and any of their own dependencies must be available on the target Mac.
The linker resolves macOS system and C++ runtime symbols from libraries available
on the compiling Mac without requiring a separately installed SDK. A library
or system symbol introduced after macOS 11 can require a newer macOS release.

`extern c`, `extern cpp`, and `extern swift` all declare **C ABI** functions.
Use a C-compatible exported wrapper for C++ or Swift; C++ name mangling, classes,
methods, templates, exceptions crossing the boundary, pointers, strings,
structures, callbacks, and variadic functions are not supported. Catch C++
exceptions inside the wrapper. The optional `as` string specifies the exact
symbol name; without it, the Tell name is used.

Arguments and results can be `int32`, `int64`, `float32`, or `float64`; `void`
is also allowed as a return type. Tell `number` maps to signed integers, and
`decimal` maps to floating-point values. An `int32` argument outside its range
terminates the generated program; `float32` arguments are rounded to single
precision. Returned `int32` and `float32` values are widened to Tell types.
Tell checks argument counts and Tell types, but the declaration must match the
actual library's ABI exactly.

## Executable backend

The backend supports typed declarations, functions and methods, objects,
output and interpolation, integer arithmetic, comparisons, Boolean and
bitwise operators, control flow, resizable typed lists, and basic console
and file I/O. `number` is a signed 64-bit integer and `decimal` is a 64-bit
floating-point value. Text equality compares contents and object equality
compares identity. Unary `+` and `-` work on both numeric types. Binary
decimal arithmetic, dictionaries, tuples, and sets are unsupported.

## Objects and typed function signatures

Declare fields with `let TYPE self NAME be VALUE`. Members in `own` are private to methods of that object type. Members in `others` are public.

```tell
object ReleasePolicy:
    own:
        let text self approved_environment be "production"
    others:
        to approve_environment(text: candidate_environment) out bool:
            if candidate_environment = self.approved_environment:
                give back true
            end if
            give back false
        end function
end object

let ReleasePolicy policy be ReleasePolicy()
say policy.approve_environment("production")
say policy.approve_environment("staging")
```

This program prints `true` and then `false`. `ReleasePolicy()` constructs an instance using the field initializers. Initializers run in declaration order without a `self` receiver. Instances live until the process exits. Methods can read private fields with `self.field_name` or with the field name when a local name does not shadow it.

Parameters use `type: name` and are separated by commas. `out` declares the return type, including `out void` for functions without a result. Arguments, initializers, and return values must match the declared types. A function with a result must return a value on every path. The parser still accepts the legacy `to name with parameter:` form for parser compatibility, but the executable backend intentionally rejects untyped function signatures.

## Switch

A switch evaluates its expression once and executes the first matching case.
Cases use the same type and equality rules as `=`, and their values may be
expressions. If no case matches, execution continues after the switch.
Indent cases beneath the switch; case bodies may be inline or indented on
following lines. An `end switch` at the switch's indentation is optional.

```tell
let number water_amount be 50
switch water_amount:
    if it is 0: say "tank is empty"
    if it is 25: say "Quarter of the tank is full"
    if it is 50: say "Half the tank is full"
    if it is 75: say "3/4 of the tank is full"
    if it is 100: say "Tank is full"
```

## Lists, vectors, and for each

Lists and vectors are names for the same resizable, homogeneous collection type.
Use `list of TYPE` or `vector of TYPE` anywhere a type is accepted, including
function signatures and object fields. They may be nested, and element types may
be primitive types or objects. Literals evaluate their elements from left to right.
An empty literal needs a collection type from its declaration, argument, return,
or enclosing literal. Collection values are references and remain allocated
until the process exits; equality compares identity.

```tell
let vector of number scores be [10, 20, 30]
say scores[0]                    # 10, indices start at zero
for each score in scores:
    say score
end for
let list of text names be []
add "Ada" to names
set names[0] to "Grace"
say len(names)                   # 1
remove "Grace" from names
```

`add VALUE to COLLECTION` appends a value. `remove VALUE from COLLECTION`
removes its first equal element, or raises a catchable error when it is absent.
`len(COLLECTION)` returns the current length. Values must match the element type;
an empty nested list passed to `add` must first be assigned to a typed variable.
Aliases and function parameters refer to the same collection, so mutations are
visible through each reference. A constant binding cannot be reassigned, but its
collection can be mutated. Collection values cannot be printed directly.

Indexing outside the collection raises a catchable error. `for each` evaluates its
collection expression once, then checks its current length and reads the current
element at each index. Appending during iteration can extend the loop; removing
an element shifts later elements left, so a shifted element may be skipped. Its
iteration variable is local to the loop; `break` exits the innermost loop and
`continue` advances it. An iteration variable cannot reuse an existing local name.

## Reassignment and loops

Use `set name to expression` to reassign a local variable or parameter; types must match and constant bindings cannot be reassigned. List elements can be updated with `set values[index] to expression`, including nested indexes. Indexes are checked at runtime.

`while condition:` repeats while a Boolean condition is true. `repeat count times:` evaluates its numeric count once and runs the body that many times (zero times for a nonpositive count). End them with `end while` and `end repeat`. `break` and `continue` operate on the innermost loop. Variables declared inside a loop body are local to that body.

## Input, conversion, files, and errors

`input()` reads one line as `text`, removing its line ending. An optional text
argument prints a prompt without a newline and flushes output before reading.
Convert the text explicitly with `number(text)` for a signed 64-bit integer or
`decimal(text)` for a 64-bit floating-point value:

```tell
try:
    let text answer be input("Age: ")
    let number age be number(answer)
    say age
if something goes wrong:
    say error_message
end try
```

Conversion rejects empty input, trailing characters and out-of-range values.
It follows the C library's numeric parsing rules, including leading whitespace;
`decimal` also accepts the C library's infinity and NaN forms. End of input is an
error. `input` rejects NUL bytes. Returned text lives for the process lifetime.

`write_file(path, text)` creates or truncates a text file, and
`append_file(path, text)` creates or appends to one. `read_file(path)` returns
its contents as `text`. Paths are relative to the program's working directory.
Reads require a seekable file and reject NUL bytes; these operations are for
text, not arbitrary binary data. File errors can be caught using `try`.

`raise an error saying expression` raises a text message. A `try` block's
optional `if something goes wrong` handler receives it as `error_message`.
Errors propagate through compiled functions and methods to the nearest handler;
without one, the program writes an error to stderr and exits with status 1.
Conversions, input, file operations, list index bounds, missing list items,
and list growth failures produce catchable errors. Other allocation failures
and errors from foreign libraries are not caught. Typed catches, `finally`, and
exception objects are unsupported.

## Operators shared with the Go runtime

TellC and the Go runtime accept `=`, `==`, `equal to`, and `is equal to`
for equality. Boolean `and` / `&&` and `or` / `||` evaluate the right
operand only when needed. Use `not` or `!` for Boolean negation.

The integer bitwise operators are `~`, `&`, `|`, `^`, `<<`, and `>>`.
Operands and results must stay between −9,007,199,254,740,991 and
9,007,199,254,740,991, the shared exact integer range. Shift counts
must be integers from 0 to 63.
