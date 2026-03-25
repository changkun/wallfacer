# Live Serve — Build and Run Developed Software from Wallfacer

**Status:** Draft
**Date:** 2026-03-25

---

## Problem

After an AI agent completes a task, there is no way to verify that the software actually builds and runs correctly without leaving Wallfacer. Users must open a terminal, navigate to the worktree or workspace, figure out the correct build/run commands, and manually test. This breaks the feedback loop — especially for web apps, servers, and CLI tools where "does it start?" is the first validation step.

There is also no way to keep a development server running while iterating across multiple tasks. Each time a task modifies code, the user must manually restart the server to see the changes.

---

## Current State (as of 2026-03-25)

- **Task execution**: Runs AI agent containers with Claude/Codex CLI. The agent reads/writes code but never builds or runs the resulting software.
- **Container lifecycle**: Ephemeral `--rm` containers. One container per task turn, torn down after the agent finishes.
- **Log streaming**: `GET /api/tasks/{id}/logs` streams `docker logs -f` output via SSE. Works for agent output, not for arbitrary processes.
- **Worktrees**: Per-task git worktrees provide isolated copies of the codebase at `/workspace/<basename>`.
- **No**: Dev server, build pipeline, port forwarding, process manager, or any way to run user code inside or outside the sandbox.

---

## Design

### Scope: Workspace + Task

A "serve session" targets either:
1. **Workspace directories** (default) — runs against the main workspace copy. Useful for verifying the combined result of multiple tasks after merging.
2. **A specific task's worktrees** — runs against the task's isolated branch. Useful for testing a single task's changes before committing.

The user selects the target when starting a serve session. The UI defaults to workspace scope.

### Discovery: Agent-Driven Command Detection

Rather than requiring users to configure build/run commands manually, an agent reads the codebase and proposes commands. The flow:

1. User clicks "Serve" (toolbar button or task action).
2. Wallfacer launches a lightweight agent container that reads the target directory and outputs a JSON config: `{ "build": "...", "run": "...", "port": N, "env": {...} }`.
3. The proposed config is shown in a modal for the user to review and edit.
4. User confirms → serve session starts.

The discovery agent uses a dedicated system prompt template (`prompts/serve-discover.tmpl`) that instructs it to look for `Makefile`, `package.json`, `go.mod`, `Cargo.toml`, `docker-compose.yml`, etc., and emit structured JSON.

Previously-confirmed configs are cached per workspace fingerprint in `~/.wallfacer/serve-configs/` so the agent step is skipped on repeat runs.

### Auto-Rebuild: Opt-In

By default, the serve session runs the build+run command once. When `WALLFACER_SERVE_AUTO_REBUILD=true` (or toggled via the UI), file changes trigger an automatic rebuild+restart cycle via filesystem watching inside the container.

---

## Data Model

### ServeSession

New type in `internal/store/models.go`:

```go
// ServeSession represents a running or completed live-serve session.
type ServeSession struct {
    ID            uuid.UUID          `json:"id"`
    Status        ServeStatus        `json:"status"`         // "discovering", "pending", "running", "stopped", "failed"
    Scope         ServeScope         `json:"scope"`          // "workspace" or "task"
    TaskID        *uuid.UUID         `json:"task_id,omitempty"` // set when scope=task
    Config        ServeConfig        `json:"config"`
    ContainerName string             `json:"container_name,omitempty"`
    StartedAt     *time.Time         `json:"started_at,omitempty"`
    StoppedAt     *time.Time         `json:"stopped_at,omitempty"`
    Error         string             `json:"error,omitempty"`
    AutoRebuild   bool               `json:"auto_rebuild"`
}

type ServeStatus string

const (
    ServeDiscovering ServeStatus = "discovering"
    ServePending     ServeStatus = "pending"
    ServeRunning     ServeStatus = "running"
    ServeStopped     ServeStatus = "stopped"
    ServeFailed      ServeStatus = "failed"
)

type ServeScope string

const (
    ServeScopeWorkspace ServeScope = "workspace"
    ServeScopeTask      ServeScope = "task"
)

// ServeConfig holds the build/run commands for a serve session.
type ServeConfig struct {
    BuildCmd string            `json:"build_cmd"`        // e.g. "go build -o server ."
    RunCmd   string            `json:"run_cmd"`          // e.g. "./server -addr :8080"
    Port     int               `json:"port,omitempty"`   // primary port to expose/health-check
    Env      map[string]string `json:"env,omitempty"`    // extra env vars for the process
    WorkDir  string            `json:"work_dir,omitempty"` // relative path within workspace
}
```

