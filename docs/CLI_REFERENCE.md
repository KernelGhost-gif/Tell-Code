# CLI Reference

The native Go executable runs Tell without Python. The Python runtime can be launched with `python -m tell` or through the installed `tell` entry point.

## Run and check programs

```text
tell program.tell
tell run program.tell
tell check program.tell
tell --check program.tell
tell -
```

Use `-` as the source path to read Tell source from standard input. `check` parses a program without executing it. The direct form `tell program.tell` remains supported.

## Format source

```text
tell format program.tell
tell format --check program.tell
tell format -
```

Formatting validates the source, normalizes block indentation to four spaces, and removes trailing whitespace. It preserves statement contents, comments, and punctuation. A file path is formatted in place. With `--check`, the command exits unsuccessfully when the file needs formatting. With `-`, formatted source is written to standard output.

## Run Tell source tests

```text
tell test
tell test examples
tell test examples/basic_test.tell
```

With no path, the runner discovers `*_test.tell` files recursively below the current directory. A directory argument uses the same pattern; a file argument runs that `.tell` file directly. Each file gets a fresh runtime. Use `Check that expression.` to fail a test when an expectation is false. The runner prints each file's result, includes captured program output for failures, exits `1` when a test fails, a test file cannot be read, or no test files are found, and exits `2` for an invalid path.

## Compile and run native bytecode

```text
tell build program.tell
tell build program.tell --output build/program.tbc
tell run program.tbc
tell program.tbc
```

`build` emits a versioned, JSON-encoded stack bytecode artifact (`.tbc`) with local `.tell` dependencies embedded, so the compiled program does not need the original module files at runtime. The current compiler subset supports `Let`, `Set`, `Remember ... as this.field`, output statements, bare expressions, `If`/`Otherwise`, `While`, `Repeat`, `For each`, loop break/continue, `Try`/`If something goes wrong`, `Raise an error saying ...`, local module imports, Tell function declarations, `Give back`, recursion, named and default arguments, final variadic parameters, lexical closures, classes, constructors, instance fields and methods, single inheritance, method overrides, and compiled class annotations. Expressions use the native Pratt parser, and safe scalar literal expressions are folded at compile time. `Finally`, typed/custom exception classes, class static members, access controls, multiple inheritance, and other unsupported statements fail compilation with a line diagnostic. The VM verifies opcode references and stack heights before execution. Bytecode runs on the native Go runtime only. Execution limits apply to compiled functions and loops. This is an initial compiler/VM milestone; it is not AOT native code, a full optimization pipeline, or cross-runtime bytecode.

## Interactive shell

```text
tell repl
```

The REPL keeps variables and functions between entries, accepts multiline blocks, prints `Say` output immediately, and echoes bare expression results like Python's interactive prompt:

```text
>>> Let answer be 2 plus 2.
>>> answer
4
>>> _ plus 1
5
```

`_` contains the most recent bare expression result. Commands are `:help`, `:clear`, `:exit`, `exit`, and `quit`. The native REPL runs independently of Python.

## Create a project

```text
tell new hello-world
tell init
tell init my-project
```

`new` creates a project subdirectory containing `tell.toml`, a runnable `main.tell`, and `tests/smoke_test.tell`. `init` adds the same files to the current directory, or uses the optional name in the generated manifest. Both commands refuse to replace existing project files. The manifest currently records the project name, initial version, and entrypoint; dependency resolution and package installation are not implemented.

## Options

Both runtimes accept `--version`, `--max-call-depth N`, and `--max-loop-iterations N`. Limits must be positive integers. The native help output lists the supported flags.

The native command `tell check --types program.tell` also checks primitive type annotations when their initializer or reassigned value has a statically known type. Dynamic expressions remain checked at runtime. `tell build` performs the same conservative checks automatically; the Python CLI does not yet provide this static-analysis pass.

## Not implemented yet

There is no package-management CLI, debugger, or language server. `tell new` and `tell init` scaffold a runnable project but do not resolve or install dependencies. The initial `build` command compiles only the documented native subset. `tell format` is a source formatter, not a compiler.
