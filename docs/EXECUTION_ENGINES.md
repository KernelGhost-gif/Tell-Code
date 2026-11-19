# Tell execution engines

The native Go runtime has three execution paths. Select a path with
`--engine` when running a source program or a `.tbc` artifact.

## Interpreter

The interpreter remains the compatibility default:

~~~sh
tell program.tell
tell --engine interp program.tell
tell --engine interp program.tbc
~~~

Source is parsed and evaluated directly by the existing Go runtime. This path
supports the full Go-interpreter language implementation and is the fallback
for bytecode functions that cannot be safely compiled by the current JIT.

## AOT: portable bytecode

The build command compiles source ahead of execution into the versioned,
portable `tell-bytecode-v1` artifact:

~~~sh
tell build program.tell
tell build program.tell --output build/program.tbc
tell run build/program.tbc
~~~

This is AOT compilation to the Go VM's portable stack bytecode. It avoids
parsing and lowering source at each run and embeds compiled local modules.
It is **not** native machine-code AOT and does not produce a standalone OS
executable. The artifact is still run by the Go runtime.

## JIT: native hot-function compilation

Run either source or bytecode with:

~~~sh
tell --engine jit program.tell
tell --engine jit program.tbc
~~~

The first native backend is Linux x86-64 only. It lazily compiles eligible
functions when they are called. The compiled machine code is cached for the
duration of the process. Executable memory follows a write-then-execute
transition: pages are writable while emitted, then changed to read/execute.

The initial native code generator is deliberately conservative. A function
is eligible only when:

- every parameter is explicitly typed as `number` or `decimal`;
- its return type is `number` or `decimal`;
- it is not a method, variadic function, or function with default arguments;
- the body is a straight-line expression using numeric constants, parameter
  loads, unary `+`/`-`, binary `+`, `-`, `*`, and a return.

Division is deliberately left in the interpreter because Tell reports
division-by-zero as a language error, while a raw floating-point divide would
produce an infinity. Control flow, recursion, dynamic values, strings,
collections, I/O, calls, comparisons, and other operations are not
machine-compiled yet. Those functions execute through the existing bytecode
interpreter, even in `jit` mode. This preserves behavior while the JIT's
supported subset grows.

The initial backend is implemented with a small Linux/amd64 code emitter. It
does not add an LLVM or CGo build dependency. Other operating systems and CPU
architectures use the interpreter fallback; explicit `--engine jit` reports
that the native backend is unavailable there.

## Automatic tiering

Use automatic mode for a mixed program:

~~~sh
tell --engine auto --jit-threshold 24 program.tell
tell --engine auto --jit-threshold 24 program.tbc
~~~

Eligible functions run in the interpreter during warm-up. After the configured
number of calls, the runtime compiles the function once and routes subsequent
calls to native code. Unsupported functions remain interpreted. The default
threshold is 24 calls.

To inspect what actually ran:

~~~sh
tell --engine auto --jit-stats program.tell
~~~

The report shows compiled functions, native calls, and interpreted calls. A
function being declared as typed does not automatically make its body JIT
eligible; the body must match the supported numeric subset.

## Validation and performance

The Go test suite compares native and interpreted results, verifies the
automatic hot threshold, and verifies interpreter fallback for control flow
and division-by-zero semantics. Run the focused benchmark with:

~~~sh
cd native
go test ./internal/tell -run '^$' -bench '^BenchmarkTypedNumericFunctionEngines$' -benchmem -benchtime=250ms -count=1
~~~

### Latest successful CI benchmark sample

The latest successful PR validation run measured the warmed-up execution of
the same typed function containing 159 numeric arithmetic operations on
Ubuntu 24.04 with Go 1.23.12 and an AMD EPYC 7763 64-Core Processor. Each
sub-benchmark ran for 250 ms:

| Engine | Time per call | Allocations per call |
| --- | ---: | ---: |
| Interpreter | 8,994 ns | 167 |
| Native JIT | 916.5 ns | 9 |
| Auto (after warm-up) | 939.8 ns | 9 |

Source: [Native JIT validation and benchmark, PR #22](https://github.com/KernelGhost-gif/Tell-Code/actions/runs/44456809768).

This is a synthetic, compute-heavy function benchmark. The harness excludes
one-time JIT compilation from the timed section and calls the compiled
function directly; it does not measure source parsing, whole-program startup,
I/O, dynamic code, or end-to-end application performance. Treat the numbers
as an initial reference for this specific supported subset, not a universal
speedup guarantee. Benchmark representative applications before making
product-wide performance claims.

The JIT is an initial execution tier, not yet a whole-program optimizing
compiler. True native AOT output, additional CPU backends, register
allocation, inlining, deoptimization, and broader typed code generation remain
future work.
