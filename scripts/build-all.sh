#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(sed -n 's/^__version__ = "\([^"]*\)"/\1/p' "$ROOT/tell/__init__.py")"
if [[ -z "$VERSION" ]]; then
  echo "Could not determine Tell version" >&2
  exit 1
fi

mkdir -p "$ROOT/dist"
rm -f "$ROOT"/dist/tell-* "$ROOT"/dist/SHA256SUMS
cd "$ROOT/native"

targets=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
  "windows arm64"
)

for target in "${targets[@]}"; do
  read -r goos goarch <<< "$target"
  suffix=""
  [[ "$goos" == "windows" ]] && suffix=".exe"
  output="$ROOT/dist/tell-${goos}-${goarch}${suffix}"
  echo "Building Tell ${VERSION} for ${goos}/${goarch}..."
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o "$output" ./cmd/tell
 done

cat > "$ROOT/dist/README.txt" <<README
Tell ${VERSION} native binaries

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
README

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$ROOT/dist" && sha256sum tell-* > SHA256SUMS)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$ROOT/dist" && shasum -a 256 tell-* > SHA256SUMS)
fi

echo "Native binaries created in $ROOT/dist"
