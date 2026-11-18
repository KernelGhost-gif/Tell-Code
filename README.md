# Tell

**Tell is a sentence-first programming language.** Its goal is to make code read like clear instructions written in ordinary language, while keeping familiar programming capabilities underneath.

Tell's primary runtime is the standalone Go executable. The Python interpreter is deprecated and remains available only as a compatibility bridge while existing projects migrate. New language features target Go; new desktop editor work targets Rust/GPUI Studio.

Current version: **0.3.7**.

Tell can optionally call functions from installed Python modules and JavaScript/Node.js modules from either runtime. Foreign calls cross a JSON boundary and run in a separate process. See [foreign-language interoperability](docs/INTEROPERABILITY.md), [the Python example](examples/python_interop.tell), and [the JavaScript example](examples/javascript_interop.tell).

The experimental [TellC compiler](TellC/README.md) uses LLVM to build native executables from a separate, strongly typed Tell dialect. It is distinct from the Go runtime's bytecode compiler.

## A first program

```tell
Let name be "Ada".
Let age be 20.

If age is greater than 18:
    Say "Hello, {name}!".
    Say "You are an adult.".
Otherwise:
    Say "You are still young!".
End if.
```

Output:

```text
Hello, Ada!
You are an adult.
```

## Run Tell natively

The Go executable runs ordinary `.tell` programs without Python installed. Programs that explicitly import Python modules need a Python runtime; see [Python interoperability](docs/INTEROPERABILITY.md).

See [examples/language-core.tell](examples/language-core.tell) for Unicode identifiers, named arguments, short-circuit logic, and an otherwise-if chain.

Download the executable for your operating system from the `releases/` folder or the GitHub Actions build artifact.

| File | Platform |
|---|---|
| `tell-linux-amd64` | Linux x86-64 |
| `tell-linux-arm64` | Linux ARM64 |
| `tell-darwin-amd64` | macOS Intel |
| `tell-darwin-arm64` | macOS Apple Silicon |
| `tell-windows-amd64.exe` | Windows x86-64 |
| `tell-windows-arm64.exe` | Windows ARM64 |

On Linux or macOS, make the file executable once, then run it:

```bash
chmod +x ./tell-linux-amd64
./tell-linux-amd64 examples/hello.tell
./tell-linux-amd64 repl
./tell-linux-amd64 --check examples/features.tell
./tell-linux-amd64 --version
```

On Windows, run from PowerShell:

```powershell
.\tell-windows-amd64.exe examples\hello.tell
.\tell-windows-amd64.exe repl
.\tell-windows-amd64.exe --check examples\features.tell
.\tell-windows-amd64.exe --version
```

The executable is self-contained. It does not invoke Python, a shell, or a separately installed Go runtime. Use the matching OS/CPU build; macOS and Windows builds are cross-compiled in CI and should also be smoke-tested on those operating systems before a production release.

The native runtime can compile the documented core subset to stack bytecode and run it:

```text
tell build examples/hello.tell
tell run examples/hello.tbc
```

Use `tell build source.tell --output output.tbc` to choose the artifact path. The compiler supports functions, recursion, closures, core control flow, local modules embedded in the artifact, `Try`/`Raise`, and the initial class model with constructors, fields, methods, single inheritance, and overrides. `Finally` and other language features remain unsupported in bytecode; see the [CLI reference](docs/CLI_REFERENCE.md) for its exact subset and limits. [examples/compiled-objects.tell](examples/compiled-objects.tell) demonstrates an object program that can be run by either interpreter or compiled to bytecode by the native runtime.

## Typed Tell on the native Go runtime

The Go runtime supports an opt-in typed dialect with type-first declarations
and explicit function signatures:

~~~tell
to factorial(number: n) out number:
    if n <= 1:
        give back 1
    end if
    give back n * factorial(n - 1)
end function

let number answer be factorial(5)
say answer
~~~

Use `tell --typed native/examples/native-typed.tell` to require explicit
types for variables, function parameters, and return values. The same strict
validation is available through `tell check --typed` and `tell build --typed`.
Typed parameter and return contracts are enforced during normal execution
and preserved in bytecode. Existing dynamic Tell programs remain supported
without `--typed`; see the [typed Go dialect guide](docs/TYPED_TELL.md).

