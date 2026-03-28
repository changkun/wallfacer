# Task 5: Frontend Terminal Module

**Status:** Todo
**Depends on:** 3
**Phase:** Phase 1 — Single Terminal Session
**Effort:** Medium

## Goal

Create `ui/js/terminal.js` with xterm.js initialization, WebSocket connection management, theme integration, and reconnection logic.

## What to do

1. **Create `ui/js/terminal.js`** with these functions:

   **`initTerminal()`** — Called once on load:
   - Create `new Terminal({...theme})` with colors from CSS vars: `getCSSVar('--bg-card')` for background, `getCSSVar('--text')` for foreground, `getCSSVar('--accent')` for cursor. Use `getComputedStyle(document.documentElement).getPropertyValue(name)` to read vars.
   - Create and load `FitAddon` via `new FitAddon.FitAddon()`, `terminal.loadAddon(fitAddon)`.
   - Open terminal into `#status-bar-panel`: `terminal.open(document.getElementById('status-bar-panel'))`.
   - Set up `ResizeObserver` on `#status-bar-panel` to call `fitAddon.fit()` on size change, then send resize message over WebSocket if connected.
   - Do NOT connect yet — connection happens on first panel open.

   **`connectTerminal()`** — Called when panel becomes visible:
   - If already connected, just call `fitAddon.fit()` and `terminal.focus()`, return.
   - Build WebSocket URL: `(location.protocol === 'https:' ? 'wss:' : 'ws:') + '//' + location.host + '/api/terminal/ws'`.
   - Append query params: `token` via `getWallfacerToken()` from `transport.js`, `cols` and `rows` from terminal dimensions.
   - Create `new WebSocket(url)`, set `binaryType = 'arraybuffer'`.
   - Wire events:
     - `ws.onopen`: call `fitAddon.fit()`, `terminal.focus()`.
     - `ws.onmessage`: if binary data, `terminal.write(new Uint8Array(event.data))`. If text and JSON with `type: "pong"`, ignore.
     - `ws.onclose`: if code !== 1000, start reconnection with exponential backoff (1s, 2s, 4s, max 30s). Show "Disconnected. Reconnecting..." in terminal via `terminal.write(...)`. On reconnect, `terminal.clear()`.
     - `ws.onerror`: log to console.
   - Wire terminal events:
     - `terminal.onData(data)`: send JSON `{"type":"input","data":"<btoa(data)>"}`.
     - `terminal.onResize({cols, rows})`: send JSON `{"type":"resize","cols":cols,"rows":rows}`.

   **`disconnectTerminal()`** — Called on explicit disconnect:
   - Close WebSocket with code 1000.
   - Cancel any pending reconnection timer.

   **`isTerminalConnected()`** — Returns `true` if WebSocket is open.

2. **Export to `window`**: `window.initTerminal`, `window.connectTerminal`, `window.disconnectTerminal`, `window.isTerminalConnected`.

3. **`ui/partials/scripts.html`** — Add `<script src="/js/terminal.js"></script>` after `status-bar.js` and before `explorer.js` (terminal depends on transport.js globals being available, and status-bar.js calls into terminal).

## Tests

- **`ui/js/__tests__/terminal.test.js`** (vitest):
  - `test('initTerminal creates xterm instance')` — mock `Terminal` and `FitAddon`, call `initTerminal()`, assert constructors called with theme config.
  - `test('connectTerminal builds correct WebSocket URL')` — mock `WebSocket`, call `connectTerminal()`, assert URL includes `/api/terminal/ws` with token and dimensions.
  - `test('disconnectTerminal closes WebSocket')` — connect, then disconnect, assert `ws.close(1000)` called.
  - `test('reconnection uses exponential backoff')` — simulate close with non-1000 code, assert reconnect timer increases (1s, 2s, 4s).

## Boundaries

- Do NOT modify `status-bar.js` yet (Task 6)
- Do NOT implement multi-session/tab support (Phase 2)
- Do NOT implement the visibility gate (Task 6)
- Keep reconnection simple — clear terminal and start fresh session on reconnect
