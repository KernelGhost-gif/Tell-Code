# Tell Core Language Specification (0.3 baseline)

This describes the portable core currently intended by the project. It is deliberately narrower than future aspirations in the project brief. Where runtime behavior has not yet been aligned, the difference is called out rather than silently defining parity.

## Source and statements

Source is UTF-8 text, one statement per physical line. Blank lines are ignored. A line beginning with `#` or `Note:` is a comment; an inline `#` outside a quoted string starts a comment. A trailing period is optional and is removed from ordinary statements. Block headers end in `:` and require matching terminators.

Supported blocks:

```ebnf
program      = { statement | block } ;
if-block     = "If" expression ":" program
               { "Otherwise if" expression ":" program }
               [ "Otherwise:" program ] "End if." ;
while-block  = "While" expression ":" program "End while." ;
repeat-block = "Repeat" expression "times:" program "End repeat." ;
for-block    = "For each" identifier { "," identifier } "in" expression ":" program "End loop." ;
function     = "To" identifier [ "(" [ names ] ")" | "with" names ] ":"
               program "End function." ;
try-block    = "Try:" program [ "If something goes wrong:" program ] "End try." ;
```

The grammar is provisional: block parsing is line-oriented, and some legacy statement recognizers still use regular expressions. Common statements use explicit prefixes and delimiters; delimiters are recognized only outside quoted strings and nested parentheses, brackets, or braces. Both runtimes now have a Tell expression lexer and Pratt parser. The Python runtime retains a restricted Python-AST fallback for previously accepted constructs not yet supported by the shared Tell grammar; those fallback constructs are not portable Tell syntax. Identifiers start with a Unicode letter or underscore and continue with Unicode letters, numbers, or underscores. Combining marks are not currently accepted in identifiers.

## Core statements

The portable baseline includes:

```tell
Let name be expression.
Set name to expression.
Say expression.
Check that expression.
Ask expression and save the answer as name.
Give back [expression].
Stop this loop.
Skip to the next repetition.
Use the math library as maths.
```

Variables may also be declared as constants or assigned a runtime type annotation:

```tell
Let constant greeting as text be "Hello".
Let visits as number be 2.
Set visits to visits plus 1.
Say greeting.
Say visits.
```

Multiple assignment binds or updates several names from a list or tuple in one operation:

```tell
Let width, height be (640, 480).
Set width, height to [800, 600].
```

The right side must evaluate to a list or tuple with exactly as many values as names. All target names and their existing constant/type constraints are validated before any target is updated. Multiple assignment currently does not accept per-name annotations.

`For each` accepts one binding for ordinary items or multiple bindings to destructure each list or tuple item. The item must contain exactly one value per binding; otherwise execution reports an arity error. This works with `enumerate(list)`:

```tell
For each index, value in enumerate(["Ada", "Grace"]):
    Say "{index}: {value}".
End loop.
```

The current runtime annotations are `any`, `nothing`, `boolean`, `number`, `text`, `list`, `tuple`, `set`, `dictionary`, `function`, `class`, and a declared Tell class name. A class annotation accepts instances of that class and its subclasses. Runtime checks always apply. The native CLI additionally offers conservative static validation of known initializer and assignment types with `tell check --types`; `tell build` applies it during compilation. Unknown expressions are deferred to runtime. This is not a whole-program static type system. A variable's existing annotation is preserved by `Set` and ordinary `Let` reassignment.

It also includes list/set mutation (`Add`, `Remove`), dictionary/list item updates, file read/write/append, class field initialization and updates, module imports, library imports, and `Raise an error saying ...`. Exact accepted phrases are implementation-sensitive until covered by the conformance corpus.

`Check that expression.` evaluates the expression using Tell truthiness and raises a source-located runtime error when it is false. The native bytecode compiler supports this assertion form too.

