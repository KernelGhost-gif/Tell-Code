# Foreign-language interoperability

Tell can call public functions from Python modules and JavaScript/Node.js modules. These adapters let a Tell program reuse existing package ecosystems without translating every dependency into Tell. The native Go runtime and Python interpreter both implement the two bridges.

## Python

```tell
Use the Python library called statistics as stats.
Let scores be [12, 18, 24, 30].
Say stats.mean(scores).
Say stats.fmean(data=scores).
```

Module names are dotted Python import names. If no alias is supplied, Tell uses the final module-name component. Public module functions accept positional and named arguments. The native runtime starts the selected Python executable for each call; set `TELL_PYTHON_EXECUTABLE` to select it. Otherwise it searches for `python3` and then `python`. The Python interpreter imports modules in its current process.

## JavaScript and Node.js

```tell
Use the JavaScript library called node:path as paths.
Say paths.basename("/projects/tell/main.js").

# An installed npm package can be imported the same way:
Use the JavaScript library called mathjs as maths.
Say maths.sqrt(144).
```

The module specifier may be a Node.js built-in such as `node:path`, an installed npm package such as `mathjs` or `@scope/package`, a package subpath, or a relative JavaScript file such as `./helpers.mjs`. Relative module specifiers resolve from the process working directory. If the alias is omitted, Tell derives one from the final module or file name.

Public JavaScript exports that are functions can be called with positional arguments. Tell named arguments are passed to JavaScript as one final options object, which works with APIs that accept an options parameter. The adapter supports CommonJS and ECMAScript modules, including functions that return promises. Node.js must be installed and the package must already be available; Tell does not install npm packages.

Set `TELL_NODE_EXECUTABLE` to select the Node.js executable. Otherwise Tell searches `PATH` for `node`. Each native-runtime or Python-runtime JavaScript call starts a fresh Node.js process and has a 30-second timeout. Module state is not preserved between calls. JavaScript modules can print to stdout without corrupting Tell's JSON response channel.

## Shared boundary and limitations

Arguments and results cross the foreign-language boundary as JSON values: nothing/null, booleans, finite numbers, text, lists/arrays, and dictionaries/objects with text keys. Python tuples and Tell sets are converted to JSON arrays. Functions, callbacks, class instances, cyclic objects, BigInt values, non-finite numbers, and identity-preserving handles are not supported. Convert complex objects to JSON-compatible structures in the foreign runtime before returning them.

The Go runtime uses its standard library for process management and JSON transport. No third-party language bridge is vendored, and packages are managed by the host language's package manager. The bridge does not sandbox foreign code. Imported modules and their functions run with the host user's permissions and may access files, networks, credentials, and other resources available to that account. Only import code you trust.

C, C++, Rust, JVM, .NET, and browser/WASM adapters are not included in this release. They remain possible future adapters; JavaScript and Python support should not be mistaken for universal source-language compatibility.
