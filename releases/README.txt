Tell 0.3.4 native binaries

These executables run Tell programs without Python or a separately installed Go toolchain.

Targets: Linux x64/ARM64, macOS Intel/Apple Silicon, and Windows x64/ARM64.

Run a program:
  Linux/macOS: ./tell-linux-amd64 program.tell
  Windows:     tell-windows-amd64.exe program.tell

Check syntax:
  Linux/macOS: ./tell-linux-amd64 --check program.tell
  Windows:     tell-windows-amd64.exe --check program.tell

Run source tests:
  Linux/macOS: ./tell-linux-amd64 test
  Windows:     tell-windows-amd64.exe test

Compile the supported native bytecode subset and execute it:
  Linux/macOS: ./tell-linux-amd64 build program.tell
               ./tell-linux-amd64 run program.tbc
  Windows:     tell-windows-amd64.exe build program.tell
               tell-windows-amd64.exe run program.tbc

The bytecode compiler supports only a subset of Tell; consult docs/CLI_REFERENCE.md.

The native runtime includes the allowlisted CSV library. Consult docs/STANDARD_LIBRARY.md for its API.

The runtimes include eager map, filter, reduce, and enumerate operations over lists. Tell functions support call-time defaults and a final variadic parameter; enumerate pairs can be destructured in For each loops. See docs/LANGUAGE_SPECIFICATION.md.

The CLI can create runnable starter projects with `tell new` and `tell init`. The generated manifest does not resolve dependencies.

Cross-compilation proves that a target binary was produced. Run target-specific smoke tests before claiming it was executed on that operating system.
