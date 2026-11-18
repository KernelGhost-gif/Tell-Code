# TellC language server

The language server is a separate Python package for editors that support LSP.
It does not import or require the Tell Python runtime or Tk IDE. It requires
Python 3.10+ and a TellC compiler built from this checkout; rebuild `tellc`
after updating it so that `--check-stdin` is available. See
[TellC build instructions](../TellC/README.md).

Install this package independently:

```sh
python3 -m pip install ./lsp
```

Configure your editor to run `tell-lsp` over stdio for `.tell` files. The
server runs `tellc --check-stdin` on the unsaved buffer on each change. Put the
compiler on the editor's PATH or set `TELL_LSP_COMPILER` to its executable
path. An unavailable or incompatible checker is shown as an editor diagnostic.
The server never executes the program or writes a compiled output file.

For Neovim 0.11 or newer, add this to `init.lua`:

```lua
vim.filetype.add({ extension = { tell = 'tell' } })
vim.lsp.config.tell = {
  cmd = { 'tell-lsp' },
  -- Set this if tellc is not on Neovim's PATH:
  -- cmd_env = { TELL_LSP_COMPILER = '/absolute/path/to/tellc' },
  filetypes = { 'tell' },
  root_markers = { '.git' },
}
vim.lsp.enable('tell')
```

From a checkout without installing, run `python3 -m tell_lsp` with the `lsp`
directory on `PYTHONPATH`.

Diagnostics use the TellC compiler's parser and code generation checks, so
they support its typed functions, objects and access rules, and report language
features the current compiler cannot generate. The first compiler error is
reported per buffer. The server offers keyword completions; hover, navigation,
cross-file analysis, and incremental synchronization are not supported. It is
intended for TellC's typed dialect, not the older Python or Go runtime dialects.

Run the integration tests with a freshly built compiler:

```sh
TELL_LSP_COMPILER=/absolute/path/to/tellc python3 -m unittest discover -s lsp -v
```
