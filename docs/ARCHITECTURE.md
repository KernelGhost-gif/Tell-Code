# Tell Runtime Architecture

## Current implementation

Both runtimes parse block headers with explicit prefix and delimiter routines, including nested otherwise-if chains. The parser now builds expression ASTs for common `Let`, `Set`, instance member assignment, output, assertion, return, raise, loop-control, bare expression, and `Tell object to method` statements. The expression AST includes tuple literals with shared runtime semantics. Other statements still use the line-oriented compatibility recognizers.

Tell is moving toward one primary native runtime. The Python implementation is deprecated and remains only as a compatibility runtime during migration. New language features should target Go; new desktop editor features should target Rust/GPUI Studio.

- `native/internal/tell` is a Go interpreter compiled into standalone binaries. Its block parser retains source line/column information and represents migrated statements with a `simpleStatement` containing a parsed expression AST. The runtime evaluates that AST directly; the bytecode compiler also lowers it directly rather than reparsing the statement text. Remaining statement families still use compatibility recognizers. Its expression lexer recognizes human-language operators directly, supports Unicode expression identifiers, and records offsets and Unicode-aware line/column coordinates. A few prefix and object-construction phrases remain syntax rewrites.
- `tell/interpreter.py` is the deprecated Python compatibility interpreter. It parses line-oriented blocks into nodes with typed dictionaries for migrated statements, and uses its own Tell lexer, Pratt parser, and expression nodes for the shared expression grammar. Python expression evaluation now uses Tell ASTs exclusively; host-only Python expression forms fail Tell parsing rather than falling through to a second expression language. Control-flow conditions are parsed into cached ASTs before execution.

Both runtimes own separate environments, built-ins, class/instance implementations, module loaders, and standard-library adapters. Local modules execute in an environment parented by the runtime globals, export public top-level bindings, and are cached by resolved file path per interpreter. `conformance/core.json` is the shared executable corpus. Each runtime test runner executes those same programs and compares output against the declared expected output.

The optional Python adapter is deliberately outside ordinary Go runtime startup. The Python runtime is deprecated and should not be used as the target for new features. The Python implementation imports requested modules in its current process. The native Go runtime invokes a configured Python executable per foreign function call and exchanges JSON values over stdin/stdout. Named and positional arguments work; opaque Python objects and persistent Python-side module state do not cross calls. This keeps the default Go runtime independent of Python while making the dependency explicit for programs that choose the adapter.

## Current execution path

```text
Tell source
  -> line/block parser
  -> block nodes and typed common-statement nodes
  -> expression AST built during parsing for typed statements
  -> AST evaluation (or compatibility statement dispatch for unmigrated forms)
  -> runtime environment and built-ins
```

Common assignment nodes now also include indexed collection assignment (`Set values[index] to value.`, `Set the item at position index in values to value.`, and `Set the key key in mapping to value.`), with the same typed node executed by both source interpreters and lowered to a verified `SET_INDEX` bytecode operation for list and dictionary writes. Import statements for local Tell modules, Python modules, JavaScript modules, and built-in libraries are also represented as typed nodes; the compiler reuses its existing embedded-module and interop instructions for those nodes. Constructor and method `Remember expression as this.field.` assignments are also typed; the source runtimes update the active instance directly, while bytecode emits the existing `LOAD this`/`SET_FIELD` sequence. `Call function with arguments.` and `Run function with arguments.` are typed call statements that evaluate a call AST and discard its result, matching the existing statement semantics in source and bytecode execution.

The source interpreter path is shown above. The native CLI also has an early compiler path for a limited statement subset:

```text
Tell source -> existing parser -> supported statement/expression compiler -> tell-bytecode-v1 JSON -> Go stack VM
```

It supports `Let`, `Set`, `Remember` field initialization, output statements, bare expressions, `If`/`Otherwise`, `While`, `Repeat`, `For each`, loop break/continue, `Try`/`If something goes wrong`, `Raise an error saying ...`, recursive embedding of local `.tell` modules, user functions, returns, recursion, named arguments, lexical closures, and the initial class model with constructors, fields, methods, single inheritance, and overrides, plus expressions accepted by the native parser. Repeated module imports share cached module exports in a run. Unsupported statements are rejected. A conservative type-annotation prepass checks expressions with statically known types. A scalar constant-folding pass simplifies safe literal expressions, and a verifier checks instruction references and stack heights across control-flow joins before execution. The compiler still lowers directly from the expression AST to stack bytecode without a distinct typed AST or optimization IR. The Go VM now supports three execution paths: direct source interpretation, AOT lowering to portable `.tbc` bytecode, and an opt-in native JIT tier for a narrow subset of typed numeric functions on Linux/amd64. The JIT caches eligible functions after first use (or after a hot threshold in auto mode); unsupported functions remain in the interpreter. This is not yet native AOT output or a whole-program optimizer.

## Execution engines

See [Execution Engines](EXECUTION_ENGINES.md) for the portable-bytecode AOT path, native JIT subset, automatic hot-function tiering, platform limits, and benchmark instructions.

## Persistent sessions

The deprecated Python `Interpreter` retains its global environment between `run` calls. The native `Session` API provides the same behavior with `NewSession` or `NewSessionWithLimits` and `Exec`. Both REPLs rely on this persistence and pass the same execution-limit settings used by command-line program runs.

## Architectural priorities

1. Continue migrating I/O, module, collection-mutation, object, and library statements from raw text recognizers into typed statement nodes; add full source spans and parser recovery.
2. Replace expression phrase rewriting with explicit tokens in both runtimes while retaining the English operator grammar.
3. Separate parser, AST, diagnostics, runtime, standard library, and CLI code into focused modules/packages.
4. Build shared semantic analysis and a stable IR, then expand bytecode lowering from the current syntax-limited subset.
5. Continue migrating the supported typed dialect into the Go parser, semantic checker, runtime, and bytecode format.
6. Keep the Python runtime in compatibility-only mode until native feature coverage and migration guidance are sufficient.
7. Build the Rust/GPUI Studio editor around the Go CLI and the existing Tell language server.

See [ENGINEERING_AUDIT.md](ENGINEERING_AUDIT.md) for measured gaps and current validation evidence.
