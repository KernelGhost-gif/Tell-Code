# Tell engineering checkpoint

Updated 2026-11-03 against `main`. v0.3.6 adds the optional Tell IDE described below and is published with all six native targets. Both GitHub workflows passed. Go is installed under the task workspace tools directory.

## Architecture and verified baseline

- Tell has a Python interpreter and an independent native Go runtime. Each has a hand-written lexer and Pratt expression parser. The parsers now build expression ASTs for common `Let`, `Set`, output, assertion, return, raise, loop-control, and expression statements; both runtimes execute these nodes directly, and the native bytecode compiler consumes the same Go nodes. Block and unmigrated statement parsing remain separate, line-oriented implementations with legacy recognition. Python no longer falls back to the host AST; unsupported host-only expressions are rejected by the Tell parser.
- Go's `tell build` emits verified stack bytecode for a subset. It does not compile all Tell programs or emit native machine code.
- The latest GitHub release is v0.3.6. Both the native runtime and tagged release GitHub Actions workflows passed.
- The shared conformance corpus is executed by both runtime suites; set conformance cases were added in this checkpoint.

## Implemented in the current local change set

- Both runtimes and bytecode support eager `map`, `filter`, `reduce`, and `enumerate`; callback calls enforce the normal function-depth limit.
- `For each` can bind multiple identifiers from each list item, including directly destructuring `enumerate` pairs; mismatched pair lengths are runtime errors in both interpreters and the bytecode VM.
- Multiple assignment binds or updates names from a list atomically after validating arity, duplicate names, constants, and type annotations.
- Function, method, and constructor parameters support named binding, call-time default expressions, and a single final variadic parameter (`all remaining values as rest`). Defaults evaluate per call in order and may refer to earlier parameters or closure values. The variadic value is always a list and cannot be passed by name.
- The native bytecode compiler and VM support these function signatures, including constructor defaults and variadic calls.
- Recent parser work moves common assignments, instance member assignments, output/assertion, return/raise, loop-control, bare expression statements, and `Tell object to method` calls to pre-parsed AST nodes in both runtimes. The Go bytecode compiler lowers the typed statements directly, including `SET_MEMBER`.
- Both expression Pratt parsers now distinguish grouping from empty, singleton, and multi-value tuple literals. Both runtimes implement immutable tuple values, canonical display, `tuple` type inspection/annotation, length, indexing, slicing, membership, concatenation, iteration, and destructuring. The Go bytecode compiler emits `TUPLE` and the VM preserves tuple identity through indexing, slicing, loops, and multiple assignment.
- Both Pratt parsers distinguish set literals from dictionaries (`{}` remains an empty dictionary; brace literals without key/value colons are sets). Both runtimes implement insertion-ordered, duplicate-free sets with constructors, type inspection/annotations, length, membership, iteration, equality, `Add`/`Remove`, and `union`/`intersection`/`difference`. Shared conformance tests cover those semantics. The native bytecode compiler emits a verified `SET` instruction and supports set literals, membership, iteration, algebra built-ins, and `Add`/`Remove` mutation of lists and sets.
- `Use the Python library called module.name as alias.` imports Python modules in the Python runtime and exposes public module functions through an optional JSON bridge in the native Go runtime and bytecode VM. Positional/named arguments and JSON-compatible return values work; the native adapter starts Python only when a foreign function is called. Python module state, callbacks, opaque objects, and non-JSON return values are unsupported. See `docs/INTEROPERABILITY.md`.
- The Go CLI's `tell new` and `tell init` scaffold runnable project files, tests, and a minimal `tell.toml`, with path validation and overwrite protection. They do not resolve or install dependencies.
- `docs/COLLECTIONS.md` and the language specification document the implemented contracts; the README and CLI reference describe available behavior.

## Checks run during this checkpoint

- Python: `python -m unittest discover -s tests -v` passes all 37 unittest methods, including the shared corpus and Python-module call.
- Native: `go test -count=1 ./...` and `go vet ./...` pass both Go packages. Native source and bytecode integration tests call Python's `statistics` module when Python is installed; they skip when no Python executable is available.
- The shared corpus covers tuple and set literals, constructors, annotations, display, equality, membership, set algebra, mutation and iteration, along with earlier tuple indexing, slicing, multiple assignment and destructuring. Native bytecode tests cover tuple and set values, set membership, iteration and algebra.
- The current source cross-builds for Linux amd64/arm64, macOS amd64/arm64, and Windows amd64/arm64. A temporary Windows AMD64 build ran `examples/sets.tell`; a focused set program produced identical output in source and bytecode modes. Cross-builds establish compilation only; the other five targets were not run on their target operating systems.
- Release builds produce an SPDX 2.3 SBOM alongside the six native binaries and SHA-256 checksums. The release workflow uploads the SBOM.
- `go test -race` is unavailable on this host because `gcc`, `clang`, and `cl` are not present on PATH.