### Storage

Serve sessions are stored in `~/.wallfacer/serve/`:
- `~/.wallfacer/serve/<session-uuid>.json` — session state
- `~/.wallfacer/serve-configs/<workspace-fingerprint>.json` — cached discovery results

This is separate from per-task `data/` storage because serve sessions are workspace-scoped, not task-scoped.

---

## API

### Serve Session Management

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/serve` | Get current serve session (if any) |
| `POST` | `/api/serve/discover` | Launch discovery agent to propose build/run config |
| `DELETE` | `/api/serve/discover` | Cancel active discovery |
| `POST` | `/api/serve` | Start serve session with confirmed config |
| `DELETE` | `/api/serve` | Stop running serve session |
| `PATCH` | `/api/serve` | Update serve session (toggle auto-rebuild, change config) |
| `GET` | `/api/serve/logs` | SSE stream of serve container output |

### Request/Response Details

**`POST /api/serve/discover`**
```json
{
  "scope": "workspace",
  "task_id": null
}
```
Response: `202 Accepted` — discovery agent starts. Poll `GET /api/serve` for `status: "discovering"` → `"pending"` transition with proposed `config`.

**`POST /api/serve`**
```json
{
  "scope": "workspace",
  "task_id": null,
  "config": {
    "build_cmd": "go build -o server .",
    "run_cmd": "./server -addr :3000",
    "port": 3000,
    "env": { "GIN_MODE": "debug" }
  },
  "auto_rebuild": false
}
```
Response: `200 OK` with `ServeSession`.

**`DELETE /api/serve`**
Stops the running container and moves session to `"stopped"`. Response: `200 OK`.

**`GET /api/serve/logs`**
SSE stream. Same pattern as `GET /api/tasks/{id}/logs`: runs `docker logs -f` against the serve container, with keepalive heartbeats. Streams both stdout and stderr interleaved.

**`PATCH /api/serve`**
```json
{
  "auto_rebuild": true
}
```
Toggles auto-rebuild on a running session. If turning on, starts the file watcher inside the container. If turning off, stops the watcher (process continues running without rebuild).

### Route Registration

In `internal/apicontract/routes.go`:

```go
{Method: http.MethodGet,    Pattern: "/api/serve",          Name: "GetServe",          Description: "Get current serve session state."},
{Method: http.MethodPost,   Pattern: "/api/serve/discover",  Name: "ServeDiscover",     Description: "Launch discovery agent to detect build/run commands."},
{Method: http.MethodDelete, Pattern: "/api/serve/discover",  Name: "CancelDiscover",    Description: "Cancel active discovery agent."},
{Method: http.MethodPost,   Pattern: "/api/serve",          Name: "StartServe",        Description: "Start serve session with confirmed config."},
{Method: http.MethodDelete, Pattern: "/api/serve",          Name: "StopServe",         Description: "Stop running serve session."},
{Method: http.MethodPatch,  Pattern: "/api/serve",          Name: "UpdateServe",       Description: "Update serve session (toggle auto-rebuild)."},
{Method: http.MethodGet,    Pattern: "/api/serve/logs",     Name: "ServeLog",          Description: "SSE: stream serve container output."},
```

---

## Container Execution

### Serve Container

The serve container reuses the existing sandbox image (Claude image) but with a different entrypoint. Instead of launching the Claude CLI, it runs the user's build+run commands.

**Container spec** (assembled via existing `buildBaseContainerSpec` patterns):

```
podman run --rm
  --name wallfacer-serve-<uuid8>
  --label wallfacer.serve.id=<session-uuid>
  --network <WALLFACER_CONTAINER_NETWORK>
  --cpus <WALLFACER_CONTAINER_CPUS>
  --memory <WALLFACER_CONTAINER_MEMORY>
  -p <host-port>:<container-port>            # port forwarding for web servers
  -v <workspace-or-worktree>:/workspace/<basename>
  -w /workspace/<basename>/<work_dir>
  [-e KEY=VALUE ...]                         # from config.Env
  --entrypoint /bin/bash
  <claude-image>
  -c "<build_cmd> && exec <run_cmd>"
