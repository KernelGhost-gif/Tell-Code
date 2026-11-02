# Tell engineering checkpoint

Updated 2026-11-02 against local `main`, based on commit `82a2996` (`v0.3.1`). The working tree contains the local v0.3.3 scaffolder/collections work and the v0.3.4 language changes. The last initial process inspection found no Go, Python, or Tell processes. Go is installed under the task workspace tools directory.

## Architecture and verified baseline

- Tell has a Python interpreter and an independent native Go runtime. Each has a hand-written lexer and Pratt expression parser, but statement parsing and execution remain separate, line-oriented implementations with residual regex recognition. Python retains a restricted host-AST compatibility path.
- Go's `tell build` emits verified stack bytecode for a subset. It does not compile all Tell programs or emit native machine code.
- The most recent GitHub-published release is v0.3.1. Local release binaries are generated artifacts and have not been published.
- The current shared conformance file has 45 cases, executed by both runtime suites.

## Implemented in the current local change set

- Both runtimes and bytecode support eager `map`, `filter`, `reduce`, and `enumerate`; callback calls enforce the normal function-depth limit.
- `For each` can bind multiple identifiers from each list item, including directly destructuring `enumerate` pairs; mismatched pair lengths are runtime errors in both interpreters and the bytecode VM.
- Multiple assignment binds or updates names from a list atomically after validating arity, duplicate names, constants, and type annotations.
- Function, method, and constructor parameters support named binding, call-time default expressions, and a single final variadic parameter (`all remaining values as rest`). Defaults evaluate per call in order and may refer to earlier parameters or closure values. The variadic value is always a list and cannot be passed by name.
- The native bytecode compiler and VM support these function signatures, including constructor defaults and variadic calls.
- The Go CLI's `tell new` and `tell init` scaffold runnable project files, tests, and a minimal `tell.toml`, with path validation and overwrite protection. They do not resolve or install dependencies.
- `docs/COLLECTIONS.md` and the language specification document the implemented contracts; the README and CLI reference describe available behavior.

## Checks run during this checkpoint

- Python: `python -m unittest discover -s tests -v` passes all 33 unittest methods, including the shared corpus.
- Native: `go test ./...` passes both Go packages; `go vet ./...` passes. The Go commands must run from `native/`.
- The corpus covers defaults, call-time reevaluation, constructor defaults, variadic calls and rejection cases, and enumeration in both interpreters. Native bytecode tests cover default functions, default constructors, and variadic functions.
- The final v0.3.4 build produced all six configured targets in isolated staging and verified their SHA-256 sums before updating `releases/`. Windows amd64 ran the same defaults/variadics/enumeration/destructured-loop program directly and from bytecode with matching output. The other five binaries were cross-compiled only.
- `go test -race` is unavailable on this host because `gcc`, `clang`, and `cl` are not present on PATH.

## Incomplete and known limitations

- No unified source-spanned AST or semantic-analysis pipeline exists; parser implementations are duplicated and statement parsing is still line-based.
- The type system remains dynamic with runtime annotations and a conservative native `--types` check. Generics, unions, enums, aliases, and whole-program static checking are absent.
- Classes provide constructors, fields, instance methods, single inheritance, and overriding. Static methods, properties, interfaces, access controls, abstract classes, traits, and multiple inheritance are absent.
- Collection comprehensions, lazy iterators/generators, tuples/sets as a shared runtime contract, and immutable collections are absent.
- Local `.tell` imports, caching, export filtering, and cycle detection exist. The manifest is scaffolding only; package resolution and dependency installation are absent.
- The standard library is limited and not fully symmetric. Networking, broad text/regex and filesystem APIs, cryptography, and comprehensive data support are incomplete.
- The bytecode VM covers a documented subset. There is no Tell-program AOT compiler, optimizer pipeline, debugger, linter, LSP, async/concurrency model, or self-hosting compiler.
- Execution limits cover function depth and iteration counts, not memory or wall-clock use. Tell is not a sandbox.

## Source control and publishing

All changes in this checkpoint remain local and uncommitted. The latest execution directive prohibits pushing or creating a GitHub release. The remote public release therefore remains v0.3.1; local binaries are not published.
