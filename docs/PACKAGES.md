# Package management

Tell's native CLI uses [Luna](https://github.com/Neonwave175/Luna) as its package build/install backend. Tell adds a small curated catalogue on top; it does not bundle Luna or replace its package engine.

## Install Luna

Follow the [upstream Luna installation instructions](https://github.com/Neonwave175/Luna). Luna currently requires Zsh, Python 3.10+, pip, wget, Git, Rich, and each package's own build toolchain. The Tell package adapter expects the `luna` executable on `PATH`.

Verify it:

```sh
luna --help
```

Luna is optional. Tell's compiler, interpreter, bytecode VM, and regular project commands continue to work without it.

## Browse and install packages

The catalogue is embedded in the Tell CLI binary, so searching it does not require a network request or a running Luna installation.

```sh
tell pkg search
tell pkg search text
tell pkg info rg
tell pkg install rg
tell pkg install fd bat hyperfine tokei
tell pkg list
tell pkg update
tell pkg remove rg
```

| Catalogue name | Utility | Installed command |
|---|---|---|
| `bat` | Syntax-highlighted `cat` replacement | `bat` |
| `fd` | Fast alternative to `find` | `fd` |
| `hyperfine` | Command-line benchmark runner | `hyperfine` |
| `rg` | ripgrep recursive text search | `rg` |
| `tokei` | Source-code line counter | `tokei` |

All five starter recipes build from upstream Git repositories with `cargo build --release --locked`. They currently target Linux and require Git plus the Rust stable toolchain (Cargo). The package integration workflow validates actual installs on Ubuntu Linux; other operating systems and Android/Termux are not yet CI-validated.

The first `tell pkg install NAME` automatically registers a bundled catalogue recipe with Luna, then asks Luna to build and install it. `tell pkg add NAME` only registers the recipe. If a recipe with that name already exists in `~/.local/lunaorigin`, Tell keeps it rather than overwriting your custom configuration. Packages not in Tell's catalogue are still passed directly to Luna, so existing Luna packages and custom recipes continue to work.

After installation, add Luna's binary directory to your shell path:

```sh
export PATH="$HOME/.local/luna:$PATH"
```

For example:

```sh
rg --version
fd --version
bat --version
hyperfine --version
tokei --version
```

## Lunafile format

A generated Tell project contains an empty `Lunafile`:

```json
[]
```

Luna expects a JSON array. Each entry identifies a package and its source repository, with optional configure, compile, and install commands. This is the Tokei recipe used by the integration workflow:

```json
[
  {
    "Name": "tokei",
    "URL": "https://github.com/XAMPPRocky/tokei.git",
    "Config": "",
    "Compile": "cargo build --release --locked",
    "Install": "mkdir -p ~/.local/luna && cp target/release/tokei ~/.local/luna/tokei"
  }
]
```

Save this as `Lunafile` and run `tell pkg sync` from the project root. The `Name` and `URL` fields are required. The other command fields may be empty when the package does not need that phase. The manifest format and package behavior remain owned by Luna.

## Security and updates

**Only install recipes and sync Lunafiles whose sources and commands you trust.** Luna clones source repositories and executes configure, compile, and install commands through a shell with the current user's permissions. A malicious or compromised recipe can therefore run arbitrary commands as that user.

Tell registers catalogue recipes only after an explicit `tell pkg add` or `tell pkg install` command. It does not run package hooks during `tell run`, `tell build`, or project creation. The embedded starter recipes follow upstream default branches and are not pinned to immutable commits; review recipe changes and source updates before installing in sensitive environments.

## Integration validation

The [Luna package integration workflow](../.github/workflows/luna-integration.yml) uses Tell's embedded catalogue to register, compile, execute, list, and remove every starter package on Ubuntu Linux.
