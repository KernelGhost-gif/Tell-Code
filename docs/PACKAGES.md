# Package management

Tell's native CLI can delegate package operations to [Luna](https://github.com/Neonwave175/Luna), a separate package manager maintained by Neonwave175. Tell does not bundle or reimplement Luna. The adapter invokes the `luna` executable and returns its exit status.

## Install Luna

Install Luna by following its [upstream instructions](https://github.com/Neonwave175/Luna). The upstream project currently requires Python 3.10 or newer, Zsh, pip, wget, Git, Rich, and the build tools needed by the packages you install. Make sure the resulting `luna` command is on your `PATH`.

Verify that the command is available:

```sh
luna --help
```

Luna is optional. The Tell runtime, interpreter, bytecode VM, and compiler continue to work without it.

## Commands

```sh
tell pkg install fmt
tell pkg install fmt ripgrep
tell pkg remove fmt
tell pkg list
tell pkg update
tell pkg sync
tell pkg sync path/to/Lunafile
```

| Tell command | Luna command |
|---|---|
| `tell pkg install A B` | `luna install A B` |
| `tell pkg remove A` | `luna remove A` |
| `tell pkg list` | `luna list` |
| `tell pkg update` | `luna update` |
| `tell pkg sync [file]` | `luna sync file` |

When no manifest path is supplied, `tell pkg sync` uses `Lunafile` in the current working directory. Run it from the Tell project root. The adapter does not automatically install dependencies when running or building a Tell program.

## Lunafile format

A generated Tell project contains an empty `Lunafile`:

```json
[]
```

Luna expects a JSON array. Each entry identifies a package and its source repository, with optional configure, compile, and install commands. This real package recipe was verified in GitHub Actions against upstream Tokei 15.0.0:

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

Save this as `Lunafile` and run:

```sh
tell pkg sync
~/.local/luna/tokei --version
```

This recipe needs Git and the Rust toolchain (`cargo`). The installed binary is placed in `~/.local/luna`; add that directory to `PATH` or use its full path as shown. The test recipe follows the upstream project's current default branch, so future source changes may affect build compatibility.

The `Name` and `URL` fields are required. The other command fields may be empty when the package does not need that phase. The exact recipe schema and package behavior are owned by Luna; consult its upstream README before authoring a manifest.

## Security

**Only sync a Lunafile whose package sources and commands you trust.** Luna's current package implementation clones the source and executes the manifest's configure, compile, and install commands through a shell. Those commands run with the permissions of the current user. A malicious or compromised recipe can therefore modify files, execute programs, or access user data.

Tell invokes Luna only after an explicit `tell pkg` command. It does not run package hooks as a side effect of `tell run`, `tell build`, or project creation. The adapter passes arguments directly to the Luna process and does not interpolate them through a shell.


## Verified integration test

The [Luna package integration workflow](../.github/workflows/luna-integration.yml) installs the real Tokei source through `tell pkg sync`, verifies the installed executable, exercises `tell pkg install` and `tell pkg list`, and removes the package with `tell pkg remove`. It passed on 2026-11-28 with Tokei 15.0.0.

## Compatibility

The integration requires the external `luna` executable to be discoverable through `PATH`. Runtime and platform requirements, package registries, manifest semantics, and supported build tools remain under Luna's control. Existing `tell.toml` project metadata is unchanged; package declarations live in Luna's `Lunafile`.
