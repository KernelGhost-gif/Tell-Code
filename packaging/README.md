# Tell release and installer packaging

## Linux (amd64 and arm64)

Build a self-contained binary from the repository root:

    cd native
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=0.4.0" -o ../dist/tell-linux-amd64 ./cmd/tell
    cd ..

Create the tarball, Debian package, RPM package, and raw binary:

    bash scripts/package-linux.sh 0.4.0 amd64 dist/tell-linux-amd64 dist

For ARM64 replace amd64 with arm64 in the GOARCH and script arguments. Required packaging tools are dpkg-deb, rpmbuild, tar, and standard GNU utilities.

Install the latest Linux build (or a specific release):

    curl -fsSL https://raw.githubusercontent.com/KernelGhost-gif/Tell-Code/main/scripts/install.sh | bash
    curl -fsSL https://raw.githubusercontent.com/KernelGhost-gif/Tell-Code/main/scripts/install.sh | bash -s -- 0.4.0

The installer verifies the downloaded binary against release SHA256SUMS before writing to /usr/local/bin. It falls back to ~/.local/bin when /usr/local is not writable.

## Windows x64

Install Inno Setup 6, build tell-windows-amd64.exe, and run from PowerShell:

    ./scripts/package-windows.ps1 -Version 0.4.0 -BinaryPath build/tell-windows-amd64.exe -OutputDirectory dist

The installer places tell.exe under Program Files, offers a system PATH task, and registers an uninstaller. The uninstaller removes the Tell directory from PATH. System PATH changes require administrator privileges.

For public distribution, Authenticode-sign the final installer and executable with an organization-controlled code-signing certificate. The current pipeline does not assume a certificate is available in developer workstations.

## macOS universal package

Run on macOS with Xcode command-line tools installed:

    bash scripts/package-macos.sh 0.4.0 dist

The script builds amd64 and arm64 executables and joins them with lipo. Production release mode requires:
- TELL_CODESIGN_IDENTITY: Developer ID Application identity for the universal executable.
- TELL_INSTALLER_IDENTITY: Developer ID Installer identity for the package.
- TELL_NOTARY_PROFILE: Keychain profile created with xcrun notarytool store-credentials.

Use REQUIRE_MACOS_SIGNING=0 only for local, unsigned packaging experiments. Never publish an unsigned package as a production release.

Generate the Homebrew formula after the universal tarball is built:

    python scripts/generate-homebrew-formula.py --version 0.4.0 --archive dist/tell_0.4.0_darwin_universal.tar.gz --output dist/tell.rb

The formula pins the archive SHA-256. To distribute through a tap, commit the generated formula to that tap's Formula/tell.rb.

## GitHub Actions release

.github/workflows/release-modernized.yml runs only when a v* tag is pushed. Linux builds amd64 and arm64 binaries and packages; Windows builds the x64 binary and Inno Setup installer; macOS builds a universal binary and signed/notarized pkg plus Homebrew formula. The publish job downloads all artifacts, creates SHA256SUMS from final files, verifies checksums, and publishes the assets.

Configure these repository Actions secrets before tagging a production release:
- APPLE_APPLICATION_CERT_P12 and APPLE_APPLICATION_CERT_PASSWORD
- APPLE_INSTALLER_CERT_P12 and APPLE_INSTALLER_CERT_PASSWORD
- APPLE_KEYCHAIN_PASSWORD
- APPLE_ID, APPLE_TEAM_ID, and APPLE_APP_SPECIFIC_PASSWORD

The two P12 secrets contain base64-encoded Developer ID Application and Developer ID Installer certificates. The release workflow intentionally fails closed if signing or notarization prerequisites are absent.

The prior tag-triggered publisher is disabled on this branch to avoid two release workflows racing to publish different asset sets. Merge this branch before relying on the new tag pipeline.
