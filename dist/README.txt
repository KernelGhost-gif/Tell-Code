Tell 0.2.0 native binaries

These executables run Tell programs without Python or a separate Go installation.

Choose the binary matching your operating system and CPU:
  tell-linux-amd64       Linux x86-64
  tell-linux-arm64       Linux ARM64
  tell-darwin-amd64      macOS Intel
  tell-darwin-arm64      macOS Apple Silicon
  tell-windows-amd64.exe Windows x86-64
  tell-windows-arm64.exe Windows ARM64

Usage:
  Linux/macOS: ./tell-linux-amd64 program.tell
  Windows:     tell-windows-amd64.exe program.tell

Check a source file without running it:
  Linux/macOS: ./tell-linux-amd64 --check program.tell
  Windows:     tell-windows-amd64.exe --check program.tell

Print the runtime version:
  Linux/macOS: ./tell-linux-amd64 --version
  Windows:     tell-windows-amd64.exe --version

The macOS and Windows binaries are cross-compiled from the source. Their actual
execution should be smoke-tested on the corresponding operating systems.
