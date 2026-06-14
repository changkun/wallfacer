---
title: "Live Serve - Build and Run Developed Software from Wallfacer"
status: drafted
depends_on: []
affects:
  - internal/store/models.go
  - internal/runner/serve.go
  - internal/handler/serve.go
  - internal/prompts/serve-discover.tmpl
  - internal/apicontract/routes.go
  - frontend/src/components/ServePanel.vue
effort: large
created: 2026-03-25
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Live Serve - Build and Run Developed Software from Wallfacer

---

## Problem

After an AI agent completes a task, there is no way to verify that the software actually builds and runs correctly without leaving Wallfacer. Users must open a terminal, navigate to the worktree or workspace, figure out the correct build/run commands, and manually test. This breaks the feedback loop, especially for web apps, servers, and CLI tools where "does it start?" is the first validation step.

There is also no way to keep a development server running while iterating across multiple tasks. Each time a task modifies code, the user must manually restart the server to see the changes.

---

## Current State (as of 2026-06-14)

- **Task execution**: Runs the agent CLI (Claude/Codex) directly as a host process via `internal/executor` (`HostBackend`). There are no containers. The agent reads/writes code but never builds or runs the resulting software.
- **Process lifecycle**: Each task turn launches one host process (an `os/exec.Cmd` wrapped by `executor.Handle`), torn down after the agent finishes the turn.
- **Worktrees**: Per-task git worktrees provide isolated copies of the codebase on the host (under `<configDir>/worktrees/`). The host process runs with `cmd.Dir` set to the worktree (or workspace) path. See `internal/runner/worktree.go` and `internal/runner/container.go` (`buildHostSpec`, `buildBaseContainerSpec`).
- **Log streaming**: `GET /api/tasks/{id}/logs` streams the agent process output via SSE, backed by `internal/pkg/livelog`. Works for agent output, not for arbitrary processes.
- **Env file**: The agent process inherits a merged environment built from `executor.ContainerSpec.EnvFile` (the wallfacer `.env` with LLM tokens, default `<configDir>/.env`) plus `ContainerSpec.Env` overlays. There is no separate app-level env file.
- **No**: Dev server, build pipeline, long-lived process manager, or any way to run user code as part of a serve session.

> Architecture note: an earlier draft of this spec assumed a container runtime
> (Podman/Docker, `--rm`, `-p` port forwarding, `--env-file` mounts, named
> volumes). That model is gone. Tasks now run as **host processes inside git
> worktrees**. This refine reframes the entire design around host execution:
> no containers, no port mapping (host processes bind host ports directly),
> no bind mounts or named volumes (the worktree/workspace path is already on
> the host filesystem), and no image to extend for tooling.

---

## Design

### Scope: Workspace + Task

A "serve session" targets either:
1. **Workspace directories** (default) - runs against the main workspace copy. Useful for verifying the combined result of multiple tasks after merging.
2. **A specific task's worktree** - runs against the task's isolated branch worktree. Useful for testing a single task's changes before committing.

