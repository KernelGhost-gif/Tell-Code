#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(sed -n 's/^__version__ = "\([^"]*\)"/\1/p' "$ROOT/tell/__init__.py")"
if [[ -z "$VERSION" ]]; then
  echo "Could not determine Tell version" >&2
  exit 1
fi

mkdir -p "$ROOT/releases"
STAGE="$(mktemp -d "$ROOT/releases/.tell-build.XXXXXX")"
trap 'rm -rf "$STAGE"' EXIT
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
  output="$STAGE/tell-${goos}-${goarch}${suffix}"
  echo "Building Tell ${VERSION} for ${goos}/${goarch}..."
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o "$output" ./cmd/tell
done

cat > "$STAGE/README.txt" <<README
Tell ${VERSION} native binaries

These executables run Tell programs without a separate Go installation. Python is only needed when a program explicitly imports a Python module.

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
README

python3 "$ROOT/scripts/generate-sbom.py" \
  --version "$VERSION" \
  --directory "$STAGE" \
  --native-dir "$ROOT/native"
cp "$ROOT/THIRD_PARTY_NOTICES.txt" "$STAGE/THIRD_PARTY_NOTICES.txt"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$STAGE" && sha256sum tell-* > SHA256SUMS)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$STAGE" && shasum -a 256 tell-* > SHA256SUMS)
fi

for artifact in "$STAGE"/*; do
  mv -f "$artifact" "$ROOT/releases/"
done

echo "Native binaries created in $ROOT/releases"
