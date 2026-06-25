---
title: Platform-Specific Tray and Window Behaviors
status: archived
depends_on:
  - specs/local/desktop-app/task-03-tray-skeleton.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 7: Platform-Specific Tray and Window Behaviors

## Goal

Implement the platform-specific behaviors for macOS, Windows, and Linux as described in the spec's platform differences table.

## What to do

### macOS
1. Create macOS template icon assets (`tray-template.png`, `tray-template@2x.png`) — monochrome with alpha channel so macOS auto-adapts for light/dark menu bar
2. Configure Wails to use template images via `mac.Options` (set `NSImage.isTemplate = true` equivalent)
3. Window close hides to tray instead of quitting — use Wails `OnBeforeClose` hook to intercept and call `window.Hide()` instead
4. Cmd+Q should actually quit (trigger graceful shutdown)
5. Dock icon click should show/focus the window

### Windows
1. Create `.ico` icon file at `assets/icons/tray.ico` containing 16x16 and 32x32 variants
2. Window close minimizes to tray — same `OnBeforeClose` intercept as macOS
3. Show a balloon notification on first minimize: "Wallfacer is still running in the background" (show only once per session, track with a boolean flag)
4. Left-click on tray icon shows/focuses the main window (right-click opens menu)

### Linux
1. Use PNG icons (22x22 recommended) — already created in Task 3
2. Left-click opens the tray menu (same as right-click) — Linux convention
3. Document the GNOME limitation (AppIndicator extension required) in a comment and in the docs

### Shared
4. Create platform-specific files:
   - `internal/cli/tray_darwin.go` — macOS template icon setup, dock click handler
   - `internal/cli/tray_windows.go` — balloon notification, left-click behavior
   - `internal/cli/tray_linux.go` — GNOME note, icon path
5. Use build tags to select platform-specific code

## Tests

- `TestWindowCloseHidesToTray`: Verify `OnBeforeClose` returns false (preventing actual close) and that `window.Hide()` is called
- `TestBalloonNotificationOnce`: (Windows) Verify the balloon fires on first minimize but not subsequent ones
- Build verification: `GOOS=darwin go vet -tags desktop ./...`, `GOOS=windows go vet -tags desktop ./...`, `GOOS=linux go vet -tags desktop ./...`

## Boundaries

- Do NOT add code signing or notarization in this task (that's packaging, Task 8)
- Do NOT change the tray menu structure — only platform-specific behaviors
- Do NOT add Linux desktop file or freedesktop integration yet

## Implementation notes

- **Balloon notification (Windows):** `fyne.io/systray` does not support Windows balloon/toast notifications. Skipped — the tray icon appearance is sufficient indication.
- **Dock icon click (macOS):** Wails v2 has no public API for dock icon reopen events (`applicationShouldHandleReopen`). Users can reopen via "Open Dashboard" in the tray menu or by left-clicking the tray icon.
- **Cmd+Q (macOS):** Already handled natively by Wails — triggers `OnShutdown` which runs `tm.Stop()` + `sc.Shutdown()`. No additional code needed.
- **Left-click tray:** Used `systray.SetOnTapped(showWindow)` on all platforms for consistency — left-click shows the window, right-click opens the menu.
- **OnBeforeClose:** Added to `desktop.go` for macOS and Windows — returns `true` to prevent quit and calls `WindowHide()` instead. On Linux, window close quits normally.
- **Windows .ico:** Generated programmatically with 16x16 and 32x32 PNG-encoded variants in ICO container format.
