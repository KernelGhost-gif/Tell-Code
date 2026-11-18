# Tell Studio and IDE migration

The Python/Tk desktop IDE is deprecated. It remains available temporarily as a
compatibility fallback, but new editor work belongs in the Rust/GPUI
application under [studio/](../studio/README.md).

## Current replacement: Tell Studio

Tell Studio is an early native desktop shell built on GPUI, the Rust UI
framework used by Zed. It provides a single-file source workspace, basic
keyboard editing, save, and execution through the native Go runtime.

Build and launch it from the repository root:

~~~sh
cargo run --manifest-path studio/Cargo.toml -- path/to/program.tell
~~~

The native Go executable must be available as tell on PATH, or set
TELL_NATIVE_EXECUTABLE to its absolute path. Studio is an early foundation,
not a full Zed-style IDE yet: multi-tab editing, diagnostics, language-server
integration, debugger tooling, and project navigation are future work.

## Legacy Python/Tk editor

For compatibility during the transition only:

~~~sh
python -m tell ide
~~~

This command prints a deprecation warning. The standalone tell-ide Python
console entry point is no longer installed by new package builds.

The legacy UI requires Python 3.10 or newer and Tkinter. Existing projects may
continue using it while migrating, but new features should be added to
studio/ and the native Go toolchain instead.

## Migration policy

The Python interpreter and IDE are not deleted in this change. They remain
compatibility components until supported Go runtime features and the Rust
editor reach sufficient parity. The Python runtime should receive only
security, compatibility, and migration fixes; new runtime features belong in
Go. The IDE should receive only critical compatibility fixes; new editor
features belong in GPUI Studio.

For editor diagnostics for the typed TellC dialect, see the separate [TellC language server](../lsp/README.md).
