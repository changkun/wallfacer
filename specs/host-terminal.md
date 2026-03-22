# Plan: Host Shell Terminal Panel

**Status:** Draft
**Date:** 2026-03-22

---

## Problem Statement

The Wallfacer UI has a terminal panel stub (empty `<div id="status-bar-panel">` with resize handle and backtick toggle) but no actual terminal functionality. Users must switch to a separate terminal app to run commands on the host machine. An integrated terminal — like VS Code's — would let users run commands, inspect files, and interact with their workspace without leaving the board UI.

---

## Goal

Populate the existing terminal panel stub with a fully interactive host shell (bash/zsh) using xterm.js for terminal emulation and WebSocket for bidirectional I/O. The shell runs on the host where the Wallfacer server process lives, at the same privilege level as the server itself.

---

## Current State

### Terminal Panel Stub

- **HTML** (`ui/partials/status-bar.html`): Empty `<div id="status-bar-panel" class="status-bar-panel hidden">` with comment "Terminal stub panel (populated by future PRs)"
- **Resize** (`ui/js/status-bar.js:125-179`): Drag-to-resize handle, 80–600px range, height persisted to `localStorage`
- **Toggle**: Backtick key toggles visibility, Terminal button in status bar right section
- **CSS** (`ui/css/status-bar.css`): Panel styles (flex column, 260px default height, overflow hidden)

### Existing Streaming Infrastructure

| Component | Location | What it does |
|-----------|----------|-------------|
| `StreamLogs` handler | `internal/handler/stream.go:175` | Streams container logs via HTTP with `http.Flusher` |
| Log consumer | `ui/js/modal-logs.js` | Consumes HTTP streams via Fetch Streams API |
| ANSI converter | `ui/js/modal-ansi.js` | Converts ANSI escape codes to HTML spans |
| SSE auth | `internal/handler/middleware.go:66` | `?token=` query param auth for streaming paths |

