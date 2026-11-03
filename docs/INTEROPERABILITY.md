# Foreign-language interoperability

Tell currently has an optional Python module adapter. Ordinary Tell programs and the Go runtime do not require Python. A program that imports a Python module requires that module to be installed in the Python environment used by the adapter.

```tell
Use the Python library called statistics as stats.
Let scores be [12, 18, 24, 30].
Say stats.mean(scores).
Say stats.fmean(data=scores).
```

Module names may be dotted Python import names. If no alias is supplied, Tell uses the final module-name component. Public module functions can be called with positional or named arguments. Arguments and return values cross the Go/Python boundary as JSON values: `nothing`, booleans, numbers, text, lists/tuples, and dictionaries with text keys. Python objects, callbacks, class instances, NumPy arrays, and identity-preserving handles are not supported by this adapter. Convert such values to JSON-compatible structures in Python before returning them.

The Python interpreter imports the requested module in its current process. The native Go runtime starts the selected Python executable for each foreign function call, sends one JSON request over standard input, and reads one JSON response. Set `TELL_PYTHON_EXECUTABLE` to select the executable; otherwise Go searches for `python3` and then `python`. A call has a 30-second process limit. Python module imports and functions execute with the host user's permissions; this bridge is not a sandbox. Python's own standard output is isolated from Tell's output channel.

The bridge uses only Go's standard library and Python's standard library for transport. No third-party bridge dependency is vendored. Python packages remain user-managed and are not installed automatically by Tell. The Go adapter currently supports module functions only; it does not keep Python process state between calls.

Run the example with the native executable:

```powershell
tell run examples/python_interop.tell
```

```sh
tell run examples/python_interop.tell
```

See [the architecture notes](ARCHITECTURE.md) for the wider compiler/runtime boundaries. C ABI, Rust, JavaScript, JVM, .NET, and browser/WASM adapters remain future work.
