# Tell

**Tell is a sentence-first programming language.** Its goal is to make code read like clear instructions written in ordinary language, while keeping familiar programming capabilities underneath.

Tell has two runtimes:

- **Native runtime:** a standalone executable written in Go. It runs `.tell` programs directly and does not need Python or Go installed on the user's machine.
- **Python runtime:** the original interpreter, kept for compatibility, experimentation, and people who want to embed or extend Tell from Python.

Current version: **0.2.0**.

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

## Run Tell without Python

Download the executable for your operating system from the `dist/` folder or the GitHub Actions build artifact.

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
./tell-linux-amd64 --check examples/features.tell
./tell-linux-amd64 --version
```

On Windows, run from PowerShell:

```powershell
.\tell-windows-amd64.exe examples\hello.tell
.\tell-windows-amd64.exe --check examples\features.tell
.\tell-windows-amd64.exe --version
```

The executable is self-contained. It does not invoke Python, a shell, or a separately installed Go runtime. Use the matching OS/CPU build; macOS and Windows builds are cross-compiled in CI and should also be smoke-tested on those operating systems before a production release.

## Run the Python version

Python 3.10 or newer is required for this runtime only.

```bash
python tell.py examples/hello.tell
python tell.py --check examples/features.tell
python tell.py --version
```

Or install the package:

```bash
python -m pip install .
tell examples/hello.tell
```

## Language features

| Feature | Native Go runtime | Python runtime |
|---|---:|---:|
| Variables and assignment | Yes | Yes |
| Arithmetic and word operators | Yes | Yes |
| Text interpolation | Yes | Yes |
| If / otherwise | Yes | Yes |
| While, repeat, for-each loops | Yes | Yes |
| Functions and recursion | Yes | Yes |
| Lists and dictionaries | Yes | Yes |
| Input and output | Yes | Yes |
| Basic file read/write/append | Yes | Yes |
| Try / error handling | Yes | Yes |
| Math, random, JSON, statistics, date/time libraries | Core functions | Python-backed allowlist |
| Full Python module ecosystem | No | No, intentionally allowlisted |
| Classes, inheritance, async, package manager | Planned | Planned |

The native and Python runtimes aim to share the same core syntax. The Python runtime currently has a broader set of built-ins and standard-library behavior. This is an evolving language, not a claim of complete Python compatibility.

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
```

Built-ins include `len`, `range`, `sum`, `min`, `max`, `sorted`, `reversed`, `abs`, `round`, `average`, `first`, `last`, `type_of`, `random_number`, `number`, `sqrt`, `floor`, and `ceiling`.

### Standard libraries

The native runtime provides these standard libraries:

```tell
Use the math library as maths.
Say maths.sqrt(144).

Use the json library.
Let data be json.loads("{\"answer\": 42}").
Say data["answer"].
```

Native modules currently include `math`, `random`, `statistics`, `json`, and `datetime`. The Python runtime has a similar allowlist.

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

The script uses `CGO_ENABLED=0`, so the outputs do not depend on a C runtime or external Go installation. It creates the six executables in `dist/` and writes a `SHA256SUMS` file.

### Run the tests

```bash
./scripts/test-native.sh
python -m unittest discover -s tests -v
```

The Python command above tests the preserved Python implementation; it is not required to run native Tell programs.

## Project layout

```text
Tell-Code/
├── .github/workflows/      # Native CI and cross-platform builds
├── examples/               # Sample .tell programs
├── native/                 # Standalone Go runtime and tests
│   ├── cmd/tell/            # Native CLI entry point
│   └── internal/tell/       # Parser, evaluator, and runtime
├── scripts/                # Build and test helpers
├── tell/                   # Python interpreter
├── tests/                  # Python interpreter tests
├── dist/                   # Prebuilt native executables
├── tell.py                 # Python convenience launcher
└── pyproject.toml           # Python package metadata
```

## Roadmap

- **0.2:** Native runtime and multi-platform standalone binaries.
- **0.3:** Stronger grammar, better diagnostics, more consistent standard-library behavior.
- **0.4:** Classes, objects, and modules.
- **0.5:** Package manager and debugger.
- **1.0:** Stable language specification and compatibility suite.

Tell is designed to make programming more approachable without making the language depend on guesswork. Its syntax is intentionally sentence-like, but constrained enough that every instruction has a predictable meaning.

## License

MIT. See [LICENSE](LICENSE).
