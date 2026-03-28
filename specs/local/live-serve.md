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

### Secrets and Environment

Application secrets (database URLs, API keys for third-party services, etc.) are separate from LLM tokens. The spec introduces a dedicated `~/.wallfacer/serve.env` file for app-level environment variables. This file:

- Is mounted into serve containers via `--env-file` (never into task/agent containers).
- Is editable from the serve modal UI ("Environment File" tab).
- Is NOT the same as `~/.wallfacer/.env` (which holds LLM tokens and wallfacer config).
- Supports per-session overrides via `config.Env` (key-value pairs in the serve config take precedence).

This separation ensures LLM credentials never leak into user application processes, while giving applications access to the secrets they need (database passwords, AWS keys, etc.).

### Persistent Volumes

Serve containers run with `--rm`, so non-mounted paths are lost on stop. To support stateful applications (SQLite databases, upload directories, build caches), `ServeConfig` includes an optional `volumes` map of host-path → container-path bind mounts. These are mounted read-write alongside the workspace mount.

A named volume `wallfacer-serve-data` is also created automatically and mounted at `/data` inside serve containers, providing a default persistent storage location that survives container restarts without requiring explicit configuration.

### Connected Resources (v1: Network Access, v2: Service Orchestration)

**v1 scope**: The serve container joins the configured container network (`WALLFACER_CONTAINER_NETWORK`), giving it access to any services the user has started separately (e.g., `docker run -d --name postgres ...` on the same network). The discovery agent is extended to detect `docker-compose.yml` / `compose.yaml` and propose a `pre_cmd` that starts dependencies before the main app.

**v2 extension point**: Full service orchestration (starting/stopping dependent containers as part of the serve lifecycle) is deferred. The data model includes a `services` field on `ServeConfig` (initially unused) to reserve the schema space. See Open Questions.

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
    BuildCmd string            `json:"build_cmd"`          // e.g. "go build -o server ."
    RunCmd   string            `json:"run_cmd"`            // e.g. "./server -addr :8080"
    PreCmd   string            `json:"pre_cmd,omitempty"`  // runs before build (e.g. "docker compose up -d postgres redis")
    Port     int               `json:"port,omitempty"`     // primary port to expose/health-check
    Env      map[string]string `json:"env,omitempty"`      // per-session env var overrides (on top of serve.env)
    Volumes  map[string]string `json:"volumes,omitempty"`  // host-path → container-path bind mounts for persistent data
    WorkDir  string            `json:"work_dir,omitempty"` // relative path within workspace

    // v2 extension point — not implemented in v1.
    // Services defines dependent containers to start/stop with the serve session.
    // Example: [{"name":"postgres","image":"postgres:16","port":5432,"env":{"POSTGRES_PASSWORD":"dev"}}]
    Services []ServeService `json:"services,omitempty"`
}

