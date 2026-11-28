# Package management

Tell's native CLI uses [Luna](https://github.com/Neonwave175/Luna) as its package build/install backend. Tell adds an embedded, curated catalogue with recipes for common developer tools across multiple language ecosystems. It does not bundle Luna or replace its package engine.

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
tell pkg search python
tell pkg search typescript
tell pkg search kotlin
tell pkg search r
tell pkg search cargo:serde
tell pkg search npm:eslint
tell pkg search nuget:json
tell pkg info ruff
tell pkg install rg fd bat hyperfine tokei
tell pkg install jq btop fastfetch
tell pkg install gofumpt staticcheck shfmt
# Install directly from native language package registries
tell pkg install cargo:cargo-audit
tell pkg install npm:@biomejs/biome npm:eslint
tell pkg install pip:mypy pip:pylint
tell pkg install go:golang.org/x/tools/gopls
tell pkg install gem:solargraph
tell pkg install composer:phpstan/phpstan
tell pkg install nuget:dotnet-format
tell pkg install pub:very_good_cli
tell pkg install cabal:hlint
tell pkg install mix:phoenix_new
tell pkg install coursier:scalafmt
 tell pkg install julia:JSON3 r:styler opam:ocamlformat
tell pkg install mint:realm/SwiftLint@0.60.0 luarocks:luafilesystem
tell pkg list
tell pkg update
tell pkg remove rg
```

The 29-entry starter catalogue includes utilities and formatters/linters for Rust, C, C++, C#, Go, Python, JavaScript, TypeScript, Java, Kotlin, PHP, Ruby, Haskell, Dart, Lua, Swift, R, shell scripts, SQL, JSON, YAML, TOML, Markdown, HTML, XML, and CSS.

| Package | Languages / purpose |
|---|---|
| `bat`, `fd`, `hyperfine`, `rg`, `tokei` | Rust-built terminal and code tools |
| `btop`, `cmatrix`, `fastfetch`, `jq`, `neovim` | C/C++ system, terminal, JSON, and editor tools |
| `gofumpt`, `staticcheck` | Go formatting and static analysis |
| `shfmt`, `shellcheck` | Shell formatting and static analysis |
| `ruff`, `black` | Python linting and formatting |
| `prettier`, `dprint` | JavaScript, TypeScript, and common web/data formats |
| `google-java-format` | Java formatting |
| `ktlint` | Kotlin linting and formatting |
| `csharpier` | C# and XML formatting |
| `air` | R formatting and language server |
| `php-cs-fixer` | PHP formatting |
| `rubocop` | Ruby linting and formatting |
| `hlint` | Haskell analysis |
| `dart-format` | Dart formatting |
| `stylua` | Lua and Luau formatting |
| `swift-format` | Swift formatting |

Run `tell pkg search PROVIDER:QUERY` to search supported native registries without relying on the curated list. Full-text search is available for Cargo, npm, Composer, RubyGems, NuGet, Cabal, Coursier, OPAM, Hex/Mix, CRAN, and LuaRocks. Go and Python currently perform exact module/package index lookups; Julia and Mint have install support but no direct CLI search command in this adapter.

Native registry installs are tracked separately from Luna. Supported prefixes are `cargo:`, `npm:`, `pip:` (using uv or pipx), `go:`, `gem:`, `composer:`, `nuget:`, `pub:`, `cabal:`, `mix:`, `coursier:`, `julia:`, `r:`, `opam:`, `mint:`, and `luarocks:`. These prefixes use the matching language ecosystem manager. `cargo:`, `npm:`, `pip:`, `go:`, `gem:`, `composer:`, `nuget:`, `pub:`, `cabal:`, `mix:`, `coursier:`, `mint:`, and `luarocks:` are intended for CLI tools and apps. `julia:` and `r:` add libraries to the active Julia/R environment; `luarocks:` installs Lua rocks into a Tell-local tree; `opam:` installs into the active OPAM switch. These global/environment installs are not substitutes for project dependency declarations: use the ecosystem-native project workflow when adding a library dependency to an application.

Use `tell pkg info NAME` to see the exact toolchain required by a curated package. Recipes currently target Linux. Windows, macOS, Android, and Termux package installation are not implied by the existence of Tell binaries for those platforms. Some upstream tools may build on other operating systems, but those recipes have not been validated there.

The first `tell pkg install NAME` automatically registers a bundled catalogue recipe with Luna, then asks Luna to build and install it. `tell pkg add NAME` only registers the recipe. If a recipe with that name already exists in `~/.local/lunaorigin`, Tell keeps it rather than overwriting your custom configuration. Packages not in Tell's catalogue are still passed directly to Luna, so existing Luna packages and custom recipes continue to work.

After a native registry install, add Tell's native binary directories to your shell path. Mint links Swift tools to `~/.local/tell/bin`; LuaRocks uses `~/.local/tell/luarocks/bin`. Luna installs continue to use Luna's binary directory:

```sh
export PATH="$HOME/.local/tell/bin:$HOME/.local/tell/pub-cache/bin:$HOME/.local/tell/luarocks/bin:$HOME/.local/luna:$PATH"
```

Examples:

```sh
rg --version
ruff --version
black --version
prettier --version
staticcheck -version
swift-format --version
```

Some tools have additional runtime behavior. For example, dprint requires a `dprint.json` file and configured plugins for the specific formats you use. Java, Kotlin, C#, R, PHP, Ruby, Haskell, Swift, and Dart entries require their named runtime/build toolchains to be installed before package installation.

## Validation status

Each curated entry exposes a validation label through `tell pkg info NAME`. Native registry installs use the package manager's own release and dependency metadata. The initial Rust starter set (`bat`, `fd`, `hyperfine`, `rg`, and `tokei`) has passed real build, execution, listing, and removal checks on Ubuntu Linux. The additional 24 catalogue recipes are included with explicit toolchain requirements and remain marked as awaiting CI validation until the package integration workflow has exercised them.

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

Tell registers catalogue recipes only after an explicit `tell pkg add` or `tell pkg install` command. It does not run package hooks during `tell run`, `tell build`, or project creation. The embedded recipes follow upstream default branches and are not pinned to immutable commits; review recipe changes and source updates before installing in sensitive environments.

## Integration validation

The [Luna package integration workflow](../.github/workflows/luna-integration.yml) builds the native CLI and executes the validated starter subset through Luna. The initial Tokei package test passed on 2026-11-28 with Tokei 15.0.0; the expanded workflow verifies additional language toolchains as those jobs are enabled.