## Compile native executables with TellC

TellC is an experimental C++ compiler that translates its typed dialect through LLVM into a native executable. Its dialect is separate from the dynamically typed language used by the Go and Python runtimes; programs are not necessarily interchangeable. For example:

```tell
to factorial(number: n) out number:
    if n <= 1:
        return 1
    end if
    return n * factorial(n - 1)
end function

let number result be factorial(5)
say result
```

Declarations specify a type, function and method parameters use `type: name`, and every function declares a result with `out type` (or `out void`). The compiler checks initializers, arguments, and returns, and requires a value-returning function to return on every path. Built-in types include `number` (signed 64-bit integer), `decimal` (64-bit floating point), `text`, and `bool`. Newlines end statements; semicolons can separate statements on one line. Keywords and built-in type names are case insensitive; object type names are case sensitive.

The executable backend supports typed functions, integer arithmetic, equality, `if` blocks, output and interpolation, and objects with fields and typed methods. Object members can be private (`own`) or public (`others`); see [the object example](TellC/examples/objects.tell). The parser accepts some constructs that the backend cannot yet compile, including assignment, loops, and lists. Decimal arithmetic, imports, and executable foreign calls are also unsupported. TellC reports unsupported syntax or compilation features instead of silently running them; see the [TellC guide](TellC/README.md) for the complete current scope.

On Apple silicon, build the standalone compiler from the repository root with CMake, Ninja, Xcode command-line tools, and a matching LLVM 24 source tree:

```sh
LLVM_SOURCE=/path/to/llvm-project ./scripts/build-tellc-macos.sh
build/tellc-macos-arm64/tellc/tellc TellC/examples/factorial.tell -o factorial
./factorial
```