// ServeService defines a dependent container managed alongside the serve session.
// Reserved for v2 service orchestration — not implemented in v1.
type ServeService struct {
    Name    string            `json:"name"`              // container name suffix (e.g. "postgres")
    Image   string            `json:"image"`             // container image (e.g. "postgres:16")
    Port    int               `json:"port,omitempty"`    // port to expose
    Env     map[string]string `json:"env,omitempty"`     // env vars for the service container
    Volumes map[string]string `json:"volumes,omitempty"` // persistent mounts for service data
}
```

### Storage

Serve sessions are stored in `~/.wallfacer/serve/`:
- `~/.wallfacer/serve/<session-uuid>.json` — session state
- `~/.wallfacer/serve-configs/<workspace-fingerprint>.json` — cached discovery results
- `~/.wallfacer/serve.env` — app-level secrets and environment variables (shared across all sessions)

A named Docker/Podman volume `wallfacer-serve-data` provides default persistent storage, mounted at `/data` inside serve containers.

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
| `GET` | `/api/serve/env` | Get app-level serve.env content (values masked) |
| `PUT` | `/api/serve/env` | Update app-level serve.env content |

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
    "pre_cmd": "docker compose up -d postgres",
    "build_cmd": "go build -o server .",
    "run_cmd": "./server -addr :3000",
    "port": 3000,
    "env": { "GIN_MODE": "debug" },
    "volumes": { "./data": "/app/data" }
  },
  "auto_rebuild": false
}
```
Response: `200 OK` with `ServeSession`. App-level secrets from `~/.wallfacer/serve.env` are injected automatically; per-session `config.Env` overrides take precedence.

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
{Method: http.MethodGet,    Pattern: "/api/serve/env",      Name: "GetServeEnv",       Description: "Get app-level serve.env (values masked)."},
{Method: http.MethodPut,    Pattern: "/api/serve/env",      Name: "UpdateServeEnv",    Description: "Update app-level serve.env content."},
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
  --cpus <WALLFACER_SERVE_CPUS>
  --memory <WALLFACER_SERVE_MEMORY>
  -p <host-port>:<container-port>            # port forwarding for web servers
  --env-file ~/.wallfacer/serve.env          # app-level secrets (if file exists)
  [-e KEY=VALUE ...]                         # per-session overrides from config.Env
  -v <workspace-or-worktree>:/workspace/<basename>
  -v wallfacer-serve-data:/data              # named volume for persistent storage
  [-v <host-path>:<container-path> ...]      # user-defined volumes from config.Volumes
  -w /workspace/<basename>/<work_dir>
  --entrypoint /bin/bash
  <claude-image>
  -c "<pre_cmd> ; <build_cmd> && exec <run_cmd>"
