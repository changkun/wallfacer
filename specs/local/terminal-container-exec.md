# Terminal: Container Exec Integration

**Status:** Not started
**Date:** 2026-03-28

---

## Current State

- **Host terminal** is complete: `/api/terminal/ws` WebSocket handler in `internal/handler/terminal.go`, xterm.js client in `ui/js/terminal.js`, PTY spawning via `internal/pty/`. Single-session only.
- **`GET /api/containers`** exists in `internal/handler/containers.go` — returns `[]sandbox.ContainerInfo` (ID, Name, TaskID, TaskTitle, Image, State, Status, CreatedAt) for running wallfacer sandbox containers.
- **CLI `wallfacer exec`** (`internal/cli/exec.go`) attaches to containers via `podman exec -it` with `syscall.Exec()` process replacement. Not available from the web UI.
- **`SandboxBackend` interface** (`internal/sandbox/backend.go`) defines `Launch` and `List` methods with a local implementation. The terminal handler does not use this interface.
- **Terminal sessions** ([terminal-sessions.md](terminal-sessions.md)) is not started — no tab infrastructure exists yet.

## Problem

The host terminal runs a shell on the host machine. To inspect or debug a running task container, users must use `wallfacer exec <task-id>` from a separate terminal. A "Container Shell" tab type that attaches to running task containers would eliminate this context switch.

## Goal

Add a container exec terminal mode that spawns `podman exec -it <container> bash` instead of a host shell, selectable from a dropdown of running containers.

## Design

### Container exec WebSocket handler

Add a new WebSocket endpoint (e.g., `/api/terminal/container/ws?container=<id>&token=<key>`) that:

- Accepts a container ID parameter identifying the target container.
- Spawns `podman exec -it <container> bash` via PTY (using `internal/pty/`) instead of a host shell.
- Reuses the same JSON message protocol as the host terminal (`input`, `resize`, `ping` message types, binary output frames).
- Dispatches the spawn mechanism via `SandboxBackend` so different backends use different exec commands (see Cloud Deployment below).

The host terminal handler (`internal/handler/terminal.go`) serves as the implementation template — the only difference is what command is spawned.

### Container selector UI

- Dropdown populated from `GET /api/containers` (already implemented in `internal/handler/containers.go`).
- Shows container name and associated task title.
- Selecting a container opens a new "Container Shell" tab (requires terminal session tab infrastructure from [terminal-sessions.md](terminal-sessions.md)).

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
