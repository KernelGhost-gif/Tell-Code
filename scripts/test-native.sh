#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT/native"
go test ./...
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
go build -o "$tmp" ./cmd/tell
"$tmp" --version
"$tmp" --check "$ROOT/examples/features.tell"
"$tmp" "$ROOT/examples/hello.tell"
"$tmp" "$ROOT/examples/factorial.tell"
printf '7\n' | "$tmp" "$ROOT/examples/guessing_game.tell"
