# Terminal: Container Exec Integration

**Status:** Not started
**Date:** 2026-03-28

---

## Problem

The host terminal ([05-host-terminal.md](05-host-terminal.md)) runs a shell on the host machine. To inspect or debug a running task container, users must use `wallfacer exec <task-id>` from a separate terminal. A "Container Shell" tab type that attaches to running task containers would eliminate this context switch.

## Goal

Add a container exec terminal mode that spawns `podman exec -it <container> bash` instead of a host shell, selectable from a dropdown of running containers.

## Design Sketch

- **"Container Shell" tab type** in the terminal panel (alongside host shell tabs from [05a-terminal-sessions.md](05a-terminal-sessions.md)).
- **Container selector** dropdown populated from `GET /api/containers`.
- Spawns `podman exec -it <container> bash` via PTY instead of a host shell.
- Same WebSocket relay protocol as the host terminal.
- Replaces `wallfacer exec` CLI for many use cases.

### Cloud Deployment

In cloud deployment (K8s backend per [01-sandbox-backends.md](01-sandbox-backends.md)), the host shell has limited utility — the API server is a stateless pod. Container exec becomes the primary terminal mode:

| Backend | Host shell | Container exec |
|---------|-----------|---------------|
| Local | PTY via `internal/pty` | `podman exec` via PTY |
| K8s | Disabled or server pod shell | `kubectl exec` via SPDY/WebSocket relay |
| Remote Docker | Disabled | `docker -H <remote> exec` via PTY |

The WebSocket protocol and xterm.js frontend are backend-agnostic — only the PTY spawn mechanism changes. The handler dispatches based on the active `SandboxBackend`.

## Dependencies

- Requires M5 Phase 1 (complete).
- Ideally after [05a-terminal-sessions.md](05a-terminal-sessions.md) (tab infrastructure).
