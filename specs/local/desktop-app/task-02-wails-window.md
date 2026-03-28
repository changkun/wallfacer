# Task 2: Desktop Subcommand with Wails Window

**Status:** Todo
**Depends on:** Task 1
**Phase:** Core window
**Effort:** Medium

## Goal

Implement the `desktop` subcommand so it starts the existing HTTP server and opens a native Wails window pointed at it, replacing the browser-open behavior.

## What to do

1. In `internal/cli/desktop.go` (with `//go:build desktop`), implement `RunDesktop()`:
   - Parse the same flags as `RunServer` (`-addr`, `-data`, `-container`, `-image`, `-env-file`, `-log-format`)
   - Start the HTTP server in a background goroutine using the same initialization sequence as `RunServer` (config dir, workspace manager, runner, handler, mux, listener)
   - Extract the bound address from the listener
   - Call `wails.Run()` with `options.App`:
     - `Title: "Wallfacer"`
     - `Width: 1400, Height: 900`
     - `URL: "http://localhost:<port>"` — point the WebView at the running server
     - `OnStartup`: store the Wails runtime context
     - `OnShutdown`: trigger graceful server shutdown (same sequence as signal handler in `RunServer`)
   - Do NOT use `AssetServer` with embedded files — the HTTP server already serves them
2. Refactor the server initialization from `RunServer()` into a shared helper (e.g., `initServer()`) that both `RunServer` and `RunDesktop` can call. This helper returns the `http.Server`, `net.Listener`, runner, and handler.
3. Skip the `openBrowser()` call when in desktop mode
4. Wire graceful shutdown: when the Wails window closes, trigger the same shutdown sequence (cancel context, drain HTTP, wait for runner)

## Tests

- `TestInitServer`: Test that `initServer()` returns a valid server and listener (integration test, may need test fixtures for config dir)
- `TestRunServerStillWorks`: Verify that `RunServer()` still works identically after the refactor (existing tests should cover this)
- Manual: `go build -tags desktop . && ./wallfacer desktop` opens a native window showing the task board

## Boundaries

- Do NOT add system tray functionality yet
- Do NOT add app icons or packaging
- Do NOT change the HTTP handler, routes, or middleware
- Keep the refactored `initServer()` minimal — extract only what both paths share
