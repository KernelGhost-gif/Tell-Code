#!/usr/bin/env bash
set -euo pipefail
if [[ "$#" -ne 4 ]]; then echo "usage: package-linux.sh VERSION ARCH BINARY OUTPUT_DIR" >&2; exit 2; fi
VERSION="$(printf '%s' "$1" | sed 's/^v//')"; ARCH="$2"; BINARY="$3"; OUT="$4"
case "$ARCH" in amd64) DEB_ARCH=amd64; RPM_ARCH=x86_64 ;; arm64) DEB_ARCH=arm64; RPM_ARCH=aarch64 ;; *) echo "unsupported architecture: $ARCH" >&2; exit 2 ;; esac
[[ -f "$BINARY" ]] || { echo "binary not found: $BINARY" >&2; exit 2; }
command -v dpkg-deb >/dev/null || { echo "dpkg-deb is required" >&2; exit 2; }
command -v rpmbuild >/dev/null || { echo "rpmbuild is required (install rpm)" >&2; exit 2; }
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
mkdir -p "$OUT" "$TMP/deb/DEBIAN" "$TMP/deb/usr/bin" "$TMP/deb/usr/share/doc/tell" "$TMP/rpmbuild/SOURCES" "$TMP/rpmbuild/SPECS"
install -m 755 "$BINARY" "$TMP/deb/usr/bin/tell"
install -m 644 "$ROOT/LICENSE" "$TMP/deb/usr/share/doc/tell/copyright"
cat > "$TMP/deb/DEBIAN/control" <<CONTROL
Package: tell-language
Version: $VERSION
Section: devel
Priority: optional
Architecture: $DEB_ARCH
Maintainer: Tell Language contributors
Description: Tell sentence-first programming language runtime
 Standalone native runtime and bytecode toolchain for Tell programs.
CONTROL
dpkg-deb --build --root-owner-group "$TMP/deb" "$OUT/tell_$VERSION""_linux_$DEB_ARCH.deb"

cp "$BINARY" "$TMP/rpmbuild/SOURCES/tell"
cp "$ROOT/LICENSE" "$TMP/rpmbuild/SOURCES/LICENSE"
cat > "$TMP/rpmbuild/SPECS/tell.spec" <<SPEC
Name: tell-language
Version: $VERSION
Release: 1
Summary: Tell sentence-first programming language runtime
License: MIT
BuildArch: $RPM_ARCH
Source0: tell
Source1: LICENSE

%description
Standalone native runtime and bytecode toolchain for Tell programs.

%install
mkdir -p %{buildroot}%{_bindir} %{buildroot}%{_licensedir}/tell
install -m 0755 %{_sourcedir}/tell %{buildroot}%{_bindir}/tell
install -m 0644 %{_sourcedir}/LICENSE %{buildroot}%{_licensedir}/tell/LICENSE

%files
%{_bindir}/tell
%license %{_licensedir}/tell/LICENSE
SPEC
rpmbuild --target "$RPM_ARCH" --define "_topdir $TMP/rpmbuild" -bb "$TMP/rpmbuild/SPECS/tell.spec"
RPM_FILE="$(find "$TMP/rpmbuild/RPMS" -type f -name '*.rpm' -print -quit)"
[[ -n "$RPM_FILE" ]] || { echo "rpmbuild did not produce an RPM" >&2; exit 1; }
cp "$RPM_FILE" "$OUT/tell_$VERSION""_linux_$RPM_ARCH.rpm"

mkdir -p "$TMP/archive"
cp "$BINARY" "$TMP/archive/tell"
tar -C "$TMP/archive" -czf "$OUT/tell_$VERSION""_linux_$ARCH.tar.gz" tell
if [[ "$BINARY" != "$OUT/tell-linux-$ARCH" ]]; then cp "$BINARY" "$OUT/tell-linux-$ARCH"; fi