## Incomplete and known limitations

- A unified source-spanned AST and shared semantic-analysis pipeline still do not exist; the migrated AST subset is incremental and statement parsing remains line-based.
- The type system remains dynamic with runtime annotations and conservative `--types` checks in both CLIs. Generics, unions, enums, aliases, and whole-program static checking are absent.
- Classes provide constructors, fields, instance methods, single inheritance, and overriding. Static methods, properties, interfaces, access controls, abstract classes, traits, and multiple inheritance are absent.
- Collection comprehensions, lazy iterators/generators, immutable dictionaries/sets, and hash-based set membership are absent. Sets use deterministic insertion order and linear equality checks; tuples are immutable sequences.
- Local `.tell` imports, caching, export filtering, and cycle detection exist. The manifest is scaffolding only; package resolution and dependency installation are absent.
- The standard library is limited and not fully symmetric. Networking, broad text/regex and filesystem APIs, cryptography, and comprehensive data support are incomplete.
- The bytecode VM covers a documented subset. There is no Tell-program AOT compiler, optimizer pipeline, debugger, linter, LSP, async/concurrency model, or self-hosting compiler.
- Execution limits cover function depth and iteration counts, not memory or wall-clock use. Tell is not a sandbox.

## Source control and publishing

The v0.3.6 implementation is committed to `main` and published at https://github.com/KernelGhost-gif/Tell-Code/releases/tag/v0.3.6. Both GitHub Actions workflows passed, including the six-target release build. Cross-compilation does not establish execution on non-Windows hosts.

## Tell IDE (v0.3.6 local work)

- Added a Tk desktop application launched with `python -m tell ide`, `tell ide`, or `tell-ide`. It has a tabbed source editor, line numbers, lexical Tell highlighting, indentation for nested blocks, file dialogs, save prompts, Find, an interactive shell, and a status bar.
- Run (F5), check (F7), bytecode build (Ctrl+B), and tests (Ctrl+T) launch the native Go runtime when available. Python runtime fallback supports run/check/test. Editor buffer can run from a temporary sibling file, preserving relative module resolution; program stdin is routed from the shell input area.
- Added quiet REPL modes to both runtimes so IDE shells can use a persistent session without terminal banners or prompts. Added IDE startup documentation and launcher metadata.
- The Windows GUI was opened locally; a Tell program produced `4`, and the persistent shell retained `answer = 40` and evaluated `answer plus 2` as `42`. These are live smoke checks, not substitutes for runtime suites. The UI requires Tkinter. The Go executable is distributed separately from the optional Python/Tk UI.
- The IDE does not include a debugger, LSP, semantic diagnostics, or formatter integration. Highlighting is lexical only. v0.3.6 is published; the Python/Tk desktop UI is distributed in the source package rather than embedded in native binaries.

## v0.3.7 polyglot expansion

- Added optional JavaScript/Node.js interoperability to both the Python interpreter and native Go runtime. Tell can import Node built-ins, installed npm packages, scoped packages, package subpaths, and relative JavaScript modules, then invoke public exported functions.
- Added matching bytecode support, including validation and execution of JavaScript imports in compiled Tell programs.
- Foreign calls use JSON-compatible values, support asynchronous JavaScript exports, and enforce a 30-second process timeout. Tell named arguments are passed as one final JavaScript options object.
- Added native interpreter, bytecode, and Python interpreter tests plus a runnable Node.js example. CI and release workflows install Node.js so the bridge is exercised.
- This is a process-based interoperability layer, not an embedded runtime or sandbox. Python and Node.js must be installed for their respective bridges; state, callbacks, opaque objects, and non-JSON values do not cross the boundary.
- C/C++, Rust, JVM, .NET, browser/WASM bridges, a general FFI, package manager, and full native compiler pipeline remain future work.


## 2026-11-12 modernization branch checkpoint
- Continued on `modernization/0.4-diagnostic-conformance`: normalized Python missing-name/type-mismatch messages to the native wording; added fresh-process fixtures for SyntaxError (64), NameError (70), and TypeError (70). Native parsing now rejects malformed expression-led statements before execution, marks parse failures as `SyntaxError`, and the CLI honors that kind. Python and Go now emit matching double-quoted invalid tokens and token-level columns. Added parser/CLI/diagnostic unit tests.
- Continued on `modernization/0.4-process-conformance`: expanded the fresh-process `runtime-v1.json` suite beyond numeric/error edges to cover stdin prompts, file write/append/read, relative local-module imports, list mutation and indexed assignment, `Call`/`Run`/natural-language `Tell` calls, and built-in library imports. Each checks exact stdout, stderr, and exit status in both production CLIs.
- Continued on `modernization/0.4-call-run-ast`: `Call` and `Run` function statements are now represented as typed call-expression nodes in both interpreters; bytecode lowers them to CALL followed by POP so results are discarded like the statement runtime. Added source/bytecode tests and a shared conformance case.
- Continued on `modernization/0.4-remember-ast`: `Remember expression as this.field.` is parsed and executed as a typed statement in Python and Go, compiled through `SET_FIELD`, and covered by constructor/method source tests, bytecode tests, and the shared cross-runtime corpus.

