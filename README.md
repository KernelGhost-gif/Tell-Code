<div align="center">

<img src="assets/tell-banner.svg" alt="Tell — code that reads like clear instructions" width="100%">

<p><strong>A sentence-first programming language with a native Go runtime.</strong><br>
Readable instructions on the surface. A real language implementation underneath.</p>

<p>
  <a href="https://github.com/KernelGhost-gif/Tell-Code/actions/workflows/native.yml"><img src="https://github.com/KernelGhost-gif/Tell-Code/actions/workflows/native.yml/badge.svg?branch=main" alt="Native runtime CI"></a>
  <a href="https://github.com/KernelGhost-gif/Tell-Code/actions/workflows/tellc.yml"><img src="https://github.com/KernelGhost-gif/Tell-Code/actions/workflows/tellc.yml/badge.svg?branch=main" alt="TellC CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-8b7cff.svg" alt="MIT License"></a>
  <img src="https://img.shields.io/badge/version-0.3.7-ffad7a.svg" alt="Tell version 0.3.7">
  <img src="https://img.shields.io/badge/native%20runtime-Go%201.22%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.22 or newer">
  <img src="https://img.shields.io/badge/builds-6%20OS%2FCPU%20targets-35b7a4.svg" alt="Six OS and CPU build targets">
  <a href="https://github.com/KernelGhost-gif/Tell-Code/stargazers"><img src="https://img.shields.io/github/stars/KernelGhost-gif/Tell-Code?style=social" alt="GitHub stars"></a>
</p>

<p>
  <a href="#quick-start">Quick start</a> ·
  <a href="#the-tell-way">The Tell way</a> ·
  <a href="#what-works-today">Features</a> ·
  <a href="#the-toolchain">Toolchain</a> ·
  <a href="#documentation">Docs</a> ·
  <a href="#contributing">Contribute</a>
</p>

</div>

---

## What if code read more like instructions?

Most programming languages make you learn their syntax before you can express an idea. **Tell starts from the other direction:** write clear, sentence-shaped instructions, while keeping the structure and predictability of a real programming language.

`Let`, `Set`, `Say`, `If`, `Otherwise`, `For each`, `To`, and `Give back` are not decoration. They are part of a defined grammar interpreted by a real runtime.

Tell is not an AI prompt parser. It does not guess what a sentence means, and it is not ordinary English pretending to be code. It is a programming language with syntax rules, functions, collections, modules, diagnostics, tests, and multiple execution paths.

## A first program

```tell
Let colleague be "Avery".
Let workspace be "Customer Operations".

Say "Welcome, {colleague}."
Say "Your {workspace} workspace is ready."
```

Output:

```text
Welcome, Avery.
Your Customer Operations workspace is ready.
```

Readable enough to explain itself. Structured enough for a machine to execute.

## Quick start

### 1. Download Tell

The repository includes native Go executables for six OS/CPU combinations. Choose the one that matches your machine:

