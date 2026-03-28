# Task 8: App Icons and Build Packaging

**Status:** Todo
**Depends on:** Task 2, Task 7
**Phase:** Packaging
**Effort:** Medium

## Goal

Create app icon assets and Makefile targets to produce distributable binaries: `Wallfacer.app` on macOS, `Wallfacer.exe` on Windows, and a desktop-ready binary on Linux.

## What to do

### Icons
1. Create the app icon in multiple sizes at `assets/icons/`:
   - `appicon.png` (1024x1024) — master icon
   - `appicon.icns` — macOS app icon (generated from master via `iconutil` or equivalent)
   - `appicon.ico` — Windows app icon (16, 32, 48, 256 sizes)
   - Use the wallfacer brick motif (placeholder geometric icon is fine for now)
2. Embed icon assets via Wails build configuration

### macOS Packaging
3. Create `build/darwin/Info.plist` with:
   - `CFBundleName: Wallfacer`
   - `CFBundleIdentifier: de.changkun.wallfacer`
   - `CFBundleVersion` and `CFBundleShortVersionString` from build flags
   - `LSUIElement: false` (show in dock)
4. Wails build produces `.app` bundle automatically when targeting macOS

### Windows Packaging
5. Create `build/windows/wails.exe.manifest` for Windows version info
6. Create `build/windows/info.json` with version metadata for Wails

### Makefile Targets
7. Add targets to `Makefile`:
   ```makefile
   build-desktop:       ## Build native desktop app for current platform
       wails build -tags desktop

   build-desktop-darwin:  ## Build macOS .app bundle
       wails build -tags desktop -platform darwin/universal

   build-desktop-windows: ## Build Windows .exe
       wails build -tags desktop -platform windows/amd64

   build-desktop-linux:   ## Build Linux binary
       wails build -tags desktop -platform linux/amd64
   ```
8. Ensure `make build-binary` (the CLI build) is unchanged and does NOT require Wails CLI

### Wails Config
9. Create `wails.json` at the project root with:
   - `name: "Wallfacer"`
   - `outputfilename: "Wallfacer"`
   - `frontend:install` and `frontend:build` commands (may be no-ops since UI is already built)
   - `author` metadata

## Tests

- `TestMakefileBuildBinary`: Verify `make build-binary` still works without Wails CLI installed
- Verify icon files exist and have correct dimensions (can be a simple file-existence check)
- Manual: `make build-desktop` produces a runnable binary/app bundle on the current platform

## Boundaries

- Do NOT add code signing, notarization, or distribution automation (DMG, MSI, etc.)
- Do NOT add auto-update functionality
- Do NOT modify CI pipelines — that can be a follow-up
- Placeholder icons are acceptable; final design is a separate concern