```

Key differences from task containers:
- **Port forwarding** (`-p`): Exposes the configured port to the host. Task containers never expose ports.
- **Custom entrypoint**: Overrides the Claude CLI entrypoint with `/bin/bash -c`.
- **No agent flags**: No `--verbose`, `--output-format`, `--resume`.
- **No API tokens needed**: The `.env` file is NOT mounted (no LLM calls). Only user-specified `config.Env` vars are passed.
- **Longer lifetime**: Runs until explicitly stopped, not until agent ends a turn.

### Discovery Container

A short-lived agent container that reads the codebase and outputs a JSON config.

```
podman run --rm
  --name wallfacer-discover-<uuid8>
  --label wallfacer.serve.discover=true
  --env-file ~/.wallfacer/.env
  -v <workspace>:/workspace/<basename>:ro     # read-only mount
  -w /workspace/<basename>
  <claude-image>
  -p "<discovery-prompt>" --verbose --output-format stream-json
```

The discovery prompt is rendered from `prompts/serve-discover.tmpl` and instructs the agent to:
1. List and inspect build files (`Makefile`, `package.json`, `go.mod`, `Cargo.toml`, `pyproject.toml`, `docker-compose.yml`).
2. Determine the most appropriate build and run commands.
3. Identify the primary port (if any).
4. Output exactly one JSON block: `{"build_cmd": "...", "run_cmd": "...", "port": N, "env": {...}}`.

The runner parses the agent's output, extracts the JSON block, and stores it as the proposed config.

### Auto-Rebuild Mode

When `auto_rebuild` is enabled, the container command changes to:

```bash
# Build once, then watch for changes and rebuild
<build_cmd> && exec <run_cmd>
```

The auto-rebuild is implemented via a wrapper script injected into the container:

```bash
#!/bin/bash
set -e

build_and_run() {
    <build_cmd>
    <run_cmd> &
    APP_PID=$!
}

build_and_run

# Watch for file changes (inotifywait or fswatch)
while inotifywait -r -e modify,create,delete /workspace/<basename> \
    --exclude '(\.git|node_modules|__pycache__|\.next)'; do
    echo "[wallfacer-serve] Change detected, rebuilding..."
    kill $APP_PID 2>/dev/null || true
    wait $APP_PID 2>/dev/null || true
    build_and_run
done
```

The `inotifywait` tool (from `inotify-tools`) must be available in the sandbox image. If not present, auto-rebuild degrades gracefully: the option is hidden in the UI and the API returns an error when toggling it on.

---

## UI

### Toolbar Button

Add a "Serve" button to the main toolbar (alongside existing workspace/settings controls). The button shows:
- **Idle state**: Play icon + "Serve"
- **Discovering state**: Spinner + "Detecting..."
- **Running state**: Green dot + "Serving on :PORT" (clickable to open in browser)
- **Failed state**: Red dot + "Serve failed"

Clicking the button when idle opens the serve modal. Clicking when running opens the log viewer.

### Serve Modal

A modal with three sections:

1. **Scope selector**: Radio buttons for "Workspace" (default) or "Task". When "Task" is selected, a dropdown lists tasks with worktrees (in_progress, waiting, done, failed states).

2. **Config editor** (shown after discovery or from cache):
   - Build command (text input, monospace)
   - Run command (text input, monospace)
   - Port (number input)
   - Environment variables (key-value editor)
   - Working directory (text input, relative path)
   - Auto-rebuild toggle (checkbox)

3. **Action buttons**: "Detect Commands" (runs discovery), "Start", "Stop", "Open in Browser" (when port is configured and session is running).

### Log Panel

When a serve session is running, a log panel can be opened from the toolbar button or the serve modal. It uses the same SSE streaming pattern as task log viewing:

- Connects to `GET /api/serve/logs`
- Auto-scrolls to bottom
- ANSI color rendering (reuses existing `ansiToHtml` utility if present)
- "Clear" button to reset visible log (client-side only)
- "Stop" button to kill the session

### SSE Integration

The serve session state is broadcast via the existing task SSE stream (`GET /api/tasks/stream`) as a new event type:

```json
{
  "type": "serve",
  "data": { /* ServeSession */ }
}
```

This lets the toolbar button update reactively without polling.

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_SERVE_AUTO_REBUILD` | `false` | Enable auto-rebuild by default for new serve sessions |
| `WALLFACER_SERVE_HOST_PORT` | `0` | Host port for port forwarding (0 = auto-assign) |
| `WALLFACER_SERVE_TIMEOUT` | `0` | Auto-stop timeout in minutes (0 = no timeout) |