The user selects the target when starting a serve session. The UI defaults to workspace scope. The resolved working directory is the host path of the workspace (or the task's worktree), the same path resolution `buildHostSpec` already performs.

### Discovery: Agent-Driven Command Detection

Rather than requiring users to configure build/run commands manually, an agent reads the codebase and proposes commands. The flow:

1. User clicks "Serve" (toolbar/sidebar action or task action).
2. Wallfacer launches a short-lived discovery agent process that reads the target directory and outputs a JSON config: `{ "build_cmd": "...", "run_cmd": "...", "port": N, "env": {...} }`.
3. The proposed config is shown in the serve panel for the user to review and edit.
4. User confirms, then the serve session starts.

The discovery agent uses a dedicated system prompt template (`internal/prompts/serve-discover.tmpl`) that instructs it to look for `Makefile`, `package.json`, `go.mod`, `Cargo.toml`, `docker-compose.yml`, etc., and emit structured JSON. It is launched the same way as the existing ephemeral ideation run (`internal/runner/ideate.go`, `runIdeationEphemeral`): a one-shot host process whose stdout is parsed for a JSON block.

Previously-confirmed configs are cached per workspace fingerprint in `<configDir>/serve-configs/` so the agent step is skipped on repeat runs. The fingerprint reuses `prompts.InstructionsKey(workspaces)` (sorted-paths SHA256, 16 hex chars), the same keying AGENTS.md uses.

### Secrets and Environment

Application secrets (database URLs, API keys for third-party services, etc.) are separate from LLM tokens. The spec introduces a dedicated `<configDir>/serve.env` file for app-level environment variables. This file:

- Is supplied to serve processes as the `ContainerSpec.EnvFile` (never to task/agent processes, which use the wallfacer `.env`).
- Is editable from the serve panel UI ("Environment" tab), the same pattern as the AGENTS.md/instructions editor.
- Is NOT the same as `<configDir>/.env` (which holds LLM tokens and wallfacer config).
- Supports per-session overrides via `config.Env` (key-value pairs in the serve config, applied as `ContainerSpec.Env`, which wins on collision).

This separation ensures LLM credentials never leak into user application processes, while giving applications access to the secrets they need (database passwords, AWS keys, etc.).

> Mechanism note: under the host backend the env file is not "mounted". The
> serve runner sets `ContainerSpec.EnvFile = <configDir>/serve.env` and
> `ContainerSpec.Env = config.Env`; `HostBackend.buildChildEnv` merges the
> file then overlays `Env` (overlay wins). This is exactly how the agent
> process already receives `<configDir>/.env`.

### Working Directory and Persistent State

Because serve processes run directly on the host inside the worktree/workspace, there are no container bind mounts or named volumes to configure. State written by the app (SQLite files, upload directories, build caches) lands on the host filesystem under the working directory and persists naturally across serve restarts.

`ServeConfig` keeps an optional `work_dir` (a path relative to the resolved workspace/worktree root) so apps that live in a subdirectory (e.g. a `frontend/` or `cmd/server/`) can set their own CWD. There is no `volumes` field in the host model.

### Connected Resources

The serve process runs on the host, so it can reach any service the user has already started on the host (a local Postgres, Redis, a `docker compose` stack the user manages out-of-band). The discovery agent is extended to detect `docker-compose.yml` / `compose.yaml` and propose a `pre_cmd` that starts dependencies before the main app (e.g. `docker compose up -d postgres redis`).

Full service orchestration (Wallfacer starting/stopping dependent services as part of the serve lifecycle) is out of scope here. The data model includes a `services` field on `ServeConfig` (initially unused) to reserve the schema space. See Open Questions.

### Auto-Rebuild: Opt-In

By default, the serve session runs the build+run command once. When `WALLFACER_SERVE_AUTO_REBUILD=true` (or toggled via the UI), file changes under the working directory trigger an automatic rebuild+restart cycle.

Because the serve process is a host process, the file watcher runs on the host (Go-side `fsnotify` in the serve runner) rather than via an in-container `inotifywait`. On a change, the runner kills the running app process and re-runs `build_cmd` + `run_cmd`. This removes the earlier dependency on adding `inotify-tools` to a sandbox image (there is no image).

---

## Data Model

### ServeSession

New type in `internal/store/models.go`:

```go
// ServeSession represents a running or completed live-serve session.
type ServeSession struct {
    ID          uuid.UUID   `json:"id"`
    Status      ServeStatus `json:"status"`            // "discovering", "pending", "running", "stopped", "failed"
    Scope       ServeScope  `json:"scope"`             // "workspace" or "task"
    TaskID      *uuid.UUID  `json:"task_id,omitempty"` // set when scope=task
    Config      ServeConfig `json:"config"`
    ProcessName string      `json:"process_name,omitempty"` // executor.Handle name for the serve process
    Port        int         `json:"port,omitempty"`         // resolved listen port (echoed from config for the UI link)
    StartedAt   *time.Time  `json:"started_at,omitempty"`
    StoppedAt   *time.Time  `json:"stopped_at,omitempty"`
    Error       string      `json:"error,omitempty"`
    AutoRebuild bool        `json:"auto_rebuild"`
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
    Port     int               `json:"port,omitempty"`     // primary port the app listens on (for the UI "open" link)
    Env      map[string]string `json:"env,omitempty"`      // per-session env var overrides (overlaid on serve.env)
    WorkDir  string            `json:"work_dir,omitempty"` // relative path within the workspace/worktree root

    // Reserved extension point - not implemented in v1.
    // Services defines dependent services Wallfacer would start/stop with the
    // serve session. Unused until service orchestration is designed.
    // Example: [{"name":"postgres","image":"postgres:16","port":5432}]
    Services []ServeService `json:"services,omitempty"`
}

// ServeService is reserved for a future service-orchestration design and is
// not implemented in v1.
type ServeService struct {
    Name  string            `json:"name"`
    Image string            `json:"image,omitempty"`
    Port  int               `json:"port,omitempty"`
    Env   map[string]string `json:"env,omitempty"`
}
```

Note vs. the earlier draft: the `volumes` field and the container-only
`ContainerName` field are removed (no bind mounts, no container). `ProcessName`
replaces `ContainerName` and holds the `executor.Handle` name used to stream
logs and to stop the process.

### Storage

Serve sessions are stored under the wallfacer config dir:
- `<configDir>/serve/<session-uuid>.json` - session state
- `<configDir>/serve-configs/<fingerprint>.json` - cached discovery results
- `<configDir>/serve.env` - app-level secrets and environment variables (shared across sessions)

This is separate from per-task data because serve sessions are workspace-scoped, not task-scoped. There is no persistent volume to manage; app state lives on the host under the working directory.

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
| `GET` | `/api/serve/logs` | SSE stream of serve process output |
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
Response: `202 Accepted`, discovery agent starts. Poll `GET /api/serve` for the `status: "discovering"` then `"pending"` transition with proposed `config`.

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
    "work_dir": "cmd/server"
  },
  "auto_rebuild": false
}
```
Response: `200 OK` with `ServeSession`. App-level secrets from `<configDir>/serve.env` are supplied automatically as the process env file; per-session `config.Env` overrides take precedence.

**`DELETE /api/serve`**
Stops the running process and moves the session to `"stopped"`. Response: `200 OK`.

**`GET /api/serve/logs`**
SSE stream. Same pattern as `GET /api/tasks/{id}/logs`: tails the serve process output via `internal/pkg/livelog`, with keepalive heartbeats. Streams stdout and stderr interleaved.

**`PATCH /api/serve`**
```json
{
  "auto_rebuild": true
}
```
Toggles auto-rebuild on a running session. If turning on, starts the host file watcher. If turning off, stops the watcher (the app keeps running without rebuild-on-change).

### Route Registration

In `internal/apicontract/routes.go`:

```go
{Method: http.MethodGet,    Pattern: "/api/serve",          Name: "GetServe",       Description: "Get current serve session state."},
{Method: http.MethodPost,   Pattern: "/api/serve/discover", Name: "ServeDiscover",  Description: "Launch discovery agent to detect build/run commands."},
{Method: http.MethodDelete, Pattern: "/api/serve/discover", Name: "CancelDiscover", Description: "Cancel active discovery agent."},
{Method: http.MethodPost,   Pattern: "/api/serve",          Name: "StartServe",     Description: "Start serve session with confirmed config."},
{Method: http.MethodDelete, Pattern: "/api/serve",          Name: "StopServe",      Description: "Stop running serve session."},
{Method: http.MethodPatch,  Pattern: "/api/serve",          Name: "UpdateServe",    Description: "Update serve session (toggle auto-rebuild)."},
{Method: http.MethodGet,    Pattern: "/api/serve/logs",     Name: "ServeLog",       Description: "SSE: stream serve process output."},
{Method: http.MethodGet,    Pattern: "/api/serve/env",      Name: "GetServeEnv",    Description: "Get app-level serve.env (values masked)."},
{Method: http.MethodPut,    Pattern: "/api/serve/env",      Name: "UpdateServeEnv", Description: "Update app-level serve.env content."},
```

---

## Process Execution

### Serve Process

The serve process is a plain host process launched through the existing `executor.Backend` (`HostBackend`), the same backend that runs agent turns. Instead of an agent argv, the command is a shell that runs the user's build+run commands.

**Launch spec** (assembled via the existing `buildBaseContainerSpec` and the host-mode path in `internal/runner/container.go`):

```go
spec := executor.ContainerSpec{
    Name:    "wallfacer-serve-" + shortID,            // executor.Handle name
    Labels:  map[string]string{"wallfacer.serve.id": session.ID.String()},
    EnvFile: filepath.Join(configDir, "serve.env"),   // app-level secrets (if present)
    Env:     config.Env,                              // per-session overrides (win on collision)
    WorkDir: resolvedWorkDir,                         // host path: <workspace-or-worktree>/<work_dir>
    Cmd:     []string{"/bin/bash", "-c", script},     // pre_cmd ; build_cmd && exec run_cmd
}
handle, err := backend.Launch(ctx, spec)
```

where `script` is:

```bash
set -e
<pre_cmd>        # optional; skipped if empty
<build_cmd>
exec <run_cmd>
```

Key differences from agent turns:
- **Direct host port**: The app binds the host port directly (host process). There is no `-p` mapping and no Podman machine indirection. The "open in browser" link uses `http://localhost:<port>`.
- **Shell command, not agent argv**: The `Cmd` is `/bin/bash -c <script>` rather than a harness-built agent argv. No `--verbose`, `--output-format`, `--resume`.
- **App secrets, not LLM tokens**: `EnvFile` points at `serve.env`, not the wallfacer `.env`. Per-session `config.Env` overrides are applied via `ContainerSpec.Env`.
- **Pre-command**: Optional `pre_cmd` runs before build (e.g. starting dependent services via compose).
- **Longer lifetime**: Runs until explicitly stopped, not until an agent ends a turn.

