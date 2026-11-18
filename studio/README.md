# Tell Studio

Tell Studio is the first native desktop-editor slice for Tell. It is written in
Rust and uses the GPUI framework from Zed. GPUI is pinned to commit
f3626706fa6855274a561f8f4aebf40666051596 so upstream changes do not silently break the app.

## Current capabilities

- Opens a Tell file supplied as the first command-line argument.
- Provides a single-file source buffer with UTF-8-safe cursor movement, insertion, deletion, and a visible caret.
- Saves the current file with the Save button or Ctrl/Command+S.
- Runs the saved file through the native Go CLI with the Run button or Ctrl/Command+R.
- Executes the Go process off the UI thread and displays stdout/stderr.

## Build

Install the Rust toolchain and the platform development packages required by GPUI. Then run:

~~~sh
cargo run --manifest-path studio/Cargo.toml -- path/to/program.tell
~~~

Tell Studio searches for tell on PATH. Set TELL_NATIVE_EXECUTABLE to use another native Go binary.

## Scope and next steps

This is an editor foundation, not a complete Zed replacement. It currently has one open buffer and a deliberately small editing model. Multi-tab workspaces, native file dialogs, selection and clipboard support, undo/redo, syntax highlighting, diagnostics, LSP integration (including the existing TellC server), debugger integration, and project search remain future work.

The Python/Tk IDE remains available only as a deprecated compatibility fallback while the Rust app and Go toolchain gain the required features. No claim of reduced memory or CPU use is made until the new app is benchmarked against the legacy IDE on the same workloads.
