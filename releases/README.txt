Tell 0.3.0 native binaries

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

Cross-compilation proves that a target binary was produced. Run target-specific smoke tests before claiming it was executed on that operating system.