> Open design point: the current `HostBackend.Launch` is specialized for the
> Claude/Codex harnesses (it builds the agent argv internally; see
> `launchClaude`). Serve needs a "raw command" launch path. The realistic
> options are (a) add a thin `LaunchCommand(ctx, spec)` seam to the backend
> that runs `spec.Cmd` verbatim under `cmd.Dir = spec.WorkDir`, reusing
> `buildChildEnv`/`hostHandle`, or (b) have the serve runner own a small
> `os/exec` wrapper that mirrors `hostHandle` (state machine + livelog) without
> going through the harness path. Option (a) keeps a single process model and
> is preferred. Decide during Phase 2.

The serve runner tracks the running process in a dedicated registry slot (the
serve session is a singleton in v1), reusing the singleton entry of
`internal/runner/registry.go` (`SetSingletonHandle`/`GetSingletonHandle`) or a
parallel serve-scoped registry. It holds the `executor.Handle` for stop/kill and
the log reader for SSE.

### Discovery Process

A short-lived agent process that reads the codebase and outputs a JSON config. It is launched like `runIdeationEphemeral` (a one-shot agent host process), not as a long-lived serve process:

- `EnvFile`: the wallfacer `.env` (LLM tokens) - discovery is an agent run.
- `WorkDir`: the resolved workspace/worktree root (read-only intent; the agent is prompted not to modify files).
- `Cmd`: the harness agent argv with the discovery prompt.

