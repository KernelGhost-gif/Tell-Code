# Tell Runtime Conformance Semantics (v1 target)

Status: normative target for the 0.4 migration. The current 0.3 statement interpreters are not yet conformant to every rule or diagnostic below. Changes that intentionally alter these rules require a new conformance schema version and a language compatibility note.

## Source model

Source files are UTF-8. Lines are separated by LF; a CRLF pair is treated as one line break. Lines and columns are 1-based. Offsets are zero-based UTF-8 byte offsets. Columns count Unicode code points, not bytes or UTF-16 code units. A span is half-open: start is included and end is excluded.

A diagnostic must refer to the first offending token or operator. Do not report a whole-statement start when a narrower token span exists. The exact format is:

    [ErrorType] File "<filename>", line X, col Y: <Exact Message>

The file name is the path supplied to the CLI, normalized only for presentation by the CLI. Runtime library APIs return a typed error and do not print or terminate the process.

## Portable scalar values

The portable scalar value set is boolean, nothing, text, and IEEE-754 binary64 number. The value written as nothing is the single null value. There is no implicit conversion from text or boolean to number. Numeric parsing accepts decimal and scientific notation. The runtime must preserve the IEEE-754 result for operations that produce infinity, NaN, or signed zero.

Portable number rendering is:
- Boolean: true or false.
- Null: nothing.
- Positive/negative infinity: infinity or -infinity.
- NaN: NaN.
- Finite integral binary64 values: rendered without a trailing decimal point.
- Other finite values: shortest round-trippable decimal representation.

Arithmetic:
- Addition, subtraction, multiplication, division, and power accept numeric operands; text plus text concatenates.
- Division by positive or negative zero raises RuntimeError with message "division by zero".
- Overflow of a finite operation follows IEEE-754 and produces infinity; underflow follows IEEE-754 and may produce signed zero.
- Comparisons return booleans. Equality is type-aware: true is not equal to 1, and nothing is equal only to nothing.
- Boolean and/or are short-circuiting. The left operand is evaluated first and the right operand is not evaluated when its value determines the result.

## Names, scopes, functions, and closures

Top-level declarations belong to the module scope. A function call creates a child lexical environment whose parent is the environment captured when the function was declared. Nested functions retain that environment after the outer function returns. Reads resolve from the innermost environment outward. Assignment updates the nearest existing binding; declaration creates a binding in the current scope. Unresolved names raise NameError.

A function call frame records the function, instruction pointer, base stack index, local environment, and active source location. The default maximum active Tell call depth is 64. The configured limit must be checked before entering another Tell function frame. A depth violation raises RuntimeError and must not leak a host stack trace.

## Nothing and missing values

A dictionary key that is absent and a variable that is bound to nothing are distinct states. Reading a missing variable raises NameError. Accessing a member or index on nothing raises RuntimeError; the runtime must not translate this into a host-language panic, AttributeError, or nil dereference.

## Resource and process behavior

Both command-line runtimes accept the same positive max-call-depth and max-loop-iterations options. Defaults are 64 active Tell calls and 1,000,000 iterations for any one loop. Exceeding a limit raises RuntimeError. These are execution guards, not an operating-system sandbox or a hard memory limit.

Process exit codes:
- 0: successful execution.
- 64: syntax or command usage error.
- 70: type, name, or runtime error.
- 74: file or I/O error.

Program output is written to stdout. One diagnostic is written to stderr. Do not add tell:, stack traces, usage banners, or debug output to a normal program error. Multiple diagnostics may be emitted only by commands explicitly documented to perform multi-file analysis.

## Conformance contract

Each case in schema v1 runs in a fresh process and temporary working directory. source_code is written to filename. Optional files entries create additional relative files and directories; absolute paths and traversal outside the temporary directory are rejected. stdin is passed to the process input stream. expected_stdout, expected_stderr, and expected_exit_code are byte-exact.

The fixtures in runtime-v1.json exercise arithmetic, closure capture, floating-point overflow/underflow, division by zero, null member access, and recursion limits. The smoke-v1.json suite is deliberately smaller and is used by CI while the legacy CLIs are migrated to the full diagnostic and semantic contract.