Both interpreters support `Try` with an optional `If something goes wrong` handler. The handler can read the text of the caught error through `error_message`. `Raise an error saying expression` raises a Tell runtime error. The native bytecode compiler supports this same basic catch behavior; typed catches, custom exception classes, `Finally`, and source stack traces are not implemented.

## Expressions

Expressions include numbers, quoted strings, booleans (`true`, `false`), `nothing`, names, lists, tuples, sets, dictionaries, grouping, function calls, indexing, slicing, member access, unary `not`/`+`/`-`, arithmetic, comparisons, membership, and boolean logic. Parentheses with a comma make a tuple: `()`, `(value,)`, or `(first, second)`; `(value)` is grouping. Lists, tuples, and Unicode text support Python-style slices, including an optional step; a zero step is an error. Tuple slices remain tuples. Dictionary literals use `{key: value}` pairs; a brace literal without colons is a set, while `{}` is an empty dictionary.

Tuple/grouping syntax is deterministic:

```ebnf
grouped-expression = "(" expression ")" ;
tuple-literal      = "(" ")"
                   | "(" expression "," [ expression { "," expression } [ "," ] ] ")" ;
set-literal        = "{" expression { "," expression } [ "," ] "}" ;
```

The portable operators include symbols and these English forms: `plus`, `minus`, `times`, `divided by`, `modulo`, `to the power of`, `is equal to`, `is not equal to`, `is greater than`, `is at least`, `is less than`, `is at most`, `is in`, `is not in`, `and`, `or`, and `not`.

Lists also support eager `map(function, list)`, `filter(function, list)`, `reduce(function, list, initial_value)`, and `enumerate(list)`. The first three take Tell functions as appropriate; `enumerate` returns index/value pairs as a list of two-element lists. `map` and `filter` return lists, and `reduce` returns its initial value for an empty list. These calls respect the configured function-call limit.

Precedence, from low to high, is `or`, `and`, comparisons/membership, addition/subtraction, multiplication/division/modulo, exponentiation, then unary operators and postfix calls/index/member access. Exponentiation is right-associative. `and` and `or` short-circuit and return booleans. Parentheses should be used where precedence could be unclear. Runtime-specific discrepancies are tracked as conformance failures, not intended semantics.

Strings support `{expression}` interpolation. If an interpolation cannot be evaluated, current runtimes may preserve the braces; this behavior is legacy and should not be used for error signaling.

## Values, scope, and functions

Scalar output is portable: booleans render as lowercase true or false, and the null value renders as nothing.

User-defined function, constructor, and method calls support named arguments in any order, including a mix of positional arguments followed by named arguments. Positional arguments cannot follow named arguments. Standard-library and built-in calls currently accept positional arguments only in the native runtime.

Function signatures support call-time default expressions and one final variadic parameter:

```tell
To greet with person and greeting defaulting to "Hello":
    Give back "{greeting}, {person}!".
End function.

To collect with first and all remaining values as rest:
    Give back [first, rest].
End function.
```

Defaults are evaluated for each call, in parameter order, in the new function scope; they can refer to earlier parameters and captured variables. Required parameters must precede defaulted parameters. `all remaining values as name` must be the final parameter and receives a list, including an empty list when no extra values are passed. The variadic parameter cannot be supplied by name.

Values are dynamically typed, with optional runtime annotations and immutable constants as described in [Variables and Types](VARIABLES_AND_TYPES.md). The common values are numbers, strings, booleans, nothing/null, lists, dictionaries, functions, classes, and instances. Functions capture their defining environment and return with `Give back`; absent a return they yield nothing. Positional, named, defaulted, and final variadic parameters are supported by both runtimes and the native bytecode VM.

## Classes (implemented subset)

Classes support constructors, instance fields, instance methods, single inheritance, and method overriding:

```tell
Create a class called Animal:
    When creating an Animal with name:
        Remember name as this.name.
    End constructor.
    To speak:
        Say "{this.name} makes a sound.".
    End function.
End class.

Create a class called Dog based on Animal:
    To speak:
        Say "Woof, {this.name}!".
    End function.
End class.

Let pet be a new Dog with "Rex".
Tell pet to speak.
```

