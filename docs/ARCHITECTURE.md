# Tell Runtime Architecture

## Current implementation

Both runtimes parse block headers with explicit prefix and delimiter routines, including nested otherwise-if chains. The parser now builds expression ASTs for common `Let`, `Set`, output, assertion, return, raise, loop-control, bare expression, and `Tell object to method` statements. Other statements still use the line-oriented compatibility recognizers.

Tell has two independent runtimes:

- `native/internal/tell` is a Go interpreter compiled into standalone binaries. Its block parser retains source line/column information and represents migrated statements with a `simpleStatement` containing a parsed expression AST. The runtime evaluates that AST directly; the bytecode compiler also lowers it directly rather than reparsing the statement text. Remaining statement families still use compatibility recognizers. Its expression lexer recognizes human-language operators directly, supports Unicode expression identifiers, and records offsets and Unicode-aware line/column coordinates. A few prefix and object-construction phrases remain syntax rewrites.
- `tell/interpreter.py` parses line-oriented blocks into nodes with typed dictionaries for migrated statements, and uses its own Tell lexer, Pratt parser, and expression nodes for the shared expression grammar. A restricted Python-AST fallback remains for legacy Python-compatible forms, without using Python `eval`.

Both runtimes own separate environments, built-ins, class/instance implementations, module loaders, and standard-library adapters. Local modules execute in an environment parented by the runtime globals, export public top-level bindings, and are cached by resolved file path per interpreter. `conformance/core.json` is the shared executable corpus. Each runtime test runner executes those same programs and compares output against the declared expected output.

## Current execution path

```text
Tell source
  -> line/block parser
  -> block nodes and typed common-statement nodes
  -> expression AST built during parsing for typed statements
  -> AST evaluation (or compatibility statement dispatch for unmigrated forms)
  -> runtime environment and built-ins
```

The source interpreter path is shown above. The native CLI also has an early compiler path for a limited statement subset:

```text
Tell source -> existing parser -> supported statement/expression compiler -> tell-bytecode-v1 JSON -> Go stack VM
```

It supports `Let`, `Set`, `Remember` field initialization, output statements, bare expressions, `If`/`Otherwise`, `While`, `Repeat`, `For each`, loop break/continue, `Try`/`If something goes wrong`, `Raise an error saying ...`, recursive embedding of local `.tell` modules, user functions, returns, recursion, named arguments, lexical closures, and the initial class model with constructors, fields, methods, single inheritance, and overrides, plus expressions accepted by the native parser. Repeated module imports share cached module exports in a run. Unsupported statements are rejected. A conservative type-annotation prepass checks expressions with statically known types. A scalar constant-folding pass simplifies safe literal expressions, and a verifier checks instruction references and stack heights across control-flow joins before execution. The compiler still lowers directly from the expression AST to stack bytecode without a distinct typed AST or optimization IR; it has no whole-program type inference, broader optimizer, JIT, or AOT compilation of Tell programs. The Go executable itself is a native-compiled interpreter.

## Persistent sessions

The Python `Interpreter` retains its global environment between `run` calls. The native `Session` API provides the same behavior with `NewSession` or `NewSessionWithLimits` and `Exec`. Both REPLs rely on this persistence and pass the same execution-limit settings used by command-line program runs.

## Architectural priorities

1. Continue migrating I/O, module, collection-mutation, object, and library statements from raw text recognizers into typed statement nodes; add full source spans and parser recovery.
2. Replace expression phrase rewriting with explicit tokens in both runtimes while retaining the English operator grammar.
3. Separate parser, AST, diagnostics, runtime, standard library, and CLI code into focused modules/packages.
4. Build shared semantic analysis and a stable IR, then expand bytecode lowering from the current syntax-limited subset.
5. Align numeric, collection, error, and library behavior with cross-runtime conformance cases as each statement family moves to the AST.

See [ENGINEERING_AUDIT.md](ENGINEERING_AUDIT.md) for measured gaps and current validation evidence.