The example prints `120`. The standalone compiler embeds LLVM code generation and the Mach-O linker; it and its output target macOS 11 or later. The [standalone Linux build](TellC/docs/BUILDING.md#standalone-linux) produces static x86-64 and ARM64 compilers that can each emit executables for either architecture. Build details, dependencies, and tests are in the [TellC guide](TellC/README.md), [build guide](TellC/docs/BUILDING.md), and [test guide](TellC/docs/TESTING.md). `tellc source.tell` defaults to `a.out`; use `-o` to choose the output and `tellc --llvm-version` to inspect its LLVM version.

## Legacy Python runtime (deprecated)

Python 3.10 or newer is required only for this legacy compatibility runtime. New development should target the native Go runtime.

```bash
python tell.py examples/hello.tell
python -m tell run examples/hello.tell
python -m tell check examples/hello.tell
python -m tell run --max-call-depth 128 --max-loop-iterations 50000 examples/hello.tell
cat examples/hello.tell | python -m tell run -
python tell.py --check examples/features.tell
python tell.py --version
```

Or install the package:

```bash
python -m pip install .
tell examples/hello.tell
```

## Native desktop editor

The replacement editor is the early Rust/GPUI application in [studio/](studio/README.md). It opens one Tell source file, supports basic editing and saving, and runs the native Go executable. Build it from the repository root:

~~~sh
cargo run --manifest-path studio/Cargo.toml -- path/to/program.tell
~~~

Set TELL_NATIVE_EXECUTABLE to the Go executable path when it is not available as tell on PATH. This is an initial editor foundation, not yet a full Zed-style IDE; tabs, diagnostics, language-server integration, undo/redo, and project navigation remain future work.

The Python/Tk IDE is deprecated and remains temporarily available as a compatibility fallback through python -m tell ide, which prints a deprecation warning. New installations no longer include the separate tell-ide console command. See the [migration guide](docs/IDE.md).

## Language features

| Feature | Native Go runtime | Python runtime |
|---|---:|---:|
| Variables and assignment | Yes | Yes |
| Arithmetic and word operators | Yes | Yes |
| Text interpolation | Yes | Yes |
| If / otherwise-if / otherwise | Yes | Yes |
| While, repeat, for-each loops | Yes | Yes |
| Functions and recursion | Yes | Yes |
| Named arguments for Tell-defined functions, constructors, methods | Yes | Yes |
| Default and variadic function parameters | Yes | Yes |
| Unicode identifiers (letters, numbers, underscore) | Yes | Yes |
| Persistent multiline REPL | Yes | Yes |
| Lists and dictionaries | Yes | Yes |
| Multiple assignment and eager functional collections | Yes | Yes |
| Input and output | Yes | Yes |
| Basic file read/write/append | Yes | Yes |
| Try / error handling | Yes | Yes |
| Math, random, JSON, statistics, date/time libraries | Core functions | Python-backed allowlist |
| Full Python module ecosystem | Optional Python process bridge | Python imports in-process |
| JavaScript/Node.js module functions | Optional Node.js process bridge | Optional Node.js process bridge |
| Constructors, fields, instance methods, single inheritance | Yes | Yes |
| Local `.tell` modules with public exports and import caching | Yes | Yes |
| Static methods, access modifiers, multiple inheritance | Planned | Planned |
| Async, package manager | Planned | Planned |

The Go runtime is the primary implementation and the Python runtime is maintained only for compatibility during migration. Cross-runtime differences remain; use the Go executable for new projects. This is an evolving language, not a claim of complete Python compatibility.

## Syntax guide

### Variables and output

```tell
Let score be 10.
Set score to score plus 5.
Say "Your score is {score}.".
```

### Expressions

| Tell phrase | Meaning |
|---|---|
| `a plus b` | Addition |
| `a minus b` | Subtraction |
| `a times b` | Multiplication |
| `a divided by b` | Division |
| `a modulo b` | Remainder |
| `a to the power of b` | Exponentiation |
| `a is equal to b` | Equality |
| `a is not equal to b` | Inequality |
| `a is greater than b` | Greater than |
| `a is at least b` | Greater than or equal to |
| `a is less than b` | Less than |
| `a is at most b` | Less than or equal to |
| `a and b`, `a or b`, `not a` | Boolean logic |

The shorthand `count is 3` is also accepted. Ordinary arithmetic symbols are accepted too.

### Decisions

```tell
If temperature is greater than 30:
    Say "It is hot.".
Otherwise:
    Say "It is not too hot.".
End if.
```

### Loops

```tell
Repeat 3 times:
    Say "Round {the current repetition number}.".
End repeat.

For each colour in ["red", "green", "blue"]:
    Say colour.
End loop.

While count is less than 5:
    Set count to count plus 1.
End while.
```

Use `Stop this loop.` to break, or `Skip to the next repetition.` to continue.

### Functions

```tell
To add(first, second):
    Give back first plus second.
End function.

Let total be the result of add with 4 and 8.
Say total.
```

Parameters may also be written as `To add with first and second:`. Functions can call themselves recursively.

### Input and collections

```tell
Ask "What is your name? " and save the answer as name.
Let colours be ["red", "blue"].
Add "green" to colours.
Remove "red" from colours.
Set the item at position 0 in colours to "purple".

Let profile be {"name": "Ada", "age": 36}.
Set the key "age" in profile to 37.
Say profile["age"].

Let width, height be [640, 480].
Set width, height to [800, 600].
Say [width, height].
```

Built-ins include `len`, `range`, `sum`, `min`, `max`, `sorted`, `reversed`, `map`, `filter`, `reduce`, `abs`, `round`, `average`, `first`, `last`, `any`, `all`, `type_of`, `random_number`, `number`, `sqrt`, `floor`, and `ceiling`. See the [collections guide](docs/COLLECTIONS.md) for the shared functional operations and their semantics.

### Standard libraries

The native runtime provides these standard libraries:

```tell
Use the math library as maths.
Say maths.sqrt(144).

Use the json library.
Let data be json.loads("{\"answer\": 42}").
Say data["answer"].
```

Both runtimes provide the `csv` module with `parse(text)` and `stringify(rows)` operations. CSV values are text cells and stringify uses LF line endings. The standard-library modules are `math`, `random`, `statistics`, `json`, `datetime`, and `csv`; exact exports can vary between runtimes. See the [standard library reference](docs/STANDARD_LIBRARY.md).

Tell files can also import another `.tell` file relative to the importing source file:

```tell
Use the module "helpers.tell" as helpers.
Say helpers.triple(7).
```

Imports execute once per interpreter and export public top-level names. Names beginning with `_` are private. Circular imports are rejected. `tell build` compiles and embeds local module dependencies recursively, so a `.tbc` artifact does not need the source modules at runtime. See the [module guide](docs/MODULES.md) and [CLI reference](docs/CLI_REFERENCE.md). There is no package registry or dependency installer.

### Files

```tell
Write "Hello from Tell!" to the file "hello.txt".
Read the file "hello.txt" into message.
Say message.
Append " Another line." to the file "hello.txt".
```

File paths are relative to the folder from which Tell is launched. Only run programs you trust, especially when they read or write files.

### Error handling

```tell
Try:
    Let answer be 10 divided by 0.
If something goes wrong:
    Say "Something went wrong: {error_message}".
End try.
```

## Build from source

Create a project with `tell new hello-world`, then run `tell run hello-world/main.tell` and `tell test hello-world`. Use `tell init` to scaffold files in the current directory.

### Build the native executable for your current OS

Install Go 1.22 or newer, then:

```bash
cd native
go test ./...
go build -o tell ./cmd/tell
./tell ../examples/hello.tell
```

### Cross-compile all supported binaries from Linux, macOS, or WSL

Install Go 1.22 or newer and run:

```bash
./scripts/build-all.sh
```

The script uses `CGO_ENABLED=0`, so the outputs do not depend on a C runtime or external Go installation. It creates the six executables in `releases/` and writes a `SHA256SUMS` file.

### Run the tests

```bash
./scripts/test-native.sh
python -m unittest discover -s tests -v
```

Tell source tests use `Check that expression.` assertions and filenames ending in `_test.tell`. Run `python -m tell test` or `tell test` to discover them below the current directory; pass a directory or a single `.tell` file to narrow the run.

The Python command above tests the preserved Python implementation; it is not required to run native Tell programs.

## Project layout

```text
Tell-Code/
├── .github/workflows/      # Native CI and cross-platform builds
├── examples/               # Sample .tell programs
├── native/                 # Standalone Go runtime and tests
│   ├── cmd/tell/            # Native CLI entry point
│   └── internal/tell/       # Parser, evaluator, and runtime
├── TellC/                  # Experimental LLVM compiler and typed dialect
├── scripts/                # Build and test helpers
├── tell/                   # Python interpreter
├── tests/                  # Python interpreter tests
├── releases/                   # Prebuilt native executables
├── tell.py                 # Python convenience launcher
└── pyproject.toml           # Python package metadata
```

The supported core is described in [`docs/LANGUAGE_SPECIFICATION.md`](docs/LANGUAGE_SPECIFICATION.md), with the [CLI reference](docs/CLI_REFERENCE.md), [collections guide](docs/COLLECTIONS.md), [standard library reference](docs/STANDARD_LIBRARY.md), [variable and runtime type behavior](docs/VARIABLES_AND_TYPES.md), and the implemented object subset in [`docs/OBJECTS_AND_CLASSES.md`](docs/OBJECTS_AND_CLASSES.md). Architecture, security boundaries, and known gaps are documented in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), [`docs/SECURITY.md`](docs/SECURITY.md), and [`docs/ENGINEERING_AUDIT.md`](docs/ENGINEERING_AUDIT.md). The shared programs in `conformance/core.json` run in both runtime test suites.

Tagging a version as `v*` runs both test suites, cross-builds the six native targets, generates checksums, and publishes a GitHub Release using the workflow's `GITHUB_TOKEN` with `contents: write` permission.

## Roadmap

- **0.2:** Native runtime and multi-platform standalone binaries.
- **0.3:** Stronger grammar, better diagnostics, more consistent standard-library behavior.
- **0.4:** Extend the initial class model with access controls, static members, and richer object behavior.
- **0.5:** Package manager and debugger.
- **1.0:** Stable language specification and compatibility suite.

Tell is designed to make programming more approachable without making the language depend on guesswork. Its syntax is intentionally sentence-like, but constrained enough that every instruction has a predictable meaning.

## License

MIT. See [LICENSE](LICENSE).