### Cached Discovery Configs

Stored at `~/.wallfacer/serve-configs/<fingerprint>.json`:

```json
{
  "fingerprint": "sha256:<hex>",
  "detected_at": "2026-03-25T10:00:00Z",
  "config": {
    "build_cmd": "go build -o server .",
    "run_cmd": "./server -addr :8080",
    "port": 8080,
    "env": {}
  }
}
```

The fingerprint is computed from the sorted workspace paths (same algorithm as AGENTS.md fingerprinting). Cached configs are reused until the user explicitly re-runs discovery.

---

## Implementation Phases

### Phase 1 — Data model + storage

| File | Change |
|------|--------|
| `internal/store/models.go` | Add `ServeSession`, `ServeStatus`, `ServeScope`, `ServeConfig` types |
| `internal/store/serve.go` (new) | `SaveServeSession`, `GetServeSession`, `DeleteServeSession` |
| `internal/store/serve_test.go` (new) | Round-trip persistence tests |

**Effort:** Low. New types and file I/O following existing patterns.

### Phase 2 — Serve container runner

| File | Change |
|------|--------|
| `internal/runner/serve.go` (new) | `StartServe`, `StopServe`, `ServeContainerName` methods on Runner |
| `internal/runner/serve.go` | Container spec assembly: mounts, port forwarding, entrypoint override |
| `internal/runner/serve.go` | Container registry tracking for serve containers (separate from task containers) |
| `internal/runner/serve_test.go` (new) | Unit tests for container arg assembly, port mapping |

**Effort:** Medium. Reuses `buildBaseContainerSpec` patterns but introduces port forwarding and custom entrypoints.

### Phase 3 — Discovery agent

| File | Change |
|------|--------|
| `prompts/serve-discover.tmpl` (new) | System prompt template for command discovery agent |
| `internal/runner/serve_discover.go` (new) | `RunDiscovery` — launches short-lived agent, parses JSON output |
| `internal/runner/serve_discover.go` | Config caching logic (fingerprint → JSON file) |
| `internal/runner/serve_discover_test.go` (new) | Test JSON extraction from mock agent output |

**Effort:** Medium. Agent container launch reuses existing `runContainer` patterns; JSON extraction from NDJSON is new.

### Phase 4 — API endpoints + handler

| File | Change |
|------|--------|
| `internal/apicontract/routes.go` | Register 7 new routes |
| `internal/handler/serve.go` (new) | `GetServe`, `StartServe`, `StopServe`, `UpdateServe`, `ServeDiscover`, `CancelDiscover`, `ServeLogs` handlers |
| `server.go` | Wire handlers in `buildMux` |
| `internal/handler/serve_test.go` (new) | Handler tests for each endpoint |

**Effort:** Medium. Follows existing handler patterns (`oversight.go`, `stream.go`).

### Phase 5 — Auto-rebuild support

| File | Change |
|------|--------|
| `internal/runner/serve.go` | Auto-rebuild wrapper script generation |
| `internal/runner/serve.go` | `inotifywait` availability check |
| `sandbox/claude/Dockerfile` | Install `inotify-tools` package |

**Effort:** Low. Shell script injection into container command.

### Phase 6 — UI

| File | Change |
|------|--------|
| `ui/js/serve.js` (new) | Serve modal, config editor, log panel, SSE listener |
| `ui/js/app.js` | Toolbar button integration, serve state management |
| `ui/css/styles.css` | Serve button states, modal styling, log panel |
| `ui/index.html` | Serve modal template, toolbar button |
| `ui/js/generated/routes.js` | Regenerated via `make api-contract` |

**Effort:** Medium. New modal + log viewer, but follows existing patterns (refinement modal, task log viewer).

### Phase 7 — SSE integration + polish

| File | Change |
|------|--------|
| `internal/handler/stream.go` | Emit `serve` events on session state changes |
| `ui/js/serve.js` | Handle `serve` SSE events, update toolbar reactively |
| `internal/store/serve.go` | Notify mechanism for serve session changes |

**Effort:** Low. SSE delta pattern is well-established.

### Phase 8 — Tests, docs, contract regeneration

| File | Change |
|------|--------|
| `internal/apicontract/` | Regenerate `make api-contract` |
| `docs/guide/configuration.md` | Document `WALLFACER_SERVE_*` env vars |
| `docs/guide/board-and-tasks.md` | Document serve feature in task workflow |
| `CLAUDE.md` | Add serve routes to API Routes section |