The following work is on `modernization/0.4-foundation`; it has not been merged to `main` or published as a release.

- Removed the Python `ast.parse` expression fallback. The Python runtime now executes only Tell expression ASTs; conditional expressions are supported in both production Pratt parsers.
- Both production parsers validate control-flow expressions at parse time and retain parsed ASTs for `If`, `While`, `Repeat`, and `For each`, avoiding repeated runtime parsing.
- Expression ASTs retain token line/column spans for operators, calls, member access, and indexing. Runtime diagnostics preserve these spans for division-by-zero, null member access, recursion limits, and control-flow conditions.
- Added stable CLI diagnostics and exit-code adapters: syntax/usage 64, type/name/runtime 70, I/O 74. The Python CLI now exposes the conservative `--types` analyzer, matching the native CLI's static-check entry point for statically knowable types.
- Split the production Go runtime implementation into parser, runtime, execution, statement, module, and lexer files while retaining the existing same-package API. Added explicit runtime call-frame metadata (function, caller/local lexical environments, and call-site coordinates) with unwind tests.
- Added `docs/GRAMMAR_V1.ebnf`, specified numeric coercion and IEEE-754 behavior in `docs/CONFORMANCE_SEMANTICS_V1.md`, and expanded numeric and conditional-expression cases in the shared core corpus.
- Added exact source-span tests for runtime expression errors and condition expressions. Updated the process-level `runtime-v1.json` expectations to the token-level diagnostic positions and normalized portable error messages.
- Moved `Ask`, `Read the file`, `Write`, and `Append` into typed statement AST nodes in both production runtimes. Both parser suites assert the AST kinds, and runtime tests exercise prompt handling plus file read/write/append behavior. File and input failures carry an explicit `IOError` kind, source location, and process status 74.
- Moved `Add` and `Remove` collection mutations into typed AST nodes in both interpreters, connected them to the existing native bytecode `ADD_ITEM`/`REMOVE_ITEM` instructions, and added shared list/set mutation coverage to the cross-runtime corpus.
- Continued on the post-merge `modernization/0.4-import-ast` branch: local Tell, Python, JavaScript, and built-in library imports are typed nodes in both runtimes; native bytecode lowers them to existing import instructions. Indexed list/dictionary assignment—including Tell's natural-language list-position and dictionary-key forms—is typed in both runtimes and compiles through `SET_INDEX`. Added source/parser/bytecode tests and shared conformance cases for indexed assignment.
- Extended `runtime-v1.json` with a missing-file case that checks byte-exact stderr and exit status, and corrected the process harness to place runtime options before the positional source filename so native Go flag parsing receives them.
- CI now runs both the smoke suite and the full process-level runtime-v1 suite against Python and native binaries. The complete latest validation result is tracked in the modernization GitHub Actions run; do not infer release readiness from branch-local success alone.
- Remaining migration work: move the remaining regex/line-based I/O, module, collection-mutation, object, and standard-library statements into typed statement nodes; expand process-level fixtures beyond the current seven runtime-v1 cases; complete the shared semantic pipeline and broader bytecode lowering. The production Go and Python runtimes still have separate statement/block parsers.


## 2026-11-14 follow-on parity hardening (PR #9)

- The native Go static analyzer now consumes the production typed statement/expression nodes and infers known builtin return types, class-constructor result types, and class inheritance. It rejects statically provable primitive, builtin-call, numeric-operator, and unrelated-class mismatches while continuing to defer dynamic values.
- The Python analyzer's class/type diagnostic spelling is normalized to the same stable quoted names and message shape used by the native CLI.
- Both parsers now accept the optional `, then` suffix on `If` and `Otherwise if` headers. The Go parser also validates malformed `Let` and `Set` right-hand expressions during parsing instead of deferring those failures to execution.
- The process-conformance harness can invoke `--types` per fixture. The shared contract now covers class mismatch/subtyping, builtin return inference, known operator mismatch, optional `, then`, and malformed declaration syntax, alongside exact process output and exit codes.
- These are targeted parity improvements. The production statement/block parsers remain separate and line-oriented; remaining host/runtime and standard-library differences still require further shared fixtures and staged migrations. Validation is delegated to the branch's Linux/Windows CI; do not mark the milestone verified until those runs complete.
