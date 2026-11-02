# Tell Modules

Tell can import local `.tell` source files by a path relative to the importing file:

```tell
Use the module "helpers.tell" as helpers.
Say helpers.double(21).
```

The module runs once per interpreter session. Its public top-level bindings are available through the alias; names beginning with `_` stay private. Modules can import other local modules. Circular imports are rejected. The source interpreter caches modules by resolved file path.

## Bytecode builds

The native compiler resolves and compiles local `.tell` dependencies recursively, then embeds their bytecode in the `.tbc` artifact. The compiled artifact can run after the source module files are removed. At runtime, an imported module executes in its own environment with access to the importing program's globals, and repeated imports reuse its exports. The bytecode compiler rejects circular imports during compilation.

Imports use the same process permissions as the program. Tell does not sandbox a module or validate third-party code. There is no package registry, version resolver, lockfile, or remote dependency installation.
