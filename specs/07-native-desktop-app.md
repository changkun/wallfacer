# M7: Native Desktop App

**Status:** Not started | **Date:** 2026-02-21

## Goal

Make wallfacer behave like a proper desktop application — launch from dock/taskbar, no terminal required, OS-native window — while keeping the existing Go server and browser-based UI architecture intact.

## Core Constraints

The desktop app retains these hard runtime dependencies:

1. **Container runtime** — user must have Docker Desktop or Podman installed
2. **Git** on the host — worktrees, rebase, merge all run locally
3. **Workspace directories** on the local filesystem

These are fundamental to the architecture, not packaging details.

---

## Current State

The application is a pure CLI tool (`wallfacer run`) that starts an HTTP server on `:8080` and opens the system browser via `openBrowser()` in `internal/cli/cli.go:159`. The UI is a server-rendered HTML + vanilla JS frontend embedded via `//go:embed ui` in `main.go`. There is no native window, system tray, or desktop packaging.

Key entry points:
- `main.go` — CLI dispatch to `run`, `exec`, `status`, `doctor` subcommands
- `internal/cli/server.go:RunServer()` — HTTP server lifecycle (listen, serve, graceful shutdown)
- `internal/cli/cli.go:openBrowser()` — cross-platform browser launcher (macOS: `open`, Windows: `cmd /c start`, Linux: `xdg-open`)

The Windows browser fallback mentioned in the original spec already exists in `openBrowser()`.

---

## Approach: Wails Native App

[Wails](https://wails.io) packages a Go backend + web frontend into a native desktop binary using the OS's native WebView (WKWebView on macOS, WebView2 on Windows, WebKitGTK on Linux). No Electron, no bundled Chromium, small binary.

**Why this fits well:**
- Backend is already Go — no rewrite
- Frontend is already vanilla HTML/JS — Wails renders it in a WebView
- Existing HTTP handlers stay as-is; the WebView connects to `localhost:8080`
- Output: `Wallfacer.app` on macOS, `Wallfacer.exe` on Windows, binary on Linux

**What needs to change:**

| Area | Change |
|------|--------|
| `main.go` | Add `desktop` subcommand that wraps `RunServer` inside `wails.Run()` app lifecycle |
| Browser launch | Desktop mode skips `openBrowser()` — Wails window replaces it |
| `net.Listen` | Keep port binding; Wails WebView points at it |
| First-run setup | Wails dialogs for token entry (optional quality-of-life) |
| Build toolchain | `wails build` for desktop binary; `go build` unchanged for CLI mode |
| System tray | Add tray icon with "Open Dashboard" and "Quit" menu items via Wails tray support |
| macOS | Build as `.app` bundle with `Info.plist` and icon |
| Windows | Build as `.exe` with version info and icon |

**Wails app skeleton:**
```go
// internal/cli/desktop.go
func RunDesktop() {
    app := NewDesktopApp()  // wraps RunServer
    err := wails.Run(&options.App{
        Title:     "Wallfacer",
        Width:     1400,
        Height:    900,
        AssetServer: &assetserver.Options{
            Assets: uiFiles,  // existing embed.FS from main.go
        },
        OnStartup: app.startup,     // calls RunServer
        Bind:      []interface{}{app},
    })
}
```

**Effort:** Medium. The architectural fit is very good — the main work is the Wails integration layer and packaging (icons, code signing for distribution).

---

## Rejected Alternative: Electron

Would run the Go binary as a child process. Adds ~150 MB for bundled Chromium, introduces a Node.js runtime, more complex build pipeline. No meaningful capability gain over Wails for this use case.