The discovery prompt is rendered from `internal/prompts/serve-discover.tmpl` and instructs the agent to:
1. List and inspect build files (`Makefile`, `package.json`, `go.mod`, `Cargo.toml`, `pyproject.toml`, `docker-compose.yml`).
2. Determine the most appropriate build and run commands.
3. Identify the primary port (if any).
4. Detect dependent services: look for `docker-compose.yml`/`compose.yaml` and database connection strings, and propose a `pre_cmd` to start them (e.g. `docker compose up -d postgres redis`).
5. Identify required environment variables: scan for `os.Getenv`, `process.env`, `.env.example`, `config.yaml`, etc. and list variable *names* the app expects (without guessing secret values).
6. Identify the working subdirectory if the app does not live at the repo root, and propose `work_dir`.
7. Output exactly one JSON block matching the `ServeConfig` schema: `{"build_cmd": "...", "run_cmd": "...", "pre_cmd": "...", "port": N, "env": {...}, "work_dir": "..."}`.

The runner parses the agent's output, extracts the JSON block (reusing the NDJSON/agent-output parsing already used by ideation, `internal/runner/parse.go` / `ideate_parse.go`), and stores it as the proposed config. The `env` field from discovery contains only variable *names* with empty/placeholder values; the user fills in actual secrets in the serve panel.

### Auto-Rebuild Mode

When `auto_rebuild` is enabled, the serve runner watches the working directory on the host (Go `fsnotify`) and, on a relevant change, restarts the app:

