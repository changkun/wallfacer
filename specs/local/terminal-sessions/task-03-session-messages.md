# Task 3: Session Control WebSocket Messages

**Status:** Todo
**Depends on:** Task 2
**Phase:** Backend protocol
**Effort:** Small

## Goal

Add `create_session`, `switch_session`, and `close_session` message types to the terminal WebSocket protocol so the frontend can manage multiple sessions.

## What to do

1. In `internal/handler/terminal.go`, extend the `terminalMessage` struct with a `Session` field:
   ```go
   type terminalMessage struct {
       Type    string `json:"type"`
       Data    string `json:"data,omitempty"`
       Cols    int    `json:"cols,omitempty"`
       Rows    int    `json:"rows,omitempty"`
       Session string `json:"session,omitempty"`
   }
   ```

2. Add three new cases in the WebSocket message handler switch:

   - **`create_session`**: Call `registry.create()` with current terminal dimensions. Send response:
     ```json
     {"type":"session_created","session":"<new-id>"}
     ```
     Automatically switch the active session to the new one and notify the relay dispatcher.

   - **`switch_session`**: Read `msg.Session` for the target ID. Call `registry.get()` to validate. Switch the dispatcher's active session. Send response:
     ```json
     {"type":"session_switched","session":"<id>"}
     ```
     Return error if session ID not found:
     ```json
     {"type":"error","data":"session not found: <id>"}
     ```

   - **`close_session`**: Read `msg.Session` for the target ID. Call `registry.remove()`. If the closed session was active, auto-switch to another (or close WebSocket if none remain). Send response:
     ```json
     {"type":"session_closed","session":"<id>"}
     ```

3. Add a `list_sessions` response sent on initial connect and after any session change:
   ```json
   {"type":"sessions","sessions":[{"id":"...","active":true},{"id":"...","active":false}]}
   ```

## Tests

- `TestTerminalWS_CreateSession` — send `create_session`, verify `session_created` response, verify input routes to new session.
- `TestTerminalWS_SwitchSession` — create two sessions, switch between them, verify output isolation.
- `TestTerminalWS_CloseSession` — close a non-active session, verify it's gone. Close the active session, verify auto-switch.
- `TestTerminalWS_CloseLastSession` — close the only session, verify WebSocket closes.
- `TestTerminalWS_SwitchInvalidSession` — switch to nonexistent ID, verify error response.
- `TestTerminalWS_SessionsList` — verify `sessions` message sent on connect and after create/close.

## Boundaries

- Do NOT change the frontend yet (Task 5 wires these messages).
- Do NOT add tab UI.
- Keep the initial connection behavior unchanged: one session auto-created, existing clients that never send session messages work exactly as before.
