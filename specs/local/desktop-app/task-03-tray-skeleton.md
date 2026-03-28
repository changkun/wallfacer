# Task 3: System Tray — Static Skeleton

**Status:** Todo
**Depends on:** Task 2
**Phase:** System tray
**Effort:** Medium

## Goal

Add a system tray icon with a static menu containing "Open Dashboard" and "Quit" items. No polling or dynamic state yet.

## What to do

1. Create `internal/cli/tray.go` (with `//go:build desktop`) containing a `TrayManager` struct:
   - Fields: Wails runtime context, reference to the Wails app window
   - `NewTrayManager(ctx context.Context, window ...)` constructor
   - `Start()` method that creates the tray icon and menu
   - `Stop()` method for cleanup
2. Create a placeholder tray icon (a simple 22x22 PNG brick icon) at `assets/icons/tray.png`
   - Also create `assets/icons/tray@2x.png` (44x44) for HiDPI
   - On macOS, use template image format (monochrome with alpha)
3. Register the system tray in `RunDesktop()` via `wails.Run()` options or the Wails systray API
4. Implement the static menu structure:
   ```
   Open Dashboard  → calls window.Show() / window.Focus()
   ─────────────
   Quit            → triggers graceful shutdown
   ```
5. "Open Dashboard" should show and focus the main Wails window
6. "Quit" should initiate graceful shutdown (cancel the server context, same as SIGTERM path)
7. On macOS, closing the window should hide it (not quit) — the tray keeps the app alive. Re-open via "Open Dashboard" or dock icon click.

## Tests

- `TestTrayManagerNew`: Verify `NewTrayManager` initializes without panic
- `TestTrayMenuItems`: Verify the menu has exactly the expected items ("Open Dashboard", separator, "Quit")
- Manual: Launch desktop mode, verify tray icon appears, "Open Dashboard" shows window, "Quit" exits cleanly

## Boundaries

- Do NOT add polling, dynamic status, or automation toggles yet
- Do NOT implement platform-specific icon variants (Windows .ico, etc.) yet
- Keep the menu static — no task counts, no cost display