**Limitation:** All streaming is one-directional (server→client). Interactive terminal requires full-duplex communication — **WebSocket is needed** (the project's first).

### CLI `exec` Command

`internal/cli/exec.go` uses `syscall.Exec` to replace the process with `podman exec -it`. This is terminal-only (requires PTY) and cannot work over HTTP.

---

## Design

### Transport: WebSocket

HTTP streaming cannot carry user input. WebSocket provides the full-duplex channel needed for interactive terminal I/O. This introduces the project's first WebSocket endpoint.

### Terminal Emulator: xterm.js

xterm.js is the standard web terminal emulator (used by VS Code, JupyterLab, Gitpod). It handles terminal escape sequences, cursor positioning, scrollback, selection, and clipboard natively. Distributed as standalone JS files — no bundler required.

### PTY Backend: `github.com/creack/pty`

Standard Go PTY library. Wraps `posix_openpt`/`forkpty` on macOS/Linux, ConPTY on Windows 10+. Spawns a shell process with proper terminal semantics.

---

## Backend

### New Dependencies

| Module | Purpose | Size |
|--------|---------|------|
| `github.com/creack/pty` | Cross-platform PTY allocation | ~200 LOC, no transitive deps |
| `nhooyr.io/websocket` | WebSocket built on stdlib `net/http` | Fits existing `http.ServeMux` router; context-aware |

The project currently has one dependency (`github.com/google/uuid`). These add two more.

### WebSocket Endpoint

```
GET /api/terminal/ws?token=<key>&cols=<n>&rows=<n>&cwd=<path>
```

- **Not registered via `apicontract/routes.go`** — WebSocket upgrades don't follow REST request/response semantics. Registered directly in `BuildMux` (like `/metrics`), with a comment explaining why.
- **Auth:** Add `/api/terminal/ws` to `isSSEPath` in `middleware.go` so it accepts `?token=` authentication (browser `WebSocket` constructor cannot set custom headers).

### Opt-In Configuration

Add `WALLFACER_TERMINAL_ENABLED` to `internal/envconfig/envconfig.go`:

- Defaults to `false` (opt-in). This is a full host shell — must be explicitly enabled.
- Handler returns `403 Forbidden` when disabled.
- Include `terminalEnabled: bool` in `GET /api/config` response so the frontend can hide the Terminal button when disabled.
- Editable from **Settings → API Configuration** in the UI.

### Handler: `internal/handler/terminal.go`

```go
type TerminalSession struct {
    id     string          // short random ID for logging
    ptmx   *os.File        // PTY master file descriptor
    cmd    *exec.Cmd       // shell process
    cancel context.CancelFunc
}
```

**`HandleTerminalWS` flow:**

1. Check `TerminalEnabled` from env config. Return 403 if disabled.
2. Accept WebSocket upgrade via `websocket.Accept(w, r, nil)`.
3. Determine shell: `$SHELL` env var → `/bin/bash` → `/bin/sh`.
4. Determine cwd: `cwd` query param (if valid and within a workspace) → first active workspace path.
5. Spawn shell: `pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})`.
6. Two relay goroutines:
   - **PTY→WebSocket**: Read PTY output (32 KB buffer), write as binary WebSocket messages.
   - **WebSocket→PTY**: Read WebSocket messages, dispatch by type, write stdin data to PTY.
7. When either goroutine exits, cancel the other and clean up.

### Message Protocol

**Client→Server** (JSON text messages):

| Type | Payload | Description |
|------|---------|-------------|
| `input` | `{"type":"input","data":"<base64>"}` | Keyboard input → PTY stdin |
| `resize` | `{"type":"resize","cols":N,"rows":N}` | Window resize → `pty.Setsize()` |
| `ping` | `{"type":"ping"}` | Keepalive → server responds with `{"type":"pong"}` |

**Server→Client**: Raw binary (PTY output bytes). Single exception: pong response as JSON text. This asymmetric design minimizes overhead on the high-volume output path.

### Process Cleanup

| Trigger | Action |
|---------|--------|
| WebSocket close (normal) | SIGHUP to shell process group, 2s timeout, SIGKILL if alive, close PTY fd |
| WebSocket drop (abrupt) | `context.Context` cancelled → same cleanup via defer chain |
| Shell exit (`exit` command) | Close WebSocket with status 1000 |
| Server shutdown | Context cancellation propagates from server's `BaseContext` |

### Platform Notes

- **macOS/Linux**: Full support via `creack/pty`.
- **Windows**: ConPTY support exists in `creack/pty` but is less battle-tested. Phase 1 can gate on `runtime.GOOS != "windows"` if needed.

---

## Frontend

### xterm.js Vendoring

Download and place alongside existing vendored libraries:

| File | Destination | Size |
|------|-------------|------|
| `xterm.min.js` | `ui/js/vendor/xterm.min.js` | ~90 KB gzipped |
| `xterm-addon-fit.min.js` | `ui/js/vendor/xterm-addon-fit.min.js` | ~3 KB |
| `xterm.css` | `ui/css/vendor/xterm.css` | ~5 KB |

Loaded via `<script>` and `<link>` tags in `initial-layout.html`, matching the pattern for `sortable.min.js`, `marked.min.js`, `highlight.min.js`. Embedded via existing `//go:embed ui` — no build step changes.

### New File: `ui/js/terminal.js`

**Key functions:**

- **`initTerminal()`** — Called on DOMContentLoaded. Creates `xterm.Terminal` instance with theme from CSS vars (`--bg-card`, `--text`, `--accent`). Attaches `FitAddon`. Mounts into `#status-bar-panel`. Does NOT connect yet — waits for first panel open.

- **`connectTerminal()`** — Called when panel becomes visible. Builds WebSocket URL (`ws(s)://` + host + `/api/terminal/ws?token=...&cols=...&rows=...`). Token from `getWallfacerToken()` in `transport.js`. Wires up:
  - `ws.onmessage` → `terminal.write(data)`
  - `terminal.onData` → send `{"type":"input","data":"<base64>"}`
  - `terminal.onResize` → send `{"type":"resize","cols":N,"rows":N}`

- **`disconnectTerminal()`** — Closes WebSocket with code 1000.

- **Reconnection**: On non-1000 close, exponential backoff (1s, 2s, 4s, max 30s). Show "Disconnected. Reconnecting..." overlay in terminal panel. Clear terminal on reconnect (new shell session).

### Resize Integration

The existing drag-to-resize in `status-bar.js` changes `panel.style.height`. Terminal must respond:

1. Add `ResizeObserver` on `#status-bar-panel` in `initTerminal()`.
2. On size change → `fitAddon.fit()` → recalculates cols/rows → emits `terminal.onResize` → sends WebSocket resize message.
3. On initial panel open, call `fitAddon.fit()` after panel becomes visible (hidden panel has zero dimensions).

### Toggle Integration

Modify `toggleTerminalPanel()` in `status-bar.js`:

- **Panel opens**: Call `connectTerminal()` if not connected, `fitAddon.fit()`, `terminal.focus()`.
- **Panel hides**: Keep WebSocket alive (preserve shell session and scrollback for quick toggle).
- Backtick shortcut already calls `toggleTerminalPanel()` — no change needed.

### Theme Integration

Map xterm.js theme from CSS custom properties:

```javascript
{
  background: getCSSVar('--bg-card'),
  foreground: getCSSVar('--text'),
  cursor: getCSSVar('--accent'),
  selectionBackground: 'rgba(78,140,255,0.3)',
  // Standard ANSI 16-color palette
}
```

xterm.js handles its own ANSI rendering — the existing `modal-ansi.js` converter is not reused here.

### Frontend Visibility Gate

If `GET /api/config` returns `terminalEnabled: false`:
- Hide the Terminal button in the status bar
- Disable the backtick keyboard shortcut
- If the panel is somehow opened, show a message: "Terminal disabled. Set WALLFACER_TERMINAL_ENABLED=true in Settings → API Configuration."

---

## Security

| Concern | Mitigation |
|---------|------------|
| Unauthorized access | Bearer token auth via `?token=` (same as SSE paths) |
| Opt-in control | `WALLFACER_TERMINAL_ENABLED` defaults to `false` |
| Privilege level | Same as host — user already runs the server on their machine |
| Input sanitization | None needed — PTY receives raw bytes like a physical terminal |
| Output flooding | 32 KB read buffer with WebSocket write back-pressure |
| Orphaned processes | Context cancellation + SIGHUP/SIGKILL cleanup chain |
| Path traversal (cwd) | Validate `cwd` against active workspace list |

---

## Phasing

### Phase 1: Single Terminal Session

- One shell session per browser tab
- Connect on panel open, keep alive while panel hidden, reconnect on disconnect
- Working directory defaults to first active workspace
- Opt-in via `WALLFACER_TERMINAL_ENABLED`

**Complexity: Medium.** Backend WebSocket+PTY relay is the main effort (~60%). Frontend xterm.js integration is straightforward (~25%). Config/auth plumbing is minimal (~15%).

### Phase 2: Multiple Sessions with Tabs

- Tab bar above the terminal in the panel
- Session registry in handler (`map[string]*TerminalSession`)
- New messages: `create_session`, `switch_session`, `close_session`
- Tabs show session names (numbered or named by cwd basename)

### Phase 3: Container Exec Integration

- "Container Shell" tab type that attaches to running task containers
- Spawns `podman exec -it <container> bash` instead of host shell
- Dropdown to select from running containers (data from `GET /api/containers`)
- Replaces `wallfacer exec` CLI for many use cases

---

## File Inventory

### New Files

| File | Purpose |
|------|---------|
| `internal/handler/terminal.go` | WebSocket handler, PTY lifecycle, message protocol |
| `internal/handler/terminal_test.go` | Tests: WebSocket connect, resize, auth gate, opt-in gate, cleanup |
| `ui/js/terminal.js` | xterm.js integration, WebSocket client, resize/reconnect |
| `ui/js/vendor/xterm.min.js` | Vendored xterm.js core |
| `ui/js/vendor/xterm-addon-fit.min.js` | Vendored fit addon |
| `ui/css/vendor/xterm.css` | Vendored xterm.js styles |

### Modified Files

| File | Change |
|------|--------|
| `go.mod` / `go.sum` | Add `github.com/creack/pty`, `nhooyr.io/websocket` |
| `internal/envconfig/envconfig.go` | Add `TerminalEnabled` field |
| `internal/handler/middleware.go` | Add `/api/terminal/ws` to `isSSEPath` |
| `internal/handler/config.go` | Include `terminalEnabled` in config response |
| `internal/cli/server.go` | Register `/api/terminal/ws` in `BuildMux` |
| `ui/partials/initial-layout.html` | Add `<link>` for xterm.css, `<script>` for xterm vendor files |
| `ui/partials/scripts.html` | Add `<script src="/js/terminal.js">` |
| `ui/js/status-bar.js` | Call `connectTerminal()`/`fitAddon.fit()` from `toggleTerminalPanel()` |
| `ui/css/status-bar.css` | xterm container fill styles, reconnection overlay |
| `docs/guide/configuration.md` | Document `WALLFACER_TERMINAL_ENABLED` |
| `CLAUDE.md` | Add terminal endpoint and env var |
