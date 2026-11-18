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

This first Go typed-dialect increment adds validation and runtime contract
checks; it does not yet specialize the VM's dynamic value representation.
Go execution still uses the existing general-purpose runtime and bytecode
operations. Do not treat this change as a measured speedup in execution,
memory use, or compilation time.

The next performance phase should use typed semantic information to lower
verified functions to specialized bytecode operations and compact value
representations where benchmarks justify them. Any such change should retain
a dynamic compatibility path and be measured against representative Tell
programs before performance claims are made.

## Migration

For new programs, prefer the typed form. Existing programs can migrate one
declaration and function at a time, then use --typed to make omissions and
incompatibilities explicit. The Python runtime is deprecated and should not
receive new typed-language features.
