#!/usr/bin/env bash
set -euo pipefail
REPOSITORY="KernelGhost-gif/Tell-Code"
VERSION="latest"
if [[ "$#" -ge 1 ]]; then VERSION="$1"; fi
if [[ "$(uname -s)" != "Linux" ]]; then echo "Tell bootstrap installer currently supports Linux only." >&2; exit 2; fi
case "$(uname -m)" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) echo "Unsupported CPU architecture: $(uname -m)" >&2; exit 2 ;; esac
ASSET="tell-linux-$ARCH"
if [[ "$VERSION" == "latest" ]]; then BASE="https://github.com/$REPOSITORY/releases/latest/download"
else VERSION="$(printf '%s' "$VERSION" | sed 's/^v//')"; BASE="https://github.com/$REPOSITORY/releases/download/v$VERSION"; fi
command -v curl >/dev/null || { echo "curl is required" >&2; exit 2; }
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
curl --fail --location --silent --show-error "$BASE/$ASSET" -o "$TMP/$ASSET"
curl --fail --location --silent --show-error "$BASE/SHA256SUMS" -o "$TMP/SHA256SUMS"
EXPECTED="$(awk -v name="$ASSET" '$2 == name { print $1 }' "$TMP/SHA256SUMS")"
[[ -n "$EXPECTED" ]] || { echo "No checksum entry for $ASSET" >&2; exit 1; }
printf '%s  %s\n' "$EXPECTED" "$TMP/$ASSET" | sha256sum --check --status || { echo "Checksum verification failed for $ASSET" >&2; exit 1; }
PREFIX="${TELL_INSTALL_PREFIX:-/usr/local}"
if [[ ! -w "$PREFIX" ]]; then PREFIX="$HOME/.local"; mkdir -p "$PREFIX/bin"; echo "No write access to /usr/local; installing to $PREFIX/bin" >&2; fi
mkdir -p "$PREFIX/bin"; install -m 0755 "$TMP/$ASSET" "$PREFIX/bin/tell"
echo "Tell installed at $PREFIX/bin/tell"
case ":$PATH:" in *":$PREFIX/bin:"*) ;; *) echo "Add $PREFIX/bin to PATH to invoke tell from any directory." ;; esac
