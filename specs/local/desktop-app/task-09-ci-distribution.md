# Task 9: CI Builds and Release Distribution

**Status:** Done
**Depends on:** Task 8
**Phase:** Distribution
**Effort:** Large

## Goal

Extend the existing release pipeline so tagged releases automatically build and publish desktop app binaries alongside the CLI binaries, with code signing for macOS and Windows.

## What to do

### 1. New GitHub Actions workflow

Create `.github/workflows/release-desktop.yml` triggered on `v*` tags (same as `release-binary.yml`):

**Matrix:**

| Runner | Platform | Output |
|--------|----------|--------|
| `macos-latest` | darwin/universal | `Wallfacer.app` (zipped) |
| `windows-latest` | windows/amd64 | `Wallfacer-Setup.exe` or `Wallfacer.exe` |
| `ubuntu-latest` | linux/amd64 | `Wallfacer-linux-amd64` (plain binary) |

**Steps per platform:**

1. Checkout code
2. Set up Go (from `go.mod`)
3. Install Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)
4. Install platform dependencies:
   - **macOS:** Xcode command line tools (pre-installed on `macos-latest`)
   - **Windows:** WebView2 runtime (pre-installed on `windows-latest`), NSIS for installer (optional)
   - **Linux:** `libgtk-3-dev`, `libwebkit2gtk-4.0-dev` (for WebKitGTK)
5. Run `wails build -tags desktop -ldflags "-s -w -X changkun.de/x/wallfacer/internal/cli.Version=${VERSION}"`
6. Platform-specific post-processing (see below)
7. Upload artifacts to the GitHub Release via `softprops/action-gh-release@v2`

### 2. macOS code signing and notarization

In the macOS matrix job:

1. Import the Developer ID certificate from a GitHub Actions secret (`APPLE_CERTIFICATE_P12`, `APPLE_CERTIFICATE_PASSWORD`)
2. After `wails build`, sign the `.app` bundle:
   ```
   codesign --deep --force --options runtime \
     --sign "Developer ID Application: <TEAM>" \
     build/bin/Wallfacer.app
   ```
3. Create a zip for notarization:
   ```
   ditto -c -k --keepParent build/bin/Wallfacer.app Wallfacer-darwin-universal.zip
   ```
4. Submit for notarization via `xcrun notarytool`:
   ```
   xcrun notarytool submit Wallfacer-darwin-universal.zip \
     --apple-id "$APPLE_ID" --password "$APPLE_APP_PASSWORD" --team-id "$APPLE_TEAM_ID" \
     --wait
   ```
5. Staple the notarization ticket:
   ```
   xcrun stapler staple build/bin/Wallfacer.app
   ```
6. Re-zip the stapled app for upload

**Required secrets:**
- `APPLE_CERTIFICATE_P12` — base64-encoded .p12 Developer ID certificate
- `APPLE_CERTIFICATE_PASSWORD` — certificate password
- `APPLE_ID` — Apple ID email
- `APPLE_APP_PASSWORD` — app-specific password for notarytool
- `APPLE_TEAM_ID` — 10-character team identifier

### 3. Windows code signing

In the Windows matrix job:

1. Import the code signing certificate from secrets (`WINDOWS_CERTIFICATE_P12`, `WINDOWS_CERTIFICATE_PASSWORD`)
2. After `wails build`, sign the `.exe`:
   ```powershell
   signtool sign /f cert.p12 /p "$env:CERT_PASSWORD" /tr http://timestamp.digicert.com /td sha256 /fd sha256 build\bin\Wallfacer.exe
   ```
3. Rename to `Wallfacer-windows-amd64.exe` for upload

**Required secrets:**
- `WINDOWS_CERTIFICATE_P12` — base64-encoded .p12 Authenticode certificate
- `WINDOWS_CERTIFICATE_PASSWORD` — certificate password

### 4. Linux packaging

In the Linux matrix job:

1. After `wails build`, rename binary to `Wallfacer-linux-amd64`
2. No code signing needed for Linux
3. Upload the plain binary

### 5. Makefile targets

Add convenience targets for local desktop builds (already in Task 8). Add a `release` target note that desktop builds are CI-only (Wails + platform SDKs required).

### 6. Release notes integration

Update `scripts/release-notes.sh` to mention desktop downloads in the generated release notes template — add a "Desktop App" section listing the three platform downloads with brief install instructions:
- **macOS:** Download `.zip`, extract, drag to Applications
- **Windows:** Download `.exe`, run (may need to allow in SmartScreen on first launch)
- **Linux:** Download binary, `chmod +x`, run (requires WebKitGTK)

### 7. Skip conditions

The workflow should gracefully skip code signing steps when secrets are not configured (e.g., in forks). Use `if: secrets.APPLE_CERTIFICATE_P12 != ''` guards so unsigned builds still produce artifacts.

## Tests

- **Workflow syntax validation:** `actionlint .github/workflows/release-desktop.yml` passes
- **Skip-secret guards:** Verify the workflow doesn't fail when signing secrets are absent (produces unsigned but functional binaries)
- **Release asset naming:** Verify output filenames match the pattern used by `release-binary.yml` (`Wallfacer-<os>-<arch>.<ext>`)
- **Manual dry run:** Push a test tag to a fork, verify all three platform jobs complete and upload artifacts

## Boundaries

- Do NOT modify `release-binary.yml` — CLI and desktop releases are separate workflows
- Do NOT add auto-update (Sparkle, WinSparkle, etc.) — that's a future feature
- Do NOT add Homebrew cask, Scoop manifest, or Linux package repo publishing yet
- Do NOT add DMG or MSI installer generation in the first pass (zip/exe is sufficient)
