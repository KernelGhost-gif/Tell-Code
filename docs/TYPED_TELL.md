# Typed Tell on the native Go runtime

The Go runtime now has an opt-in typed source dialect. It extends the native
Go parser and runtime; it is separate from the experimental LLVM-based TellC
compiler.

## Syntax

Declare variables with their type before the name:

~~~tell
let number count be 42
let text title be "Typed Tell"
let bool enabled be true
~~~

Declare function parameters with type: name and the return type with
out type:

~~~tell
to factorial(number: n) out number:
    if n <= 1:
        give back 1
    end if
    give back n * factorial(n - 1)
end function

let number answer be factorial(5)
say answer
~~~

Use out void for a function that does not return a value. Built-in type names
are case-insensitive; custom class names are resolved against the program's
declared types. The runtime currently maps decimal to its numeric
representation and bool to boolean.

## Enable strict validation

~~~sh
tell --typed program.tell
tell check --typed program.tell
tell build --typed program.tell
~~~

Strict mode requires explicit types on let declarations and typed function
parameters and return types. It performs conservative static checks for
known expressions and enforces parameter and return types at runtime. The
compiler stores signatures in bytecode so the checks also apply to compiled
programs.

The ordinary dynamic syntax remains the default to preserve existing code.
The Go type system is an incremental typed dialect, not yet a whole-program
type system: unresolved/dynamic values are checked at runtime, collection
element types and generics are not yet supported, and class method overloads,
traits, interfaces, and typed FFI are not part of this initial slice.


A runnable native example is available at [native/examples/native-typed.tell](../native/examples/native-typed.tell).

## Performance status

The production .tbc VM now has an initial native JIT tier for a narrow subset
of typed numeric functions on Linux/amd64. Eligible straight-line functions
with number/decimal parameters and return values can be emitted as native
x86-64 floating-point instructions. The runtime caches compiled functions;
auto mode waits for a hot-call threshold before compiling. Unsupported
functions continue through the existing interpreter.

This JIT does not yet specialize the VM's general dynamic value representation
or compile control flow, recursion, collections, I/O, or dynamic calls. It is
a small native-code backend, not LLVM ORC. Division remains interpreted so the
JIT preserves Tell's division-by-zero error semantics. Do not assume a speedup
until the benchmark is run on representative workloads. See
[Execution Engines](EXECUTION_ENGINES.md) for supported instructions and
benchmark instructions.

Future work includes a typed IR, broader native code generation, register
allocation, deoptimization, additional CPU backends, and true standalone
native AOT output. The portable .tbc build remains ahead-of-time compilation
to VM bytecode, not a native executable.

## Migration

For new programs, prefer the typed form. Existing programs can migrate one
declaration and function at a time, then use --typed to make omissions and
incompatibilities explicit. The Python runtime is deprecated and should not
receive new typed-language features.