| Platform | Download |
|---|---|
| Linux x86-64 | [tell-linux-amd64](https://github.com/KernelGhost-gif/Tell-Code/raw/refs/heads/main/releases/tell-linux-amd64) |
| Linux ARM64 | [tell-linux-arm64](https://github.com/KernelGhost-gif/Tell-Code/raw/refs/heads/main/releases/tell-linux-arm64) |
| macOS Intel | [tell-darwin-amd64](https://github.com/KernelGhost-gif/Tell-Code/raw/refs/heads/main/releases/tell-darwin-amd64) |
| macOS Apple Silicon | [tell-darwin-arm64](https://github.com/KernelGhost-gif/Tell-Code/raw/refs/heads/main/releases/tell-darwin-arm64) |
| Windows x86-64 | [tell-windows-amd64.exe](https://github.com/KernelGhost-gif/Tell-Code/raw/refs/heads/main/releases/tell-windows-amd64.exe) |
| Windows ARM64 | [tell-windows-arm64.exe](https://github.com/KernelGhost-gif/Tell-Code/raw/refs/heads/main/releases/tell-windows-arm64.exe) |

For example, download and run the Linux x86-64 binary:

```sh
curl -fL \
  -o tell \
  https://github.com/KernelGhost-gif/Tell-Code/raw/refs/heads/main/releases/tell-linux-amd64

chmod +x tell
./tell examples/hello.tell
```

On Windows PowerShell:

```powershell
Invoke-WebRequest -Uri "https://github.com/KernelGhost-gif/Tell-Code/raw/refs/heads/main/releases/tell-windows-amd64.exe" -OutFile "tell.exe"
.\tell.exe examples\hello.tell
```

The native executable does not need Python or a separate Go installation. Python and Node.js are required only when a program explicitly calls their interoperability bridges. The checked-in binaries are convenient snapshots; build from your checkout when you need an executable that exactly matches your source. Verify a binary with `tell --version`. SHA-256 checksums are in [`releases/SHA256SUMS`](releases/SHA256SUMS).

### 2. Run, check, and explore

```sh
tell examples/hello.tell          # run a program
tell check examples/features.tell # check without executing
tell repl                         # open the interactive shell
tell test examples                # run Tell source tests
tell format examples/hello.tell   # format a source file
tell --version                    # show the runtime version
```

The native REPL keeps variables and functions between entries, accepts multiline blocks, prints `Say` output immediately, and remembers the most recent bare expression result as `_`.

### 3. Build portable bytecode

```sh
tell build examples/hello.tell
tell run examples/hello.tbc
```

The Go runtime can compile the documented language subset into a versioned stack-bytecode artifact. Local `.tell` dependencies are embedded in the artifact. This is ahead-of-time compilation to Tell bytecode, **not** native machine-code compilation and not a standalone executable.

## The Tell way

The syntax is intentionally familiar, but it is still precise.

| Tell | Meaning |
|---|---|
| `Let score be 10.` | Declare a variable |
| `Set score to score plus 5.` | Update a value |
| `Say "Hello".` | Print output |
| `If ready:` / `Otherwise:` | Choose a path |
| `Otherwise if condition:` | Add another branch |
| `For each item in items:` | Iterate over a collection |
| `Repeat 3 times:` | Repeat a fixed number of times |
| `To greet(name):` | Declare a function |
| `Give back value.` | Return a value |
| `Use the module "helpers.tell" as helpers.` | Import a local Tell module |

Word operators work alongside ordinary symbols:

| Tell expression | Familiar equivalent |
|---|---|
| `a plus b` | `a + b` |
| `a times b` | `a * b` |
| `a divided by b` | `a / b` |
| `a is equal to b` | `a == b` |
| `a is at least b` | `a >= b` |
| `a and b`, `a or b`, `not a` | Boolean logic |

Periods at the end of statements are optional. Indentation keeps blocks readable, while explicit `End if.`, `End loop.`, and `End function.` terminators make their boundaries clear.

### Functions, loops, and collections

The documented runtime subset includes functions, recursion, closures, named/default/variadic parameters, lists, tuples, sets, dictionaries, multiple assignment, slicing, and eager collection operations such as `map`, `filter`, and `reduce`.

```tell
To average(values):
    Let total be 0.
    For each value in values:
        Set total to total plus value.
    End loop.
    Give back total divided by len(values).
End function.

Let scores be [12, 18, 24, 30].
Let result be the result of average with scores.
Say "Average score: {result}."
```

There is also basic error handling, local `.tell` modules, file read/write/append operations, and a growing standard library. The [language specification](docs/LANGUAGE_SPECIFICATION.md) documents the shared core and calls out runtime-specific differences.

## What works today

The **native Go runtime is Tell's primary implementation** and the recommended target for new programs.

| Capability | Current state |
|---|---|
| Native source interpreter | Main execution path |
| Interactive multiline REPL | Available |
| Source checking, formatting, and Tell test runner | Available |
| Functions, recursion, closures, collections, and basic classes | Available in the documented subset |
| Local `.tell` modules | Available; no package registry yet |
| Conservative type/declaration checks | Available through `--types` |
| Portable stack bytecode and VM | Available for a documented subset |
| Native JIT tier in the Go runtime | Initial Linux x86-64 numeric subset |
| Python and JavaScript module calls | Available through optional process bridges |
| TellC LLVM compiler | Separate experimental typed dialect |
| Tell Studio desktop editor | Early Rust/GPUI editor foundation |

### Three execution paths in the Go runtime

| Engine | What it does |
|---|---|
| Interpreter | Executes source directly; the compatibility default |
| AOT bytecode | Compiles the supported subset into portable `.tbc` artifacts for the Go VM |
| JIT / automatic tiering | Compiles eligible hot typed numeric functions to native code on Linux x86-64 |

Try the execution modes:

```sh
tell --engine interp examples/features.tell
tell --typed --engine jit native/examples/jit-numeric.tell
tell --typed --engine auto --jit-threshold 24 --jit-stats native/examples/jit-numeric.tell
```

The JIT is intentionally conservative: it currently targets a small, straight-line numeric function subset with explicit `number` or `decimal` signatures. Unsupported functions stay on the interpreter path. It is an early execution tier, not a whole-program optimizing compiler. See [Execution Engines](docs/EXECUTION_ENGINES.md) for the supported operations and benchmark caveats.

### Optional Python and JavaScript interoperability

Tell can call public functions from installed Python modules and JavaScript/Node.js modules. That lets a Tell program reuse existing libraries rather than reimplementing everything.

```tell
Use the Python library called statistics as stats.
Say stats.mean([12, 18, 24, 30]).

Use the JavaScript library called node:path as paths.
Say paths.basename("/projects/tell/main.tell").
```

Python and Node.js must be installed, and any third-party packages must already be available to those runtimes. Foreign calls run in separate processes and exchange JSON-compatible values. This is interoperability, **not** a universal multi-language runtime or a way to import arbitrary C++, Rust, JVM, or .NET code. Read the [interoperability contract](docs/INTEROPERABILITY.md) before relying on a bridge.

## The toolchain

Tell is growing beyond one interpreter. These components are related, but they are not interchangeable.

### Tell — the native Go runtime

The primary implementation for everyday Tell programs. It includes the interpreter, REPL, CLI, type/declaration checks, the initial bytecode compiler/VM, and the first native JIT tier.

- [Go runtime CLI source](native/cmd/tell/main.go)
- [CLI reference](docs/CLI_REFERENCE.md)
- [Execution engine guide](docs/EXECUTION_ENGINES.md)

### TellC — an experimental LLVM compiler

TellC is a separate C++17 compiler for a strongly typed Tell dialect. It generates LLVM IR and can produce native executables; supported LLVM configurations also provide JIT execution. Its syntax and type model are distinct from the dynamically typed Go runtime, so a program written for one dialect is not automatically compatible with the other.

TellC also has a C-ABI foreign-function interface for calling compatible prebuilt C/C++ libraries through exported wrappers. It does not compile arbitrary C++ source or expose C++ classes/templates directly.

- [TellC overview](TellC/README.md)
- [TellC architecture](TellC/docs/ARCHITECTURE.md)
- [Build instructions](TellC/docs/BUILDING.md)
- [Test coverage](TellC/docs/TESTING.md)

### Tell Studio — an early native editor

Tell Studio is a Rust application built with the GPUI framework. The current foundation opens one Tell file, supports basic editing and saving, and runs it through the native Go executable. It is not yet a full IDE: multi-tab projects, undo/redo, diagnostics, syntax highlighting, LSP integration, and debugging remain future work.

- [Tell Studio details](studio/README.md)

### The legacy Python runtime

The Python interpreter remains as a deprecated compatibility path for existing programs. New language development targets the Go runtime; the Python implementation is not the primary direction.

## A few examples to explore

| Example | What to look for |
|---|---|
| [hello.tell](examples/hello.tell) | The smallest complete program |
| [language-core.tell](examples/language-core.tell) | Core syntax and language behavior |
| [features.tell](examples/features.tell) | Functions, loops, branches, and math |
| [factorial.tell](examples/factorial.tell) | Recursion |
| [guessing_game.tell](examples/guessing_game.tell) | Interactive input |
| [sets.tell](examples/sets.tell) | Set operations |
| [tuples.tell](examples/tuples.tell) | Tuples and unpacking |
| [modules.tell](examples/modules.tell) | Local module imports |
| [python_interop.tell](examples/python_interop.tell) | Python bridge |
| [javascript_interop.tell](examples/javascript_interop.tell) | JavaScript bridge |
| [compiled-objects.tell](examples/compiled-objects.tell) | Object model and bytecode subset |

The repository also includes a shared [conformance corpus](conformance/README.md) used to compare behavior across implementations.

## Build from source

### Build the Go executable

Install Go 1.22 or newer:

```sh
cd native
go test ./...
go vet ./...
go build -o tell ./cmd/tell
./tell ../examples/hello.tell
```

### Cross-build all six native binaries

From the repository root:

```sh
./scripts/build-all.sh
```

The script cross-compiles Linux, macOS, and Windows targets for amd64 and arm64 with `CGO_ENABLED=0`. It writes the six executables, a `SHA256SUMS` file, an SPDX software bill of materials, and third-party notices into `releases/`.

### Run the tests

```sh
# Native Go runtime
cd native
go test ./...
go test -race ./...
go vet ./...

# Python compatibility runtime and project tests
cd ..
python -m unittest discover -s tests -v

# Tell source-level tests
./releases/tell-linux-amd64 test examples
```

The CI workflows also run shared runtime conformance fixtures and build TellC in its configured platform jobs.

## Project map

```text
Tell-Code/
├── .github/workflows/   # Runtime, compiler, editor, and release automation
├── assets/              # README artwork
├── conformance/         # Shared behavior fixtures
├── docs/                # Language, CLI, runtime, and engineering docs
├── examples/            # Runnable Tell programs
├── native/              # Primary Go runtime, VM, compiler, and JIT
├── TellC/               # Experimental C++ / LLVM compiler
├── studio/              # Early Rust / GPUI desktop editor
├── tell/                # Legacy Python compatibility runtime
├── tests/               # Python and project tests
├── scripts/             # Build, packaging, and test helpers
└── releases/            # Checked-in native binaries and metadata
```

## Documentation

| Guide | What it covers |
|---|---|
| [Language specification](docs/LANGUAGE_SPECIFICATION.md) | Grammar and documented core behavior |
| [Typed Tell](docs/TYPED_TELL.md) | Opt-in typed dialect on the Go runtime |
| [CLI reference](docs/CLI_REFERENCE.md) | Commands, flags, REPL, and bytecode limits |
| [Collections](docs/COLLECTIONS.md) | Lists, tuples, sets, dictionaries, and operations |
| [Standard library](docs/STANDARD_LIBRARY.md) | Built-in and library APIs |
| [Modules](docs/MODULES.md) | Local source modules and exports |
| [Interoperability](docs/INTEROPERABILITY.md) | Python and Node.js process bridges |
| [Execution engines](docs/EXECUTION_ENGINES.md) | Interpreter, bytecode VM, JIT, and limitations |
| [Objects and classes](docs/OBJECTS_AND_CLASSES.md) | Implemented object model |
| [Architecture](docs/ARCHITECTURE.md) | Runtime structure and boundaries |
| [Security](docs/SECURITY.md) | Execution effects and trust boundaries |
| [Engineering audit](docs/ENGINEERING_AUDIT.md) | Known gaps and implementation status |

## Roadmap

Tell is an evolving language. Some areas are implemented; others are still being designed or built.

- Expand the shared grammar and conformance coverage across runtimes.
- Grow the bytecode compiler and VM while keeping its supported subset explicit.
- Extend the native JIT beyond its initial numeric-function tier.
- Develop TellC's typed dialect, compiler, and platform support.
- Grow Tell Studio from a single-file editor foundation toward a fuller development environment.
- Explore package management, debugging, and language tooling.

These are directions, not claims that those features already exist. The [engineering audit](docs/ENGINEERING_AUDIT.md) and [progress notes](docs/PROGRESS.md) provide more detail on current gaps.

## A note on safety

Tell is an interpreter, **not a sandbox**. Programs run with the permissions of the user who launches them and can perform file operations. Python/Node.js modules run with the host runtime's permissions too. Do not execute untrusted Tell programs or import untrusted modules as if they were isolated. See [Security](docs/SECURITY.md).

## Contributing

Contributions, bug reports, language ideas, examples, and thoughtful criticism are welcome.

1. Read the [language specification](docs/LANGUAGE_SPECIFICATION.md) before proposing syntax changes.
2. Check the [engineering audit](docs/ENGINEERING_AUDIT.md) for known limitations.
3. Add or update tests with behavior changes.
4. Run the Go and Python test suites relevant to your change.
5. Keep experimental features and unsupported syntax clearly labelled.

Open an [issue](https://github.com/KernelGhost-gif/Tell-Code/issues) for bugs or proposals, or submit a [pull request](https://github.com/KernelGhost-gif/Tell-Code/pulls).

---

<div align="center">

### If Tell's idea interests you, give it a star.

A star helps other developers discover the project, and feedback helps shape what the language becomes.

<a href="https://github.com/KernelGhost-gif/Tell-Code">
  <img src="https://img.shields.io/badge/Explore-Tell_Code-20233d?style=for-the-badge&logo=github" alt="Explore Tell Code">
</a>
<a href="https://github.com/KernelGhost-gif/Tell-Code/stargazers">
  <img src="https://img.shields.io/badge/Star-this%20project-f4c95d?style=for-the-badge&logo=github" alt="Star this project">
</a>

**Readable code. Real execution. A language still being built.**

</div>

## License

Tell is released under the [MIT License](LICENSE).
