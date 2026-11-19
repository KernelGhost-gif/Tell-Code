# TellC language server

The TellC language server is a C++ executable that speaks LSP over stdio.
It shares TellC's parser and LLVM-backed compiler checks. It analyzes unsaved
buffers in-process without invoking `tellc`, emitting an executable, or
running the program.

Build the `tell-lsp` target in a configured TellC build directory. See the
[TellC build instructions](../TellC/README.md) for the LLVM configuration.

```sh
cmake --build build/tellc --target tell-lsp
```

The Apple silicon build script also builds the server at
`build/tellc-macos-arm64/tellc/tell-lsp`. Use the executable's absolute path
in your editor configuration, or install it into a directory on your PATH:

```sh
cmake --install build/tellc --prefix "$HOME/.local"
```

If you previously installed the Python package, point your editor at the new
executable so it picks up the C++ server. The new server needs no Python runtime
or `TELL_LSP_COMPILER` setting.

For Neovim 0.11 or newer, run the setup script from the repository root.
It installs file detection, syntax highlighting, diagnostics, and automatic
keyword completion for `.tell` files.

```sh
./scripts/setup-nvim.sh
```

The script looks for `tell-lsp` in the checkout's usual build directories and
on your PATH. You can also pass the executable path as the first argument.
Restart Neovim and open a `.tell` file. The script leaves your `init.lua`
in place and refuses to overwrite existing Tell files it did not install.

The server reports the first TellC error per buffer and offers keyword
completions, including while the buffer contains syntax errors. It uses
full-document synchronization. It supports TellC's typed dialect.

With Python available for the test harness, run the stdio integration tests
from the configured build directory:

```sh
ctest --test-dir build/tellc -R '^language_server$' --output-on-failure
```
