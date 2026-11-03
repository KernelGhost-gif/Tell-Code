Tell 0.3.6 native binaries

These executables run Tell programs without a separate Go installation. Python is only needed when a program explicitly imports a Python module. The optional desktop IDE needs Python and Tkinter; start it with: python -m tell ide

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

Run Tell source tests:
  Linux/macOS: ./tell-linux-amd64 test
  Windows:     tell-windows-amd64.exe test

Software bill of materials:
  SBOM.spdx.json (SPDX 2.3)

Third-party notices:
  THIRD_PARTY_NOTICES.txt

The macOS and Windows binaries are cross-compiled from the source. Their actual
execution should be smoke-tested on the corresponding operating systems.
