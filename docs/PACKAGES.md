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

Luna expects a JSON array. Each entry identifies a package and its source repository, with optional configure, compile, and install commands:

```json
[
  {
    "Name": "example",
    "URL": "https://github.com/example/example.git",
    "Config": "",
    "Compile": "make",
    "Install": "make install"
  }
]
```

The `Name` and `URL` fields are required. The other command fields may be empty when the package does not need that phase. The exact recipe schema and package behavior are owned by Luna; consult its upstream README before authoring a manifest.

## Security

**Only sync a Lunafile whose package sources and commands you trust.** Luna's current package implementation clones the source and executes the manifest's configure, compile, and install commands through a shell. Those commands run with the permissions of the current user. A malicious or compromised recipe can therefore modify files, execute programs, or access user data.

Tell invokes Luna only after an explicit `tell pkg` command. It does not run package hooks as a side effect of `tell run`, `tell build`, or project creation. The adapter passes arguments directly to the Luna process and does not interpolate them through a shell.

## Compatibility

The integration requires the external `luna` executable to be discoverable through `PATH`. Runtime and platform requirements, package registries, manifest semantics, and supported build tools remain under Luna's control. Existing `tell.toml` project metadata is unchanged; package declarations live in Luna's `Lunafile`.
