---
title: "Status Bar Integration and Visibility Gate"
status: complete
track: foundations
depends_on:
  - specs/foundations/host-terminal/task-02-envconfig-terminal-enabled.md
  - specs/foundations/host-terminal/task-04-backend-terminal-handler.md
  - specs/foundations/host-terminal/task-05-frontend-terminal-js.md
affects:
  - ui/js/status-bar.js
  - ui/css/status-bar.css
effort: medium
created: 2026-03-22
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 6: Status Bar Integration and Visibility Gate

## Goal

Wire the terminal module into the status bar toggle/resize flow and implement the frontend visibility gate that hides the terminal button when the feature is disabled.

## What to do

### Status bar toggle: `ui/js/status-bar.js`

1. Modify `_showTerminalPanel()` (line 134) to call `connectTerminal()` after showing the panel:
   ```javascript
   function _showTerminalPanel() {
     // ... existing show logic ...
     if (typeof connectTerminal === 'function') connectTerminal();
   }
   ```
   This connects on first open and re-fits on subsequent opens (connectTerminal handles the "already connected" case).

2. In `_hideTerminalPanel()` (line 143): do NOT disconnect. The spec says to keep the WebSocket alive while the panel is hidden (preserves shell session for quick toggle).

### Resize integration: `ui/css/status-bar.css`

3. Add xterm container fill styles so the terminal fills the panel:
   ```css
   #status-bar-panel .xterm {
     height: 100%;
   }
   #status-bar-panel .xterm-viewport {
     overflow-y: auto;
   }
   ```

4. Add a reconnection overlay style:
   ```css
   .terminal-reconnecting {
     position: absolute;
     inset: 0;
     display: flex;
     align-items: center;
     justify-content: center;
     background: var(--bg-card);
     opacity: 0.85;
     color: var(--text-secondary);
     font-size: 0.875rem;
     z-index: 10;
   }
   ```

### Visibility gate: `ui/js/status-bar.js`

5. In `initStatusBar()` or at initialization time, read the `terminalEnabled` flag from the config response (available via `window._wallfacerConfig` or the config fetch in `events.js`). If `terminalEnabled === false`:
   - Hide the Terminal button: `document.getElementById('status-bar-terminal-btn').classList.add('hidden')`.
   - In `_cycleBottomPanel()`, skip the terminal step and go directly to dep graph.

6. In `toggleTerminalPanel()`, if the terminal is disabled and the panel is somehow opened, write a message into the panel:
   ```
   Terminal disabled. Set WALLFACER_TERMINAL_ENABLED=true in Settings > API Configuration.
   ```

### Terminal initialization: `ui/js/events.js`

7. After `fetchConfig()` resolves and the config is available, call `initTerminal()` if `terminalEnabled` is true. This ensures xterm.js is only initialized when the feature is enabled.

## Tests

- **Frontend (vitest):**
  - `test('toggleTerminalPanel calls connectTerminal')` — mock `connectTerminal`, toggle panel open, assert called.
  - `test('panel hide does not call disconnectTerminal')` — toggle open then close, assert `disconnectTerminal` not called.
  - `test('terminal button hidden when terminalEnabled is false')` — set config flag false, call init, assert button has `hidden` class.
  - `test('cycle skips terminal when disabled')` — set config flag false, cycle bottom panel, assert terminal panel not shown.

- **Integration (manual):**
  - With `WALLFACER_TERMINAL_ENABLED=true`: Terminal button visible, backtick opens panel with working shell.
  - With `WALLFACER_TERMINAL_ENABLED=false` (default): Terminal button hidden, backtick skips to dep graph.
  - Resize the panel by dragging the handle: terminal re-fits to new dimensions.

## Boundaries

- Do NOT implement multi-session tabs (Phase 2)
- Do NOT implement container exec (Phase 3)
- Do NOT add new keyboard shortcuts beyond the existing backtick toggle