```

Key differences from task containers:
- **Port forwarding** (`-p`): Exposes the configured port to the host. Task containers never expose ports.
- **Custom entrypoint**: Overrides the Claude CLI entrypoint with `/bin/bash -c`.
- **No agent flags**: No `--verbose`, `--output-format`, `--resume`.
- **App secrets, not LLM tokens**: Mounts `serve.env` (app-level) instead of `.env` (LLM tokens). Per-session `config.Env` overrides are passed via `-e` flags.
- **Persistent data volume**: Named volume `wallfacer-serve-data` at `/data` plus optional user-defined bind mounts.
- **Pre-command**: Optional `pre_cmd` runs before build (e.g., starting dependent services via compose).
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
4. Detect dependent services: look for `docker-compose.yml`/`compose.yaml`, database connection strings in config files, and propose a `pre_cmd` to start them (e.g., `docker compose up -d postgres redis`).
5. Identify required environment variables: scan for `os.Getenv`, `process.env`, `.env.example`, `config.yaml`, etc. and list variables the app expects (without guessing secret values).
6. Detect data directories: look for configured storage paths, upload dirs, SQLite file paths, and propose `volumes` mappings.
7. Output exactly one JSON block matching `ServeConfig` schema: `{"build_cmd": "...", "run_cmd": "...", "pre_cmd": "...", "port": N, "env": {...}, "volumes": {...}}`.

The runner parses the agent's output, extracts the JSON block, and stores it as the proposed config. The `env` field from discovery contains only variable *names* with empty/placeholder values — the user fills in actual secrets in the serve modal.

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

2. **Config editor** (shown after discovery or from cache), organized as sub-tabs:
   - **Commands**: Pre-command, build command, run command (text inputs, monospace), working directory, auto-rebuild toggle.
   - **Network**: Port (number input), host port override.
   - **Environment**: Key-value editor for per-session `config.Env` overrides. "Edit serve.env" button opens a text editor for the shared `~/.wallfacer/serve.env` file (same pattern as AGENTS.md editor). Discovery-detected variable names shown as hints with empty values for the user to fill in.
   - **Volumes**: Key-value editor for `config.Volumes` (host path → container path). The default `/data` volume is shown as a read-only entry. "Add volume" button for custom mounts.
   - **Services** (v2, greyed out): Placeholder tab showing detected `docker-compose.yml` services. Informational only in v1 — displays a note that service orchestration is planned.

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
| `WALLFACER_SERVE_CPUS` | (inherits `WALLFACER_CONTAINER_CPUS`) | CPU limit for serve containers |
| `WALLFACER_SERVE_MEMORY` | (inherits `WALLFACER_CONTAINER_MEMORY`) | Memory limit for serve containers |

### App-Level Secrets (`~/.wallfacer/serve.env`)

A dedicated env file for application secrets, separate from the LLM token `.env`. Example:

```env
DATABASE_URL=postgres://user:pass@postgres:5432/mydb
REDIS_URL=redis://redis:6379
AWS_ACCESS_KEY_ID=AKIA...
AWS_SECRET_ACCESS_KEY=...
SESSION_SECRET=random-string-here
```

This file is:
- Mounted via `--env-file` into serve containers only (never into task/agent containers).
- Editable from the serve modal UI.
- Excluded from discovery cache (secrets are never written to `serve-configs/*.json`).
- Created empty on first serve session if it doesn't exist.

Per-session `config.Env` overrides take precedence over `serve.env` values for the same key.

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
| `internal/apicontract/routes.go` | Register 9 new routes |
| `internal/handler/serve.go` (new) | `GetServe`, `StartServe`, `StopServe`, `UpdateServe`, `ServeDiscover`, `CancelDiscover`, `ServeLogs`, `GetServeEnv`, `UpdateServeEnv` handlers |
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

7. **Resource contention**: A serve container competes with task containers for CPU/memory. Mitigated with dedicated `WALLFACER_SERVE_CPUS` and `WALLFACER_SERVE_MEMORY` env vars that default to the general container limits but can be tuned independently.

8. **Secret leakage via discovery cache**: The discovery agent may detect environment variable names from the codebase. The cached config must store only variable *names* (with empty values), never actual secrets. Actual values live exclusively in `~/.wallfacer/serve.env` and per-session `config.Env`. The cache serializer must strip non-empty values before writing.

9. **Volume path validation**: User-supplied `config.Volumes` host paths could mount sensitive host directories into the container. The handler should validate that host paths are within the workspace tree or a wallfacer-managed directory. Paths outside these boundaries require explicit confirmation.

10. **Serve.env file permissions**: `~/.wallfacer/serve.env` contains plaintext secrets. The file should be created with `0600` permissions. The UI editor should warn users that secrets are stored in plaintext on disk.

11. **Dependent service lifecycle**: In v1, `pre_cmd` (e.g., `docker compose up -d`) starts services but doesn't stop them when the serve session ends. Orphaned service containers accumulate. The v2 service orchestration design should track started services and tear them down on session stop. For v1, document that users manage service lifecycle manually or via compose.

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

4. **Service orchestration (v2)?** Full lifecycle management of dependent containers (databases, caches, queues). The `ServeService` type is reserved in the data model. v2 would: start service containers before the app, stop them on session end, stream their logs alongside the app, and expose their ports. Requires answering: shared volume for service data? Health-check dependencies (wait for Postgres to be ready before starting app)? Per-service resource limits?

5. **Authentication proxy (v2)?** For web apps that require login, Wallfacer could inject a reverse proxy (e.g., Caddy) in front of the serve container that handles OAuth/OIDC, mTLS, or basic auth. This would let users test authenticated flows without configuring auth in the app itself. The proxy would run as a sidecar container on the same network. Alternatively, the serve container could expose a tunnel URL (like ngrok/cloudflared) for testing webhooks and external integrations. Both are v2 — v1 exposes the raw app port.

6. **Secret rotation and vault integration (v2)?** For production-like testing, `serve.env` with plaintext secrets is adequate. But teams may want to pull secrets from HashiCorp Vault, AWS Secrets Manager, or 1Password CLI. v2 could support a `secret_cmd` field in `ServeConfig` that runs before the app and populates env vars dynamically (e.g., `op run --env-file=.env.tpl --`). The `serve.env` file would then contain references (`op://vault/item/field`) rather than plaintext values.

7. **Persistent volume snapshots (v2)?** The named `wallfacer-serve-data` volume persists across sessions, but there's no way to reset it to a known state. v2 could support volume snapshots: save the current state before a test run, restore on failure. Useful for database migration testing.

---

## What This Does NOT Require

- No changes to the task execution pipeline or turn loop.
- No changes to the AI agent prompts or sandbox configuration (except the new discovery template).
- No changes to worktree management — serve sessions use existing worktrees or workspace paths as-is.
- No new external dependencies beyond `inotify-tools` in the container image (optional, for auto-rebuild only).
