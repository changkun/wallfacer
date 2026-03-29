---
title: Desktop Subcommand with Wails Window
status: complete
track: local
depends_on:
  - specs/local/desktop-app/task-01-wails-dependency.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 2: Desktop Subcommand with Wails Window

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

## Implementation notes

- **URL field:** The spec prescribed `URL: "http://localhost:<port>"` on `options.App`, but Wails v2 has no `URL` field. Instead, `AssetServer.Handler` is set to an `httputil.NewSingleHostReverseProxy` that proxies all WebView requests to the running HTTP server. This achieves the same result — the WebView renders the task board served by the local HTTP server — without using embedded assets.
- **Default addr:** `RunDesktop` defaults to `:0` (random port) instead of `:8080` since the port is not user-visible (the WebView connects via the reverse proxy).
- **initServer return type:** Uses a `ServerComponents` struct (with `Srv`, `Ln`, `Runner`, `Handler`, `WsMgr`, `Ctx`, `Stop`, `ActualPort`) plus a `Shutdown()` method, rather than returning bare values.