**Effort:** Low.

---

## Key Patterns Reused

| Pattern | Source | Reused For |
|---------|--------|------------|
| `buildBaseContainerSpec` | `internal/runner/container.go` | Serve container volume mounts, resource limits |
| `containerRegistry` | `internal/runner/registry.go` | Tracking serve container name for log streaming and cleanup |
| `streamLines` + keepalive | `internal/handler/stream.go` | `GET /api/serve/logs` SSE streaming |
| `SSE delta broadcast` | `internal/handler/stream.go` | Serve session state updates via task SSE stream |
| `runContainer` | `internal/runner/execute.go` | Discovery agent container launch |
| Workspace fingerprint | `internal/instructions/instructions.go` | Config cache keying |
| Refinement agent pattern | `internal/runner/refine.go` | Discovery agent: short-lived agent with structured output |
| `SaveOversight`/`GetOversight` | `internal/store/oversight.go` | `SaveServeSession`/`GetServeSession` file I/O |
| Settings modal pattern | `ui/js/settings.js` | Serve config editor modal |

---

## Potential Challenges

1. **Port conflicts**: If the user's app listens on a port already in use on the host, the container fails to start. Mitigate by defaulting `WALLFACER_SERVE_HOST_PORT=0` (auto-assign) and reporting the actual port in the session state. The UI shows the assigned port.

2. **Container networking**: Port forwarding (`-p`) requires bridge networking. If `WALLFACER_CONTAINER_NETWORK=none`, port forwarding won't work. The serve handler should validate network config before starting and return a clear error.

3. **inotifywait availability**: The `inotify-tools` package may not be installed in the sandbox image. Phase 5 adds it to the Dockerfile, but users with custom images may lack it. Auto-rebuild should degrade gracefully: check for the binary at session start and disable the option if missing.

4. **Long-running container cleanup**: Unlike task containers that exit after the agent finishes, serve containers run indefinitely. If the wallfacer server crashes or restarts, orphaned serve containers remain. Mitigate with:
   - Label-based cleanup on server startup: scan for `wallfacer.serve.id` labels and kill orphans.
   - Optional `WALLFACER_SERVE_TIMEOUT` auto-stop.

5. **Discovery agent reliability**: The agent may produce incorrect or incomplete build/run commands. The user-review step mitigates this — the modal always shows the proposed config for editing before starting. Cached configs skip the agent entirely on repeat runs.

6. **Worktree scope vs. workspace scope**: When targeting a task's worktrees, the worktree must exist (task must have been started at least once). The API should validate this and return a clear error if worktrees haven't been set up yet.

7. **Resource contention**: A serve container competes with task containers for CPU/memory. Consider separate resource limits (`WALLFACER_SERVE_CPUS`, `WALLFACER_SERVE_MEMORY`) or sharing the existing limits.

---

## Migration & Backward Compatibility

- **Additive only**: No changes to existing data models or API contracts.
- **New routes**: All under `/api/serve/`, no collision with existing routes.
- **New storage**: `~/.wallfacer/serve/` and `~/.wallfacer/serve-configs/` are created on first use.
- **Dockerfile change**: Adding `inotify-tools` to the Claude sandbox image is backward-compatible (existing tasks unaffected).
- **UI**: Toolbar button is new; no existing UI elements are modified.

---

## Open Questions

1. **Multiple concurrent serve sessions?** This spec assumes one active session at a time (simplest UX). Supporting multiple sessions (e.g., frontend + backend) would require session IDs in the UI and more complex lifecycle management. Deferred to v2.

2. **Health checks?** If a port is configured, the serve runner could periodically `curl` the port and report health status. Useful but adds complexity. Deferred to v2.

3. **Port forwarding vs. host networking?** On Linux with `--network=host`, no `-p` flag is needed — the app's port is directly accessible. The implementation should detect host networking and skip port mapping. On macOS (Podman machine), `-p` is always required.

---

## What This Does NOT Require

- No changes to the task execution pipeline or turn loop.
- No changes to the AI agent prompts or sandbox configuration (except the new discovery template).
- No changes to worktree management — serve sessions use existing worktrees or workspace paths as-is.
- No new external dependencies beyond `inotify-tools` in the container image (optional, for auto-rebuild only).
