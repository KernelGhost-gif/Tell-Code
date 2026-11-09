#!/usr/bin/env bash
set -eo pipefail
if [[ "$#" -ne 2 ]]; then echo "usage: package-macos.sh VERSION OUTPUT_DIR" >&2; exit 2; fi
VERSION="$1"; OUT="$2"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$OUT"; TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
VERSION="$(printf '%s' "$VERSION" | sed 's/^v//')"
TELL_CODESIGN_IDENTITY="${TELL_CODESIGN_IDENTITY-}"
TELL_INSTALLER_IDENTITY="${TELL_INSTALLER_IDENTITY-}"
TELL_NOTARY_PROFILE="${TELL_NOTARY_PROFILE-}"
REQUIRE_MACOS_SIGNING="${REQUIRE_MACOS_SIGNING-1}"
(
  cd "$ROOT/native"
  CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$TMP/tell-amd64" ./cmd/tell
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$TMP/tell-arm64" ./cmd/tell
)
mkdir -p "$TMP/root/usr/local/bin"
lipo -create "$TMP/tell-amd64" "$TMP/tell-arm64" -output "$TMP/root/usr/local/bin/tell"
chmod 755 "$TMP/root/usr/local/bin/tell"
if [[ -z "$TELL_CODESIGN_IDENTITY" && "$REQUIRE_MACOS_SIGNING" == "1" ]]; then echo "TELL_CODESIGN_IDENTITY is required for release packaging." >&2; exit 2; fi
if [[ -n "$TELL_CODESIGN_IDENTITY" ]]; then codesign --force --options runtime --timestamp --sign "$TELL_CODESIGN_IDENTITY" "$TMP/root/usr/local/bin/tell"; fi
tar -C "$TMP/root/usr/local/bin" -czf "$OUT/tell_$VERSION""_darwin_universal.tar.gz" tell
PKG="$OUT/tell_$VERSION""_darwin_universal.pkg"
pkgbuild --root "$TMP/root" --identifier "dev.tell-lang.tell" --version "$VERSION" --install-location "/" "$TMP/unsigned.pkg"
if [[ -n "$TELL_INSTALLER_IDENTITY" ]]; then
  productsign --sign "$TELL_INSTALLER_IDENTITY" "$TMP/unsigned.pkg" "$PKG"
elif [[ "$REQUIRE_MACOS_SIGNING" == "1" ]]; then
  echo "TELL_INSTALLER_IDENTITY is required for release packaging." >&2; exit 2
else cp "$TMP/unsigned.pkg" "$PKG"; fi
if [[ -n "$TELL_NOTARY_PROFILE" ]]; then
  xcrun notarytool submit "$PKG" --keychain-profile "$TELL_NOTARY_PROFILE" --wait
  xcrun stapler staple "$PKG"
elif [[ "$REQUIRE_MACOS_SIGNING" == "1" ]]; then
  echo "TELL_NOTARY_PROFILE is required for release packaging." >&2; exit 2
fi
echo "Created universal macOS package in $OUT"
