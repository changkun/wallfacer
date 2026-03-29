# Task 2: Relay Dispatcher

**Status:** Done
**Depends on:** Task 1
**Phase:** Backend infrastructure
**Effort:** Medium

## Goal

Replace the two hardcoded relay goroutines (PTY→WebSocket and WebSocket→PTY) with a dispatcher that routes I/O to the currently active session, supporting session switching without reconnecting the WebSocket.

## What to do

1. In `internal/handler/terminal.go`, add a `relayDispatcher` that manages the PTY→WebSocket output relay:
   - When a session becomes active, start reading from its PTY and writing binary frames to the WebSocket.
   - When switching sessions, stop reading from the old PTY output and start reading from the new one.
   - Use a channel or context cancellation to signal relay goroutines to stop/start.

2. The input path (WebSocket→PTY) is simpler: the `input` message handler should always write to `registry.activeSession().ptmx`. No goroutine management needed — just resolve the active session on each message.

3. For `resize` messages, resize only the active session's PTY (via `pty.Setsize()`).

4. Handle session exit: when a session's shell process exits, if it's the active session, the dispatcher should:
   - Send a text WebSocket message notifying the frontend: `{"type":"session_exited","session":"<id>"}`.
   - If other sessions exist, auto-switch to the most recent one.
   - If no sessions remain, close the WebSocket (preserving current behavior for single-session case).

5. Each session gets its own PTY→WebSocket reader goroutine that only runs while the session is active. Use a `sync.Cond` or per-session context to pause/resume.

## Tests

- `TestRelayDispatcher_SingleSession` — verify existing single-session relay works (input echo, resize).
- `TestRelayDispatcher_SwitchActive` — create two sessions, switch active, verify input goes to new session's PTY.
- `TestRelayDispatcher_SessionExit` — active session exits, verify `session_exited` message sent and fallback to remaining session.
- `TestRelayDispatcher_LastSessionExit` — last session exits, verify WebSocket closes with status 1000 (backward-compatible with `TestTerminalWS_ShellExit`).
- All existing terminal tests must still pass.

## Boundaries

- Do NOT add `create_session`/`switch_session`/`close_session` WebSocket message types yet (Task 3).
- Do NOT change the frontend.
- Do NOT add tab UI.
- The dispatcher should be testable independently of the WebSocket handler (accept interfaces, not concrete WebSocket conn).
