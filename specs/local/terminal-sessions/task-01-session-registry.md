# Task 1: Backend Session Registry

**Status:** Todo
**Depends on:** None
**Phase:** Backend infrastructure
**Effort:** Medium

## Goal

Replace the single `terminalSession` local variable in `HandleTerminalWS` with a session registry that can manage multiple concurrent PTY sessions per WebSocket connection.

## What to do

1. In `internal/handler/terminal.go`, add a `sessionRegistry` struct:
   ```go
   type sessionRegistry struct {
       mu       sync.Mutex
       sessions map[string]*terminalSession
       active   string // currently active session ID
   }
   ```

2. Add methods to `sessionRegistry`:
   - `create(shell string, cwd string, cols, rows int) (string, error)` — spawns a new PTY session, generates a UUID session ID, stores in map, returns the ID.
   - `get(id string) (*terminalSession, bool)` — retrieves a session by ID.
   - `remove(id string)` — calls `cleanup()` on the session and removes it from the map.
   - `closeAll()` — cleans up all sessions (called on WebSocket disconnect).
   - `activeSession() (*terminalSession, bool)` — returns the currently active session.

3. Update `terminalSession` struct to include an `id string` field and a `done chan struct{}` that closes when the shell process exits (replacing the process monitor goroutine pattern).

4. Refactor `HandleTerminalWS` to:
   - Create a `sessionRegistry` at the start of the connection.
   - Create one initial session via `registry.create()` instead of directly calling `pty.StartWithSize()`.
   - Pass the registry to the relay goroutines (Task 2 wires the dispatcher; for now, relay from `registry.activeSession()`).
   - Call `registry.closeAll()` on cleanup instead of `sess.cleanup()`.

5. Keep the existing message types (`input`, `resize`, `ping`) working exactly as before — this task only restructures the session storage, not the message protocol.

## Tests

- `TestSessionRegistry_CreateAndGet` — create a session, verify it's retrievable by ID.
- `TestSessionRegistry_Remove` — create and remove a session, verify it's gone and PTY is closed.
- `TestSessionRegistry_CloseAll` — create multiple sessions, close all, verify all PTYs cleaned up.
- `TestSessionRegistry_ActiveSession` — verify active session tracks correctly.
- **Existing tests must still pass** — `TestTerminalWS_Connect`, `TestTerminalWS_Resize`, `TestTerminalWS_Ping`, `TestTerminalWS_ShellExit` should all pass unchanged since externally the behavior is identical (single session created on connect).

## Boundaries

- Do NOT add new WebSocket message types yet (that's Task 3).
- Do NOT change the frontend (`ui/js/terminal.js`) — the protocol is unchanged.
- Do NOT change `ui/partials/status-bar.html`.
- Do NOT add tab-related concepts — this is purely backend session management.