```
1. Run pre_cmd (first start only), then build_cmd, then run_cmd as the app process.
2. Watch <work_dir> recursively, ignoring .git, node_modules, __pycache__, .next, dist, etc.
3. On change: kill the app process, re-run build_cmd, re-run run_cmd.
```

This is implemented entirely host-side in `internal/runner/serve.go`. There is no in-container watcher and no `inotify-tools` image dependency (there is no image). If the build step fails on a rebuild, the runner keeps the watcher alive and surfaces the build error to the log stream; the previous app process stays down until the next successful build (configurable later).

---

## UI

The vanilla `ui/` tree is gone; the frontend is Vue under `frontend/src/`. Live Serve is a new Vue component plus a small entry point in the sidebar.

### Entry Point

Add a "Serve" control to the sidebar (`frontend/src/components/Sidebar.vue`) and/or a task action in `TaskDetail.vue`. The control shows:
- **Idle state**: Play icon + "Serve"
- **Discovering state**: Spinner + "Detecting..."
- **Running state**: Green dot + "Serving on :PORT" (clickable to open `http://localhost:PORT`)
- **Failed state**: Red dot + "Serve failed"

Clicking when idle opens the serve panel. Clicking when running opens the serve panel focused on logs.

### Serve Panel - `frontend/src/components/ServePanel.vue` (new)

A panel/modal (styled with `frontend/src/styles/tokens.css`, consistent with `SettingsModal.vue` and the `components/settings/` editors) with three sections:

1. **Scope selector**: Radio for "Workspace" (default) or "Task". When "Task" is selected, a dropdown lists tasks that have a worktree.

2. **Config editor** (shown after discovery or from cache), organized as sub-tabs:
   - **Commands**: Pre-command, build command, run command (monospace inputs), working directory, auto-rebuild toggle.
   - **Network**: Port (number input) used for the "open in browser" link.
   - **Environment**: Key-value editor for per-session `config.Env` overrides. An "Edit serve.env" action opens a text editor for the shared `<configDir>/serve.env` file (same pattern as the instructions/AGENTS.md editor, `InstructionsEditor.vue`). Discovery-detected variable names are shown as hints with empty values.
   - **Services** (reserved, greyed out): Placeholder showing detected `docker-compose.yml` services. Informational only in v1.

3. **Action buttons**: "Detect Commands" (runs discovery), "Start", "Stop", "Open in Browser" (when a port is set and the session is running).

### Log Panel

When a serve session is running, the panel exposes a log view that uses the same SSE pattern as task log viewing (reuse `TerminalPanel.vue` ANSI rendering where practical):

- Connects to `GET /api/serve/logs`
- Auto-scrolls to bottom
- ANSI color rendering (reuse the existing terminal/ANSI utility)
- "Clear" (client-side only)
- "Stop" (kills the session)

### State Store + SSE Integration

Add a small Pinia store (`frontend/src/stores/serve.ts`, new) for serve session state, mirroring `stores/tasks.ts`. The serve session state is broadcast over the existing task SSE stream (`GET /api/tasks/stream`) as a new event type so the sidebar control updates without polling:

