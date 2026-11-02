# Tell engineering checkpoint

Updated 2026-11-02 against local `main` at `dd5d01b` (`Migrate common Tell statements to AST nodes`). The current member-assignment AST changes are uncommitted on top of that commit. Go is installed under the task workspace tools directory.

## Architecture and verified baseline

- Tell has a Python interpreter and an independent native Go runtime. Each has a hand-written lexer and Pratt expression parser. The parsers now build expression ASTs for common `Let`, `Set`, output, assertion, return, raise, loop-control, and expression statements; both runtimes execute these nodes directly, and the native bytecode compiler consumes the same Go nodes. Block and unmigrated statement parsing remain separate, line-oriented implementations with legacy recognition. Python retains a restricted host-AST compatibility path.
- Go's `tell build` emits verified stack bytecode for a subset. It does not compile all Tell programs or emit native machine code.
- The most recent GitHub-published release is v0.3.1. Local release binaries are generated artifacts and have not been published.
- The current shared conformance file has 47 cases, executed by both runtime suites.

## Implemented in the current local change set

- Both runtimes and bytecode support eager `map`, `filter`, `reduce`, and `enumerate`; callback calls enforce the normal function-depth limit.
- `For each` can bind multiple identifiers from each list item, including directly destructuring `enumerate` pairs; mismatched pair lengths are runtime errors in both interpreters and the bytecode VM.
- Multiple assignment binds or updates names from a list atomically after validating arity, duplicate names, constants, and type annotations.
- Function, method, and constructor parameters support named binding, call-time default expressions, and a single final variadic parameter (`all remaining values as rest`). Defaults evaluate per call in order and may refer to earlier parameters or closure values. The variadic value is always a list and cannot be passed by name.
- The native bytecode compiler and VM support these function signatures, including constructor defaults and variadic calls.
- Current uncommitted parser work moves common assignments, instance member assignments, output/assertion, return/raise, loop-control, bare expression statements, and `Tell object to method` calls to pre-parsed AST nodes in both runtimes. Calls use the same member-access and call-expression evaluator as ordinary function calls; the Go bytecode compiler lowers the typed statements directly, including `SET_MEMBER`. Unmigrated statement forms keep their prior compatibility route.
- The Go CLI's `tell new` and `tell init` scaffold runnable project files, tests, and a minimal `tell.toml`, with path validation and overwrite protection. They do not resolve or install dependencies.
- `docs/COLLECTIONS.md` and the language specification document the implemented contracts; the README and CLI reference describe available behavior.

## Checks run during this checkpoint

- Python: `python -m unittest discover -s tests -v` passes all 34 unittest methods, including the shared corpus.
- Native: `go test ./...` passes both Go packages; `go vet ./...` passes. The Go commands must run from `native/`.
- The corpus covers defaults, call-time reevaluation, constructor defaults, variadic calls and rejection cases, enumeration, a sentence-form instance method call, and parsed instance property assignment in both interpreters. Native bytecode tests cover default functions, default constructors, variadic functions, sentence-form method calls, and AST-lowered property assignment.
- All six v0.3.4 binaries were rebuilt from the current source after successful staging: Linux amd64/arm64, macOS amd64/arm64, and Windows amd64/arm64. The staged Windows amd64 executable ran `examples/compiled-objects.tell` as source and as `.tbc`; both produced the expected two lines. Other target binaries were cross-compiled, not run on their target operating systems. `releases/SHA256SUMS` was regenerated and verified locally after the member-assignment AST migration.
- `go test -race` is unavailable on this host because `gcc`, `clang`, and `cl` are not present on PATH.

## Incomplete and known limitations

- A unified source-spanned AST and shared semantic-analysis pipeline still do not exist; the migrated AST subset is incremental and statement parsing remains line-based.
- The type system remains dynamic with runtime annotations and a conservative native `--types` check. Generics, unions, enums, aliases, and whole-program static checking are absent.
- Classes provide constructors, fields, instance methods, single inheritance, and overriding. Static methods, properties, interfaces, access controls, abstract classes, traits, and multiple inheritance are absent.
- Collection comprehensions, lazy iterators/generators, tuples/sets as a shared runtime contract, and immutable collections are absent.
- Local `.tell` imports, caching, export filtering, and cycle detection exist. The manifest is scaffolding only; package resolution and dependency installation are absent.
- The standard library is limited and not fully symmetric. Networking, broad text/regex and filesystem APIs, cryptography, and comprehensive data support are incomplete.
- The bytecode VM covers a documented subset. There is no Tell-program AOT compiler, optimizer pipeline, debugger, linter, LSP, async/concurrency model, or self-hosting compiler.
- Execution limits cover function depth and iteration counts, not memory or wall-clock use. Tell is not a sandbox.

## Source control and publishing

The statement-AST and member-assignment migrations are maintained on local `main`. Nothing has been pushed or published. The remote public release remains v0.3.1; the rebuilt local binaries are not published.