Instances are distinct runtime objects, not dictionaries. A derived class inherits its base constructor unless it declares its own; declaring a constructor replaces the inherited constructor without automatically invoking it. An overridden method replaces the base method and other methods remain inherited. Fields are dynamically added by `Remember ... as this.field` inside a constructor or method, and public fields can be reassigned with `Set instance.field to expression.` Static/class methods, access modifiers, abstract classes, multiple inheritance, properties, and `super` calls are not supported yet.

Variables are assigned into the current environment. Function calls create a local environment whose parent is the captured environment. Loop bindings are assigned into the current environment in the current implementation.

To limit runaway work, both runtimes default to a 64-level active function-call limit and a one-million-iteration limit per `while`, `repeat`, `for each`, or generated `range`. Both the Python API and CLIs let callers lower or raise these limits with `max_call_depth` / `--max-call-depth` and `max_loop_iterations` / `--max-loop-iterations`. Limits must be positive integers. These controls do not bound memory use, elapsed time in a single operation, or effects such as file and network access; execution is not sandboxed.

## Standard library and effects

The runtimes provide overlapping built-ins for common collection, numeric, random, and type operations, including the shared `map`, `filter`, and `reduce` functions. Allowlisted libraries include `math`, `random`, `statistics`, `json`, `datetime`, and `csv` to varying degrees. Both runtimes implement `csv.parse(text)` and `csv.stringify(rows)` with text-only cells and LF output; see the [standard library reference](STANDARD_LIBRARY.md) for details. Exact names/signatures of other exports must be checked against runtime documentation and parity tests. File operations use paths relative to the process working directory.

Local modules use `Use the module "relative/path.tell" as alias.`. The path is resolved relative to the importing source file; an in-memory program resolves from the process working directory. A module executes once per interpreter, receives built-ins through its parent scope, and exports its public top-level bindings. Names beginning with `_` are not exported. Circular imports and non-`.tell` paths are rejected. This is a local source-file mechanism, not a package manager.

Tell execution is not sandboxed. A program can perform file I/O and consume CPU/memory; do not execute untrusted programs as if they were isolated.

## Errors and compatibility

Runtime errors include the source filename, line, and starting column of the statement that failed. Expression parser errors also include a line and column within the expression, though the two runtimes do not yet share structured diagnostic types or stable error codes. Source excerpts and stack traces are not specified. The shared conformance corpus checks expected output and selected error fragments; it does not prove complete runtime parity.

Package management, async/concurrency, a static type system, AOT/JIT compilation, macros, FFI, and self-hosting are future design goals and are not part of this 0.2 language specification. The class features above are an initial subset of the broader object system planned for Tell.

## Native bytecode subset

The Go CLI can compile a deliberately smaller subset with `tell build`. Supported statements are `Let`, `Set`, `Remember ... as this.field`, output statements, bare expressions, `If`/`Otherwise`, `While`, `Repeat`, `For each`, loop break/continue, `Try`/`If something goes wrong`, `Raise an error saying ...`, local module imports, function declarations, `Give back`, and the class subset described above. Local `.tell` module dependencies are compiled and embedded recursively; repeated imports share one module instance, and public top-level bindings are exported. Compiled functions support recursion, named and default arguments, a final variadic parameter, and lexical closures; compiled classes support constructors, fields, instance methods, single inheritance, and overriding. The VM supports tuple and set literals, set iteration and set algebra through the shared built-ins. A raised runtime error transfers to the nearest `If something goes wrong` handler; error types and `Finally` are not implemented in bytecode. Expressions use the native expression grammar described above. Unsupported syntax is a compile-time error; it is not copied into the artifact. The emitted `tell-bytecode-v1` artifact is JSON-encoded stack instructions and executes only in the native Go VM. This compiler subset is a tooling extension, not a claim that every construct in this specification compiles.
