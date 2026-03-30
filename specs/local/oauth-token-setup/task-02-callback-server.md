---
title: Ephemeral Callback Server
status: complete
depends_on:
  - specs/local/oauth-token-setup/task-01-pkce-utilities.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 2: Ephemeral Callback Server

## Goal

Implement the ephemeral localhost HTTP listener that receives the OAuth callback redirect. The server binds to a random port on `127.0.0.1`, accepts exactly one request, extracts the authorization code and state from query params, then shuts down.

## What to do

1. Create `internal/oauth/callback.go` with:
   - `CallbackResult` struct: `Code string`, `State string`, `Error string`, `ErrorDescription string`.
   - `CallbackServer` struct holding the `net.Listener`, a result channel, and a `context.CancelFunc`.
   - `NewCallbackServer(ctx context.Context, timeout time.Duration) (*CallbackServer, error)` — binds `127.0.0.1:0`, starts an HTTP server in a goroutine, returns the server. The HTTP handler:
     - Parses `code`, `state`, `error`, `error_description` from query params.
     - Sends a `CallbackResult` on the channel.
     - Responds with a simple HTML success/error page (include a "You can close this tab" message).
     - Triggers graceful shutdown after responding.
   - `(s *CallbackServer) Port() int` — returns the bound port.
   - `(s *CallbackServer) Wait() (CallbackResult, error)` — blocks until result or context cancellation/timeout.
   - `(s *CallbackServer) Close()` — force-close the listener (for cancellation).

2. The timeout is enforced via the passed `ctx` (caller wraps with `context.WithTimeout`). When the context expires, `Wait()` returns an error.

3. The HTML response page should be minimal — a short message with inline CSS, no external dependencies.

## Tests

- `TestCallbackServer_ReceivesCode` — start server, HTTP GET to `localhost:{port}/callback?code=abc&state=xyz`, verify `Wait()` returns the correct `CallbackResult`.
- `TestCallbackServer_ReceivesError` — send `?error=access_denied&error_description=User+denied`, verify result.
- `TestCallbackServer_Timeout` — start server with a short timeout (100ms), never send a request, verify `Wait()` returns a timeout error.
- `TestCallbackServer_Close` — start server, call `Close()`, verify `Wait()` returns promptly.
- `TestCallbackServer_BindsLocalhost` — verify the listener address is `127.0.0.1`, not `0.0.0.0`.

## Boundaries

- Do NOT implement token exchange (that belongs in the flow engine).
- Do NOT add provider-specific logic.
- Do NOT register any HTTP routes on the main Wallfacer server.
