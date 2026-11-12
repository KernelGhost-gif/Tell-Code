# Tell 0.4 Runtime Modernization

## Scope and migration rule

This change introduces a modular expression pipeline beside the 0.3 statement runtime. It does not silently replace the production interpreter. The expression vertical slice is executable and independently testable; full migration requires moving statement parsing, evaluation, classes, modules, I/O, REPL sessions, and bytecode support in successive compatibility-preserving steps.

## Target package graph

    source
      └── lexer
            └── parser ───────────────┐
                 └── ast              │
                      ├── semantic    │
                      ├── types       │
                      ├── optimizer   │
                      └── compiler ── bytecode ── vm
                                      │             │
                                      └─────────────┴── pipeline

Dependency direction is one-way. ast depends only on source spans. lexer depends on source spans. parser depends on lexer and ast. semantic, types, and optimizer depend on ast. compiler depends on ast and bytecode. vm depends on bytecode. pipeline is the composition root. No runtime package imports a CLI package.

The scanner returns tokens containing byte offsets and 1-based line/column positions. Token lexemes are string views into the source buffer, so scanning does not allocate token text. AST construction is intentionally allocating. Bytecode stores an opcode stream, a typed constant pool, and source-line entries. The VM checks context cancellation between instructions and enforces a stack ceiling.

## Core interfaces

- lexer.Scanner: Next() Token
- parser.Parser: Parse() (ast.Expr, error)
- compiler.Compiler: Compile(context.Context, ast.Expr) (*bytecode.Chunk, error)
- vm.VM: Execute(context.Context, *bytecode.Chunk, filename string, globals map[string]vm.Value) (vm.Value, error)

The pipeline example is implemented in native/internal/pipeline/pipeline.go. It parses, resolves symbols, infers expression types, folds safe constants, compiles, optimizes bytecode, and executes. A caller can use pipeline.Run(context.Background(), "2 + 3 * 4", "example.tell", nil) to run the source string through the complete expression vertical slice and inspect Result.Value. The current bytecode instruction set is intentionally limited to scalar expression evaluation; it is not yet a replacement for the established statement VM. The expression pipeline runs AST constant folding and then bytecode jump threading plus reachability-based dead-code elimination. The bytecode pass re-verifies operands and targets, relocates branch offsets, and rebuilds source-line mappings.

## Runtime migration sequence

### Phase A — structural foundation
1. Adopt source.Position and source.Span as the canonical source coordinate model.
2. Move expression tokenization and parsing to lexer and parser.
3. Keep AST nodes free of interpreter/runtime dependencies.
4. Add semantic scopes and a type inference interface.
5. Add optimizer passes behind an ordered pass manager.
6. Keep bytecode serialization versioned; reject unknown versions and invalid operands.

### Phase B — statement grammar and semantic parity
1. Add statement and block nodes to ast, then migrate the Go line-oriented block parser.
2. Resolve declarations in lexical scopes, including function, class, module, and loop scopes.
3. Move the Python parser to the same token/AST grammar. Python may be a host runtime, not a second language definition.
4. Expand the shared conformance suite before changing semantics.
5. Remove Python's restricted Python-AST fallback only after every supported legacy construct either has a Tell grammar production or is explicitly removed in a versioned language change.

### Phase C — execution and compiler parity
1. Introduce common runtime value semantics, especially numeric conversion, truthiness, equality, collection order, slicing, and null behavior.
2. Move function calls and closures to explicit call frames with lexical environments.
3. Add verifier checks for stack height, constant indices, jump targets, and legal instruction boundaries.
4. Add jump patching, source maps, stack traces, and a bytecode disassembler.
5. Compile only syntax accepted by the shared specification; unsupported syntax must fail with a source-located diagnostic.

### Phase D — optimization
1. Run constant folding only when the operation is pure and cannot change error behavior.
2. Add dead-code elimination only after control-flow and side-effect analysis.
3. Add jump threading after CFG construction and branch-target verification.
4. Benchmark each pass on representative programs; disable passes that regress real workloads.

## Diagnostics and process contract

Diagnostics are normative and use:

    [ErrorType] File "<filename>", line X, col Y: <Exact Message>

The diagnostic package defines stable categories and exit-code mapping: syntax/usage = 64, type/name/runtime = 70, I/O = 74. CLI adapters must preserve the first source span and write only the diagnostic to stderr. Successful commands write program output to stdout. Library APIs return typed errors and never call os.Exit.

## Conformance

conformance/runtime-v1.json defines byte-exact stdout, stderr, and process exit expectations. scripts/conformance.py runs Python and native engines in isolated processes. native/cmd/conformance provides a Go runner for the native binary. The fixture is a target contract and currently includes edge cases not yet normalized in the 0.3 runtimes; do not mark the migration complete until both runners pass the same suite.

Required edge classes include numeric overflow and underflow, integer range boundaries, closure capture and shadowing, recursive calls at and beyond configured depth, division by positive/negative zero, nil/nothing member/index access, malformed strings, source-location accuracy, and process exit status.

## Production gates

- go test ./... and go vet ./... pass.
- Python unittest suite passes.
- Both runtime conformance runners pass runtime-v1.json.
- Parser fuzzing completes without panic; malformed bytecode is rejected.
- Linux binaries run on amd64 and arm64 native runners.
- Windows and macOS binaries receive native smoke tests on hosted runners.
- SHA256SUMS are generated from final artifacts, not intermediate files.
- macOS release is not published unless Developer ID signing and notarization credentials are configured.

## Production Go runtime source decomposition

The former 3,259-line native/internal/tell/tell.go implementation has been decomposed into cohesive files within the existing runtime package:

- parser.go: source cleaning, statement/block parsing, and parsed statement nodes.
- runtime.go: environments, execution limits, runtime values, type declarations, built-ins, and sessions.
- execute.go: block and node execution.
- statement.go: statement execution and module/file statement handling.
- modules.go: module loading, invocation, and standard-library bridges.
- lexer.go: expression translation, tokenization, source coordinates, and expression entry points.

This is deliberately a same-package decomposition first. It reduces file-level coupling and keeps the runtime API stable; package-level extraction follows only where dependencies can be made acyclic without duplicating runtime types. The new internal compiler packages remain separately testable.
