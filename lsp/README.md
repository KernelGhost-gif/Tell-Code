# TellC language server

The TellC language server is a C++ executable that speaks LSP over stdio.
It checks unsaved buffers with the same parser and LLVM checks as TellC.

Build the server from a configured TellC build directory. See the
[TellC build guide](../TellC/docs/BUILDING.md) for LLVM setup.

```sh
cmake --build build/tellc --target tell-lsp
cmake --install build/tellc --prefix "$HOME/.local"
```

The Apple silicon build script also builds the server at
`build/tellc-macos-arm64/tellc/tell-lsp`. If your editor still points to
the old Python launcher, set its command to the new executable.

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
completion. It uses full document synchronization.

```sh
ctest --test-dir build/tellc -R '^language_server$' --output-on-failure
```
