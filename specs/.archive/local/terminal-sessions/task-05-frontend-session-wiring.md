---
title: Frontend Session Wiring
status: archived
depends_on:
  - specs/local/terminal-sessions/task-03-session-messages.md
  - specs/local/terminal-sessions/task-04-tab-bar-ui.md
affects: []
effort: large
created: 2026-03-28
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 5: Frontend Session Wiring

## Goal

Wire the tab bar UI to the backend session protocol so that clicking "+ New", switching tabs, and closing tabs send the correct WebSocket messages and the terminal displays the right session's output.

## What to do

1. In `ui/js/terminal.js`, add session state tracking:
   ```javascript
   let _sessions = new Map();  // sessionId → { buffer: string[] }
   let _activeSessionId = null;
   ```
   When switching sessions, save the current terminal content and restore the new session's buffered output.

2. Handle incoming session-related WebSocket messages in the `onmessage` handler:
   - `session_created`: call `_addTab(id, "Shell N")`, activate the new tab.
   - `session_switched`: call `_activateTab(id)`, swap terminal content to the new session's buffer.
   - `session_closed`: call `_removeTab(id)`. If active session was closed, the backend auto-switches — wait for `session_switched`.
   - `session_exited`: show a message in the terminal ("Session ended"), remove the tab.
   - `sessions`: on initial connect, populate tabs from the session list. Activate the one marked `active: true`.

3. Wire tab bar interactions to WebSocket messages:
   - "+" button click → send `{"type":"create_session"}`.
   - Tab click → send `{"type":"switch_session","session":"<id>"}`.
   - Tab close (×) button → send `{"type":"close_session","session":"<id>"}`.

4. Buffer management for session switching:
   - On incoming binary data, write to xterm AND append to `_sessions.get(_activeSessionId).buffer` (capped at a reasonable limit, e.g., 100KB per session).
   - On `session_switched`, clear xterm, write the new session's buffer to restore scroll history.
   - Consider using xterm.js `serialize` addon for more accurate buffer preservation if buffer arrays prove insufficient.

5. Update `connectTerminal()`:
   - On WebSocket `onopen`, wait for the `sessions` message from the server before displaying terminal content.
   - On reconnect, repopulate tabs from the `sessions` message.

6. Update `disconnectTerminal()`:
   - Clear `_sessions` map and remove all tabs.

7. Update keyboard shortcut handling if any exists for terminal focus — ensure it focuses the active session.

## Tests

- `TestTerminalSessions_CreateViaUI` — simulate "+" click, verify `create_session` message sent, verify new tab appears after `session_created` response.
- `TestTerminalSessions_SwitchViaTab` — simulate tab click, verify `switch_session` message sent, verify terminal content swaps.
- `TestTerminalSessions_CloseViaTab` — simulate close button click, verify `close_session` message sent, verify tab removed.
- `TestTerminalSessions_ServerSessionExited` — simulate `session_exited` message, verify tab removed and message displayed.
- `TestTerminalSessions_ReconnectRestoresTabs` — simulate disconnect + reconnect, verify tabs repopulated from `sessions` message.
- `TestTerminalSessions_BufferSwap` — write data to session A, switch to B, switch back to A, verify A's output restored.
- Run `make test-frontend` and `make test-backend`.

## Boundaries

- Do NOT change the backend protocol (that's done in Task 3).
- Do NOT add container exec tabs (that's a separate spec).
- Do NOT add tab renaming UI or drag-to-reorder — keep it simple for now.
