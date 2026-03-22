# Native Desktop App

**Date:** 2026-02-21

## Goal

Make wallfacer behave like a proper desktop application — launch from dock/taskbar, no terminal required, OS-native window — while keeping the existing Go server and browser-based UI architecture intact.

## Core Constraints

Regardless of packaging approach, the desktop app retains these hard runtime dependencies:

1. **Container runtime** — user must have Docker Desktop or Podman installed
2. **Git** on the host — worktrees, rebase, merge all run locally
3. **Workspace directories** on the local filesystem

These are fundamental to the architecture, not packaging details.

---

## Option 1: System Tray Wrapper (minimal changes)

The binary already calls `openBrowser()` on startup. The gap is it requires a terminal to keep running. A system tray wrapper makes it behave like a proper desktop app: click icon → server starts → browser opens → tray menu appears.

**Tray menu actions:** Open Dashboard, Quit

**What needs to change:**
- Add a systray library (e.g. `github.com/getlantern/systray` or `fyne.io/systray`)
- Move server startup out of the terminal into a background goroutine
- Supply a tray icon (PNG/ICO, embedded with `//go:embed`)
- Fix Windows: `openBrowser()` currently does nothing on `windows` — add `exec.Command("cmd", "/c", "start", url)`
- macOS: build as `.app` bundle with `Info.plist` and an icon

**Integration sketch:**
```go
func main() {
    systray.Run(func() {
        systray.SetIcon(iconData)
        systray.SetTitle("Wallfacer")
        mOpen := systray.AddMenuItem("Open Dashboard", "")
        mQuit := systray.AddMenuItem("Quit", "")

        go runServer(configDir, os.Args[1:])  // existing logic

        go func() {
            for {
                select {
                case <-mOpen.ClickedCh:
                    openBrowser("http://localhost:8080")
                case <-mQuit.ClickedCh:
                    systray.Quit()
                    os.Exit(0)
                }
            }
        }()
    }, func() {})
}
```

**Effort:** Low — a few hundred lines of new code, no architectural changes. Existing server, runner, store untouched.

**Limitation:** The UI is still in a browser window; the "app" is just the background server with a tray icon.

---

## Option 2: Wails — True Native App (best UX)

[Wails](https://wails.io) packages a Go backend + web frontend into a native desktop binary using the OS's native WebView (WKWebView on macOS, WebView2 on Windows, WebKitGTK on Linux). No Electron, no bundled Chromium, small binary.

**Why this fits well:**
- Backend is already Go — no rewrite
- Frontend is already vanilla HTML/JS — Wails renders it in a WebView
- Existing HTTP handlers stay as-is; the WebView connects to `localhost:8080`
- Output: `Wallfacer.app` on macOS, `Wallfacer.exe` on Windows, binary on Linux

**What needs to change:**

| Area | Change |
|------|--------|
| `main.go` | Wrap `runServer` inside `wails.Run()` app lifecycle |
| Browser launch | Remove `openBrowser()` — Wails window replaces it |
| `net.Listen` | Keep port binding; Wails WebView points at it |
| Data dir default | Optionally use `wails.App.DataPath()` for OS path conventions |
| First-run setup | Wails dialogs for token entry (optional quality-of-life) |
| Build toolchain | `wails build` replaces `go build` |

**Wails does NOT replace:**
- Container runtime — user still needs Docker Desktop or Podman installed
- Git — still required on the host

**Wails app skeleton:**
```go
// main.go
func main() {
    app := NewApp()  // wraps runServer
    err := wails.Run(&options.App{
        Title:     "Wallfacer",
        Width:     1400,
        Height:    900,
        AssetServer: &assetserver.Options{
            Assets: embeddedAssets,  // ui/ directory
        },
        OnStartup: app.startup,     // calls runServer
        Bind:      []interface{}{app},
    })
}
```

**Effort:** Medium. The architectural fit is very good — the main work is the Wails integration layer and packaging (icons, code signing for distribution).

---

## Option 3: Electron (not recommended)

Would run the Go binary as a child process. Adds ~150 MB for bundled Chromium, introduces a Node.js runtime, more complex build pipeline. No meaningful capability gain over Wails for this use case.

---

## Decision Matrix

| Approach | Effort | UX | Cross-platform | Binary size |
|----------|--------|----|----------------|-------------|
| **System tray** | Low | Browser tab + tray icon | macOS / Linux / Win | Same as today |
| **Wails native** | Medium | Native window, dock icon | macOS / Linux / Win | Small (no bundled browser) |
| **Electron** | Medium-High | Native window | macOS / Linux / Win | +150 MB (bundled Chromium) |

---

## Recommendation

**Wails (Option 2)** gives the best end-user experience — no browser tab, proper window, dock icon, OS-native feel. The existing Go + vanilla JS architecture is a near-perfect fit.

**System tray (Option 1)** is a lower-risk stepping stone that can ship first with minimal code changes, then be superseded by the Wails build later.