```json
{ "type": "serve", "data": { /* ServeSession */ } }
```

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_SERVE_AUTO_REBUILD` | `false` | Enable auto-rebuild by default for new serve sessions |
| `WALLFACER_SERVE_TIMEOUT` | `0` | Auto-stop timeout in minutes (0 = no timeout) |

> Removed vs. the earlier draft: `WALLFACER_SERVE_HOST_PORT` (no port mapping;
> the app binds the host port directly), and `WALLFACER_SERVE_CPUS` /
> `WALLFACER_SERVE_MEMORY` (no container, so no cgroup limits applied by
> Wallfacer; the process inherits host limits). If per-process limits become
> desirable later, they would be applied via the host backend, out of scope here.

### App-Level Secrets (`<configDir>/serve.env`)

A dedicated env file for application secrets, separate from the LLM token `.env`. Example:

```env
DATABASE_URL=postgres://user:pass@localhost:5432/mydb
REDIS_URL=redis://localhost:6379
AWS_ACCESS_KEY_ID=AKIA...
AWS_SECRET_ACCESS_KEY=...
SESSION_SECRET=random-string-here
```

This file is:
- Supplied as `ContainerSpec.EnvFile` to serve processes only (never to task/agent processes, which use `.env`).
- Editable from the serve panel UI.
- Excluded from the discovery cache (secrets are never written to `serve-configs/*.json`).
- Created empty (mode `0600`) on first serve session if it does not exist.

Per-session `config.Env` overrides take precedence over `serve.env` values for the same key (`HostBackend.buildChildEnv` overlays `Env` on top of the file).

### Cached Discovery Configs

Stored at `<configDir>/serve-configs/<fingerprint>.json`:

```json
{
  "fingerprint": "<16-hex>",
  "detected_at": "2026-03-25T10:00:00Z",
  "config": {
    "build_cmd": "go build -o server .",
    "run_cmd": "./server -addr :8080",
    "port": 8080,
    "env": {}
  }
}
```

The fingerprint is `prompts.InstructionsKey(workspaces)` (sorted workspace paths, SHA256, 16 hex chars), the same algorithm AGENTS.md uses. Cached configs are reused until the user explicitly re-runs discovery. The cache serializer strips non-empty `env` values before writing so secrets never land in the cache.

---

## Implementation Phases

### Phase 1 - Data model + storage

| File | Change |
|------|--------|
| `internal/store/models.go` | Add `ServeSession`, `ServeStatus`, `ServeScope`, `ServeConfig`, `ServeService` types |
| `internal/store/serve.go` (new) | `SaveServeSession`, `GetServeSession`, `DeleteServeSession` (file I/O under `<configDir>/serve/`) |
| `internal/store/serve_test.go` (new) | Round-trip persistence tests |

**Effort:** Low. New types and file I/O following the `oversight.go` pattern.

### Phase 2 - Serve process runner

| File | Change |
|------|--------|
| `internal/runner/serve.go` (new) | `StartServe`, `StopServe`, serve process name helper on `Runner` |
| `internal/runner/serve.go` | Launch spec assembly: resolve workspace/worktree CWD, build the `/bin/bash -c` script, `EnvFile`/`Env` wiring (reuse `buildBaseContainerSpec`) |
| `internal/executor` | Add a raw-command launch seam if needed (see "Open design point" under Serve Process) so a non-agent `Cmd` can run under the host backend |
| `internal/runner/serve.go` | Registry tracking for the serve process (singleton handle + log reader) |
| `internal/runner/serve_test.go` (new) | Unit tests for spec/script assembly, CWD resolution, env wiring |

**Effort:** Medium. Reuses `buildBaseContainerSpec` / host-mode CWD resolution but introduces a raw-command process and a long-lived (non-turn) lifetime.

### Phase 3 - Discovery agent

| File | Change |
|------|--------|
| `internal/prompts/serve-discover.tmpl` (new) | System prompt template for the command-discovery agent (embedded + override, per `internal/prompts/doc.go`) |
| `internal/prompts` | Add a `RenderServeDiscover` method on the prompt Manager |
| `internal/runner/serve_discover.go` (new) | `RunDiscovery`: launch a one-shot agent process (pattern from `runIdeationEphemeral`), parse the JSON block from output |
| `internal/runner/serve_discover.go` | Config caching (fingerprint to JSON file under `<configDir>/serve-configs/`) |
| `internal/runner/serve_discover_test.go` (new) | Test JSON extraction from mock agent output, cache round-trip, secret stripping |

**Effort:** Medium. Agent launch reuses ephemeral-ideation patterns; JSON extraction reuses `ideate_parse.go`.

### Phase 4 - API endpoints + handler

| File | Change |
|------|--------|
| `internal/apicontract/routes.go` | Register the 9 new routes above |
| `internal/handler/serve.go` (new) | `GetServe`, `StartServe`, `StopServe`, `UpdateServe`, `ServeDiscover`, `CancelDiscover`, `ServeLogs`, `GetServeEnv`, `UpdateServeEnv` handlers |
| `internal/cli/server.go` | Wire handlers into the mux (handler map + body-limit map, per existing convention) |
| `internal/handler/serve_test.go` (new) | Handler tests per endpoint |

**Effort:** Medium. Follows existing handler patterns (`oversight.go`, `stream.go`, `env.go`).

### Phase 5 - Auto-rebuild (host file watcher)

| File | Change |
|------|--------|
| `internal/runner/serve.go` | `fsnotify` watcher over `work_dir`, ignore-list, kill+rebuild+restart cycle |
| `internal/runner/serve_test.go` | Tests for watch-trigger debounce, ignore-list, restart sequence |

**Effort:** Low-Medium. Pure host-side Go; no image/tooling dependency (the earlier `inotify-tools` image PR is no longer needed).

### Phase 6 - UI

| File | Change |
|------|--------|
| `frontend/src/components/ServePanel.vue` (new) | Serve panel: scope selector, config editor sub-tabs, log view, action buttons |
| `frontend/src/stores/serve.ts` (new) | Pinia store for serve session state + SSE handling |
| `frontend/src/components/Sidebar.vue` | "Serve" entry point with state-driven label/icon |
| `frontend/src/components/TaskDetail.vue` | Optional per-task "Serve this worktree" action |
| `frontend/src/api` (generated client) | Regenerated via `make api-contract` so the new routes are typed |

**Effort:** Medium. New panel + log view, following `SettingsModal.vue` / `InstructionsEditor.vue` / `TerminalPanel.vue` patterns.

### Phase 7 - SSE integration + polish

| File | Change |
|------|--------|
| `internal/handler/stream.go` | Emit `serve` events on session state changes over the task SSE stream |
| `internal/store/serve.go` | Notify mechanism for serve session changes |
| `frontend/src/stores/serve.ts` | Handle `serve` SSE events, update sidebar reactively |

**Effort:** Low. The SSE delta pattern is well established.

### Phase 8 - Tests, docs, contract regeneration

| File | Change |
|------|--------|
| `internal/apicontract/` | Regenerate via `make api-contract` |
| `docs/guide/configuration.md` | Document `WALLFACER_SERVE_*` env vars |
| `docs/guide/board-and-tasks.md` | Document the serve feature in the task workflow |
| `CLAUDE.md` | Add serve routes to the API Routes section |

**Effort:** Low.

---

## Key Patterns Reused

| Pattern | Source | Reused For |
|---------|--------|------------|
| `buildBaseContainerSpec` / host-mode CWD resolution | `internal/runner/container.go` | Serve launch spec, workspace/worktree CWD |
| `HostBackend` + `ContainerSpec` (`EnvFile`/`Env`/`WorkDir`/`Cmd`) | `internal/executor/host.go`, `internal/executor/spec.go` | Serve process launch, env merge |
| `containerRegistry` singleton handle | `internal/runner/registry.go` | Tracking the serve process for log streaming and stop |
| `livelog` SSE tailing + keepalive | `internal/pkg/livelog`, `internal/handler/stream.go` | `GET /api/serve/logs` |
| SSE delta broadcast | `internal/handler/stream.go` | Serve session updates over the task SSE stream |
| Ephemeral agent run | `internal/runner/ideate.go` (`runIdeationEphemeral`) | Discovery agent launch |
| Agent-output / NDJSON parsing | `internal/runner/parse.go`, `ideate_parse.go` | Extracting the discovery JSON block |
| Workspace fingerprint | `internal/prompts/instructions.go` (`InstructionsKey`) | Discovery config cache keying |
| File-backed session store | `internal/store/oversight.go` | `SaveServeSession` / `GetServeSession` |
| Settings/instructions editor | `frontend/src/components/SettingsModal.vue`, `InstructionsEditor.vue` | Serve config editor + serve.env editor |

---

## Potential Challenges

1. **Port conflicts**: If the app's port is already in use on the host, the process fails to bind and exits. Report the failure clearly in the session error and log stream. (There is no auto-assign indirection under host execution; the app owns the port it binds.)

2. **Long-running process cleanup**: Unlike agent turns that exit after a turn, the serve process runs indefinitely. If the wallfacer server crashes or restarts, the orphaned host process may remain. Mitigate with:
   - On server startup, look up the persisted serve session (`<configDir>/serve/<uuid>.json`) and reconcile: if `status: running`, check the recorded `ProcessName`/PID and reap or re-adopt it. Use the `wallfacer.serve.id` label / process-name convention the host backend already uses for task processes.
   - Optional `WALLFACER_SERVE_TIMEOUT` auto-stop.

3. **Discovery agent reliability**: The agent may produce incorrect or incomplete commands. The user-review step mitigates this; the panel always shows the proposed config for editing before starting. Cached configs skip the agent entirely on repeat runs.

4. **Worktree scope vs. workspace scope**: When targeting a task's worktree, the worktree must exist (the task must have been started at least once). The API validates this and returns a clear error if the worktree has not been created.

5. **Resource contention**: A serve process competes with agent processes for CPU/memory on the host. There is no Wallfacer-applied limit under host execution; document this and rely on the OS scheduler. Per-process limits are a later concern.

6. **Secret leakage via discovery cache**: The discovery agent may detect environment variable names from the codebase. The cached config stores only variable *names* (empty values), never actual secrets. Actual values live exclusively in `<configDir>/serve.env` and per-session `config.Env`. The cache serializer must strip non-empty values before writing.

7. **Working-directory writes**: Because the app runs in the host worktree/workspace, anything it writes (databases, build artifacts, caches) lands in that directory and may dirty the git worktree. Document this; the agent should prefer build output paths that are gitignored, and serve runs against a worktree do not auto-commit.

8. **serve.env file permissions**: `<configDir>/serve.env` contains plaintext secrets. Create it with `0600`. The UI editor should warn that secrets are stored in plaintext on disk.

9. **Dependent service lifecycle**: In v1, `pre_cmd` (e.g. `docker compose up -d`) starts services but Wallfacer does not stop them when the serve session ends. Document that users manage service lifecycle manually or via compose. A future service-orchestration design (reserved `services` field) would track and tear these down.

---

## Migration & Backward Compatibility

- **Additive only**: No changes to existing data models or API contracts.
- **New routes**: All under `/api/serve/`, no collision with existing routes.
- **New storage**: `<configDir>/serve/`, `<configDir>/serve-configs/`, and `<configDir>/serve.env` are created on first use.
- **No image change**: Host execution means there is no sandbox image to modify; the earlier `inotify-tools` image PR is dropped.
- **UI**: The sidebar Serve entry and new `ServePanel.vue` are additive; no existing UI is modified beyond adding the entry point.

---

## Open Questions

1. **Raw-command launch seam.** Does the serve process go through a new `executor.Backend` method (e.g. `LaunchCommand`) that runs `spec.Cmd` verbatim, or through a small serve-owned `os/exec` wrapper that mirrors `hostHandle` (state machine + livelog)? Preference is the backend seam for a single process model, decided in Phase 2.

2. **Multiple concurrent serve sessions?** This spec assumes one active session at a time (singleton). Supporting multiple (e.g. frontend + backend) would require session IDs in the UI and a per-session registry instead of the singleton slot. Deferred.

3. **Health checks?** If a port is configured, the runner could periodically probe `localhost:<port>` and report health. Useful but added complexity. Deferred.

4. **Service orchestration?** Full lifecycle management of dependent services (databases, caches, queues) on the host. The `ServeService` type is reserved. A future design would: start services before the app, stop them on session end, stream their logs, and surface readiness gating (wait for Postgres before starting the app).

5. **Tunnel / external access?** For testing webhooks and external integrations, the serve runner could expose a tunnel (ngrok/cloudflared) in front of the host port. Deferred; v1 exposes the raw `localhost` port.

6. **Secret sourcing.** For production-like testing, `serve.env` plaintext is adequate. A later option is a `secret_cmd` in `ServeConfig` that runs before the app and populates env dynamically (e.g. `op run --env-file=.env.tpl --`), so `serve.env` could hold references rather than plaintext.

7. **Git-dirty worktrees from serve runs.** Should serve runs against a worktree write build artifacts to a separate scratch dir to keep the worktree clean, or rely on `.gitignore`? See Potential Challenge 7.

---

## What This Does NOT Require

- No changes to the task execution pipeline or turn loop.
- No changes to the AI agent prompts or executor configuration beyond the new discovery template and the raw-command launch seam.
- No changes to worktree management; serve sessions use existing worktrees or workspace paths as-is.
- No container runtime, image, bind mounts, or port mapping (host execution).
- No new external dependencies; auto-rebuild uses host-side Go (`fsnotify`), not an in-container watcher.
