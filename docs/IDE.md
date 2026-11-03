# Tell IDE

Tell IDE is an optional desktop editor and interactive shell built with Python's Tk interface. It follows the editing and run loop of Python IDLE while executing programs with Tell itself.

## Start

From a Tell source checkout:

```console
python -m tell ide
```

After installing the Python package, use either command:

```console
tell ide
tell-ide
```

The UI requires Python 3.10 or newer and Tkinter. Windows Python installers normally include Tkinter. On Linux, install the distribution's `python3-tk` package if needed. The Go runtime is preferred for execution. Set `TELL_NATIVE_EXECUTABLE` to a native Tell executable to select one explicitly. If no native executable is found, the editor falls back to the Python Tell runtime. A Python fallback requires the Tell Python package to be importable by the child process.

## Editor and shell

- Open multiple `.tell` files in tabs; create, save, and close files with unsaved-change prompts.
- Use syntax colours, line numbers, block-aware indentation, horizontal and vertical scrolling, and Find.
- The bottom shell is persistent: variables and functions survive between entries. Use Shift+Enter to write a multi-line shell entry, or enter a block one line at a time.
- Shell input is routed to a running program while it is waiting for `Ask`; otherwise, it goes to the persistent Tell REPL.
- Run a module with F5, check syntax with F7, build native bytecode with Ctrl+B, and run project tests with Ctrl+T.
- Build output is written beside the source as `.tbc`. The IDE asks before replacing an existing bytecode file.
- Run and check can execute the current unsaved buffer as a temporary source file in the same directory, preserving relative module lookup. Temporary files are removed when the command exits.

The IDE is an editor and runtime console, not a debugger or language server. Highlighting is lexical and provides no static diagnostics while typing. Bytecode building requires the native Go runtime; ordinary run/check operations can use either runtime.
