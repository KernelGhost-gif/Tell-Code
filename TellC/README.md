# TellC

A C++17 compiler for an explicitly typed variant of Tell. It parses source into an AST and generates LLVM IR for native executables and just-in-time execution.

```tell
let text service_name be "Customer Portal"; let number active_users be 250
let decimal availability_target be 99.95
let bool release_approved be false
say service_name
```

Newlines terminate statements; semicolons separate statements on the same line. Keywords and built-in type names are case insensitive. Object type names are case sensitive.

The parser recognizes typed declarations, assignment statements, output, returns, functions, if/otherwise, while, repeat, for each, literals, lists, calls, member and index access, and common symbolic and word operators. The executable backend intentionally implements a smaller subset. Syntax outside the parser subset is reported as a parse error; syntax represented by the parser but not yet implemented by the backend is rejected during compilation with a source-located diagnostic. Natural-language comparison phrases, classes, imports, and named call arguments are not currently supported. Strings support basic escapes; source columns count bytes.

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

The original CMake build with an installed LLVM is also available. Set
`LLVM_DIR` to the directory containing `LLVMConfig.cmake` and
`TELL_LLC_PATH` to the matching `llc` if needed. That build uses `llc` and
Clang to compile and link programs. The public parser API is `tell::parse`
in `include/tell/parser.hpp`. The separate [TellC language server](../lsp/README.md)
provides editor diagnostics for this dialect.

Run parser and compiler integration tests with `ctest --test-dir <build-directory> --output-on-failure`.

The integration test compiles and runs the factorial and object examples, and verifies that invalid private access, parameter types, return types, and incomplete return paths fail compilation. The LLVM-backed test job runs on Linux in GitHub Actions.

## Just-in-time execution

```sh
tellc --jit TellC/examples/factorial.tell
```

`--jit` uses the same tokenizer, parser, type checks, and IR generation as executable compilation. LLVM ORC compiles that IR for the host and runs the program in the `tellc` process, returning the program's exit status. JIT mode is available in the installed-LLVM and standalone macOS builds. The static Linux build and the `-o` and `--link` options are for executable compilation.

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

The backend currently supports typed declarations, output with `{name}` interpolation, typed functions and methods, calls, returns, object construction, field access, equality, signed integer arithmetic, `if` blocks, variable reassignment, typed lists, vectors, tuples, sets, and hashmaps, indexed reads and writes, and `while`, `repeat`, and `for each` loops. `number` is a signed 64-bit integer and `decimal` is a 64-bit floating-point value. Text equality compares contents; object equality compares identity. Decimal binary arithmetic still requires code generation support.

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
collection can be mutated. Lists of printable values can be printed directly.

Indexing outside the collection raises a catchable error. `for each` evaluates its
collection expression once, then checks its current length and reads the current
element at each index. Appending during iteration can extend the loop; removing
an element shifts later elements left, so a shifted element may be skipped. Its
iteration variable is local to the loop; `break` exits the innermost loop and
`continue` advances it. An iteration variable cannot reuse an existing local name.

## Hashmaps

Use `hashmap of KEY to VALUE` in a declaration, field, or function signature. Keys can be `text`, `number`, `decimal`, or `bool`. Decimal keys treat positive and negative zero as the same key. NaN keys raise a catchable error. Every value must have the declared type, which can itself be a list, hashmap, or object. An empty literal `{}` gets its type from the surrounding declaration, argument, or return type.

```tell
let hashmap of text to number scores be {"Ada": 2, "Grace": 3}
set scores["Ada"] to 5
set scores["Lin"] to 4
say "Lin" in scores    # true
say scores["Ada"]      # 5
remove "Grace" from scores
say len(scores)        # 2

for each name in keys(scores):
    say scores[name]
end for
```

Indexed assignment inserts a new key or replaces its value. Literals evaluate pairs from left to right, and the last value for a duplicate key wins. Text keys compare by exact contents and are case-sensitive. Reading or removing an absent key raises a catchable error. Use `key in table` to test for presence and `keys(table)` to get a snapshot list in unspecified order.

Hashmaps are mutable references. Aliases, parameters, and fields share the same entries, and `=` compares hashmap identity. A constant binding can still refer to a mutable hashmap.

## Tuples, sets, slices, and destructuring

A tuple has an immutable sequence of fields, which can have different types.
Write `tuple of (TYPE, TYPE)` in a declaration or signature. Tuple literals
include `()`, `(value,)`, and `(first, second)`. Tuples support length,
membership, field-by-field equality, and indexing. A tuple with mixed field
types needs a constant index so the compiler can determine the result type.
A tuple with one field type also supports run-time indexes and iteration.

A `set of TYPE` stores unique values in insertion order. Use `{1, 2, 1}` for
a nonempty literal or `set()` with an explicit set type for an empty set.
`set(list)` takes a list and discards duplicates. Sets support `add`,
`remove`, `len`, `in`, iteration, `union`, `intersection`, and `difference`.
Set equality compares contents regardless of order.

Use `items[start:stop:step]` to copy a slice of a list or tuple. The stop is
exclusive, omitted bounds use the sequence ends, and negative bounds count
from the end. A negative step reverses the direction. Tuple slice bounds and
steps must be constant integers because they determine the result type.
A zero step is an error.

```tell
let name, score be ("Ada", 10)
set name, score to ("Grace", 12)
let list of text names be ["Ada", "Grace", "Lin"]
for each index, person in enumerate(names[::-1]):
    say (index, person)
end for
```

Destructuring accepts a tuple or list with exactly as many items as targets.
`let` infers the types of the bindings, and `set` checks the existing types
before storing any of the values. An incorrect list length is catchable.

`first`, `last`, and `reversed` accept lists and tuples. `enumerate(list)`
returns a list of `(index, item)` tuples. `map`, `filter`, and `reduce` take
a named TellC function and a list. The callback must accept the list's element
type. A filter must return `bool`. A reducer takes the accumulator first and
returns that same type, and the call supplies the initial accumulator as its
third argument.

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
Conversions, input, file operations, list index bounds, missing list items, and
list growth failures produce catchable errors. Memory allocation failures and errors from foreign libraries are not
caught. Typed catches, `finally`, and exception objects are not implemented.

## Operators shared with the Go runtime

Both runtimes accept `=`, `==`, `equal to`, and `is equal to` for equality;
`and`/`&&`, `or`/`||`, and `not`/`!` for Boolean operations; and the
integer bitwise operators `~`, `&`, `|`, `^`, `<<`, and `>>`. Boolean
operations short-circuit. `|` is bitwise OR, while `or` and `||` are Boolean
OR. For portability, bitwise operands and results must be integers between
-9007199254740991 and 9007199254740991, and shift counts must be 0–63.
This limit reflects the Go runtime's floating-point number representation.

These additions are not complete Python or Swift operator compatibility.
In particular, Python's arbitrary-size bitwise integers and Swift's custom
operators, overflow operators, optional chaining, and range operators are not
implemented by both runtimes. Other operators supported by one runtime may also
remain unavailable in the other.
