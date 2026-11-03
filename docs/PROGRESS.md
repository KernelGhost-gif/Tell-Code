# Tell engineering checkpoint

Updated 2026-11-03 against local `main`. v0.3.5 remains the latest published runtime. This local v0.3.6 change adds the optional Tell IDE described below; it is not yet published. Go is installed under the task workspace tools directory.

## Architecture and verified baseline

- Tell has a Python interpreter and an independent native Go runtime. Each has a hand-written lexer and Pratt expression parser. The parsers now build expression ASTs for common `Let`, `Set`, output, assertion, return, raise, loop-control, and expression statements; both runtimes execute these nodes directly, and the native bytecode compiler consumes the same Go nodes. Block and unmigrated statement parsing remain separate, line-oriented implementations with legacy recognition. Python retains a restricted host-AST compatibility path.
- Go's `tell build` emits verified stack bytecode for a subset. It does not compile all Tell programs or emit native machine code.
- The latest GitHub release is v0.3.5. Both the native runtime and tagged release GitHub Actions workflows passed.
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
- The type system remains dynamic with runtime annotations and a conservative native `--types` check. Generics, unions, enums, aliases, and whole-program static checking are absent.
- Classes provide constructors, fields, instance methods, single inheritance, and overriding. Static methods, properties, interfaces, access controls, abstract classes, traits, and multiple inheritance are absent.
- Collection comprehensions, lazy iterators/generators, immutable dictionaries/sets, and hash-based set membership are absent. Sets use deterministic insertion order and linear equality checks; tuples are immutable sequences.
- Local `.tell` imports, caching, export filtering, and cycle detection exist. The manifest is scaffolding only; package resolution and dependency installation are absent.
- The standard library is limited and not fully symmetric. Networking, broad text/regex and filesystem APIs, cryptography, and comprehensive data support are incomplete.
- The bytecode VM covers a documented subset. There is no Tell-program AOT compiler, optimizer pipeline, debugger, linter, LSP, async/concurrency model, or self-hosting compiler.
- Execution limits cover function depth and iteration counts, not memory or wall-clock use. Tell is not a sandbox.

## Source control and publishing

The v0.3.5 implementation is committed to `main` and published at https://github.com/KernelGhost-gif/Tell-Code/releases/tag/v0.3.5. Both GitHub Actions workflows passed, including the six-target release build. The v0.3.6 IDE work is in progress. Cross-compilation does not establish execution on non-Windows hosts.

## Tell IDE (v0.3.6 local work)

- Added a Tk desktop application launched with `python -m tell ide`, `tell ide`, or `tell-ide`. It has a tabbed source editor, line numbers, lexical Tell highlighting, indentation for nested blocks, file dialogs, save prompts, Find, an interactive shell, and a status bar.
- Run (F5), check (F7), bytecode build (Ctrl+B), and tests (Ctrl+T) launch the native Go runtime when available. Python runtime fallback supports run/check/test. Editor buffer can run from a temporary sibling file, preserving relative module resolution; program stdin is routed from the shell input area.
- Added quiet REPL modes to both runtimes so IDE shells can use a persistent session without terminal banners or prompts. Added IDE startup documentation and launcher metadata.
- The Windows GUI was opened locally; a Tell program produced `4`, and the persistent shell retained `answer = 40` and evaluated `answer plus 2` as `42`. These are live smoke checks, not substitutes for runtime suites. The UI requires Tkinter. The Go executable is distributed separately from the optional Python/Tk UI.
- The IDE does not include a debugger, LSP, semantic diagnostics, or formatter integration. Highlighting is lexical only. A release has not yet been created for v0.3.6.
