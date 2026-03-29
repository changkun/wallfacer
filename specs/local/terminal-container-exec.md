# Terminal: Container Exec Integration

**Status:** Complete | **Date:** 2026-03-28 → 2026-03-30

---

## Current State

- **Host terminal** is complete: `/api/terminal/ws` WebSocket handler in `internal/handler/terminal.go`, xterm.js client in `ui/js/terminal.js`, PTY spawning via `internal/pty/`.
- **Terminal sessions** ([terminal-sessions.md](terminal-sessions.md)) is complete — multi-session tab bar with `sessionRegistry`, relay dispatcher, per-session reader goroutines, and `create_session`/`switch_session`/`close_session` WebSocket messages.
- **`GET /api/containers`** exists in `internal/handler/containers.go` — returns `[]sandbox.ContainerInfo` (ID, Name, TaskID, TaskTitle, Image, State, Status, CreatedAt) for running wallfacer sandbox containers.
- **CLI `wallfacer exec`** (`internal/cli/exec.go`) attaches to containers via `podman exec -it` with `syscall.Exec()` process replacement. Not available from the web UI.
- **`SandboxBackend` interface** (`internal/sandbox/backend.go`) defines `Launch` and `List` methods with a local implementation. The terminal handler does not use this interface.

## Problem

The host terminal runs a shell on the host machine. To inspect or debug a running task container, users must use `wallfacer exec <task-id>` from a separate terminal. A "Container Shell" tab type that attaches to running task containers would eliminate this context switch.

## Goal

Add a container exec terminal mode that spawns `podman exec -it <container> bash` instead of a host shell, selectable from a dropdown of running containers.

## Design

### Extend session creation for container exec

The existing `sessionRegistry.create(shell, cwd, cols, rows)` in `internal/handler/terminal.go` spawns a host shell via PTY. Extend it (or add a sibling method like `createContainerExec`) to spawn `podman exec -it <container> bash` instead. The per-session reader goroutine, `outputCh` channel, relay dispatcher, and process monitor all work unchanged — only the command spawned differs.

Add a `container` field to the `create_session` WebSocket message:
```json
{"type":"create_session","container":"<container-id>"}
```
When `container` is set, spawn `podman exec -it <container> bash` via PTY instead of a host shell. When absent, spawn a host shell as before (backward-compatible). Dispatch the exec command via `SandboxBackend` so different backends use different exec mechanisms (see Cloud Deployment below).

### Container selector UI

- Dropdown populated from `GET /api/containers` (already implemented in `internal/handler/containers.go`).
- Shows container name and associated task title.
- Selecting a container sends `{"type":"create_session","container":"<id>"}`, which opens a new tab labeled with the task title (e.g., "Task: fix-auth @ 3b616d1e").

### Cloud Deployment

In cloud deployment (K8s backend per [sandbox-backends.md](../foundations/sandbox-backends.md)), the host shell has limited utility — the API server is a stateless pod. Container exec becomes the primary terminal mode:

| Backend | Host shell | Container exec |
|---------|-----------|---------------|
| Local | PTY via `internal/pty` | `podman exec` via PTY |
| K8s | Disabled or server pod shell | `kubectl exec` via SPDY/WebSocket relay |
| Remote Docker | Disabled | `docker -H <remote> exec` via PTY |

The WebSocket protocol and xterm.js frontend are backend-agnostic — only the PTY spawn mechanism changes. The handler dispatches based on the active `SandboxBackend`.

## Dependencies

- Requires host terminal (complete).
- Requires [terminal-sessions.md](terminal-sessions.md) (complete) — container shell tabs use the session/tab registry to coexist with host shell tabs.

## Outcome

Container exec is fully integrated into the terminal panel. Users can click the container picker button (box icon) in the tab bar to see running task containers and open an interactive shell inside any of them, replacing the need for `wallfacer exec` from a separate terminal.

### What Shipped

- **Backend** (`internal/handler/terminal.go`): `createContainerExec()` method on `sessionRegistry` spawns `<runtime> exec -it <container> bash` via PTY. `container` field on `terminalMessage` for `create_session` requests. `container` field on `sessionInfo` in the `sessions` list response. `runtime` field on `sessionRegistry` populated from `runner.Command()`.
- **Container picker** (`ui/js/terminal.js`): dropdown fetches `GET /api/containers`, shows running containers with task titles, clicking an item sends `create_session` with container name. Uses `position: fixed` to escape the panel's `overflow: hidden` clipping. Dismiss on click-outside or Escape.
- **Container tab labels**: sessions with a `container` field display the container name instead of "Shell N".
- **22 backend terminal tests**, **32 frontend terminal tests** — all passing.
- **Docs**: WebSocket protocol table updated with `container` field, CLAUDE.md and AGENTS.md updated.

### Design Evolution

1. **Extended `create_session` instead of a separate WebSocket endpoint.** The original spec proposed a new `/api/terminal/container/ws` endpoint. The refined design added an optional `container` field to the existing `create_session` message, reusing the entire session registry, relay dispatcher, and tab infrastructure unchanged.

2. **`position: fixed` for container picker.** The initial implementation used `position: absolute` inside the tab bar, but `#status-bar-panel` has `overflow: hidden` which clipped the dropdown. Fixed by using `position: fixed` on `document.body` with coordinates calculated from the button's bounding rect.

3. **Screen clear on session switch.** `_term.clear()` only clears scrollback, not the visible viewport. Added `_clearTermScreen()` that also writes `ESC[2J ESC[H` (ANSI clear screen + cursor home) to fully wipe the display before replaying a new session's buffer.

4. **SandboxBackend dispatch deferred.** The spec called for dispatching via `SandboxBackend` interface. The implementation uses `runner.Command()` directly for the container runtime path, which is simpler and sufficient for the local backend. K8s/remote Docker dispatch remains future work in the Cloud Deployment section.

## Task Breakdown

| # | Task | Depends on | Effort | Status |
|---|------|-----------|--------|--------|
| 1 | [Backend container session](terminal-container-exec/task-01-backend-container-session.md) | — | Medium | Done |
| 2 | [Container selector UI](terminal-container-exec/task-02-container-selector-ui.md) | 1 | Medium | Done |
| 3 | [Documentation](terminal-container-exec/task-03-docs.md) | 2 | Small | Done |

```mermaid
graph LR
  1[Task 1: Backend container session] --> 2[Task 2: Container selector UI]
  2 --> 3[Task 3: Documentation]
```
