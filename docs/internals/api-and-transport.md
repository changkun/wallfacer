# API & Transport

This document covers the HTTP API surface, request processing pipeline, real-time event delivery (SSE), container runtime integration, metrics, and supporting infrastructure for the Wallfacer server.

## HTTP API

All state changes flow through `handler.go`. The handler never blocks — long-running work is always handed off to a goroutine.

All routes are canonically defined in `internal/apicontract/routes.go`.

### Routes

| Method + Path | Handler action |
|---|---|
| **Debug & monitoring** | |
| `GET /api/debug/health` | Operational health check: goroutine count, task counts, uptime |
| `GET /api/debug/spans` | Aggregate span timing statistics across all tasks |
| `GET /api/debug/runtime` | Live server internals: pending goroutines, memory, task states, containers |
| `GET /api/debug/board` | Board manifest as seen by a hypothetical new task (no self-task, no worktree mounts) |
| `GET /api/tasks/{id}/board` | Board manifest as it appeared to a specific task (is_self=true, MountWorktrees applied) |
| **Container monitoring** | |
| `GET /api/containers` | List running sandbox containers |
| **File listing** | |
| `GET /api/files` | File listing for @ mention autocomplete |
| **Server configuration** | |
| `GET /api/config` | Get server configuration (workspaces, autopilot flags, sandbox list, payload limits) |
| `PUT /api/config` | Update server configuration (autopilot, autotest, autosubmit, sandbox assignments) |
| **Workspace management** | |
| `GET /api/workspaces/browse` | List child directories for an absolute host path |
| `PUT /api/workspaces` | Replace the active workspace set and switch the scoped task board |
| `POST /api/workspaces/mkdir` | Create a new directory under a mounted workspace |
| `POST /api/workspaces/rename` | Rename a file or directory inside a mounted workspace |
| **Cloud authentication** (only active when `WALLFACER_CLOUD=true`) | |
| `GET /login` | Begin the hosted sign-in flow |
| `GET /callback` | OAuth callback target for the hosted sign-in flow |
| `GET /logout` | Clear the session cookie and redirect to the sign-in page |
| `GET /logout/notify` | Notify peers that this browser session has logged out |
| `GET /api/auth/me` | Return the current principal (user + active org) |
| `GET /api/auth/orgs` | List organizations the current user can switch to |
| `POST /api/auth/switch-org` | Switch the active organization for subsequent requests |
| **Ideation / brainstorm** | |
| `GET /api/ideate` | Get brainstorm/ideation agent status |
| `POST /api/ideate` | Trigger the ideation agent to generate new task ideas |
| `DELETE /api/ideate` | Cancel an in-progress ideation run |
| **Environment configuration** | |
| `GET /api/env` | Get environment configuration (tokens masked) |
| `PUT /api/env` | Update environment file; omitted/empty token fields are preserved |
| `POST /api/env/test` | Test sandbox configuration by running a lightweight probe task |
| **Workspace instructions** | |
| `GET /api/instructions` | Get the workspace AGENTS.md content |
| `PUT /api/instructions` | Save the workspace AGENTS.md |
| `POST /api/instructions/reinit` | Rebuild workspace AGENTS.md from default template and repo files |
| **System prompt templates** | |
| `GET /api/system-prompts` | List all built-in system prompt templates with override status and content |
| `GET /api/system-prompts/{name}` | Get a single built-in system prompt template by name |
| `PUT /api/system-prompts/{name}` | Write a user override for a built-in system prompt template; validates before writing |
| `DELETE /api/system-prompts/{name}` | Remove user override, restoring the embedded default |
| **Prompt templates** | |
| `GET /api/templates` | List all prompt templates sorted by created_at descending |
| `POST /api/templates` | Create a new named prompt template |
| `DELETE /api/templates/{id}` | Delete a prompt template by ID |
| **Git workspace operations** | |
| `GET /api/git/status` | Git status for all mounted workspaces |
| `GET /api/git/stream` | SSE stream of git status updates for all workspaces |
| `POST /api/git/push` | Push a workspace to its remote |
| `POST /api/git/sync` | Fetch and rebase a workspace onto its upstream branch |
| `POST /api/git/rebase-on-main` | Fetch origin/<main> and rebase the current branch on top |
| `GET /api/git/branches` | List branches for a workspace |
| `POST /api/git/checkout` | Switch a workspace to a different branch |
| `POST /api/git/create-branch` | Create and check out a new branch in a workspace |
| `POST /api/git/open-folder` | Open a workspace directory in the OS file manager |
| **Usage & statistics** | |
| `GET /api/usage` | Aggregated token and cost usage statistics |
| `GET /api/stats` | Task status and workspace cost statistics, plus a `planning` section keyed by workspace group. Optional `?workspace=<path>` restricts task aggregation; optional `?days=N` restricts planning aggregation to rounds newer than N days (execution buckets are unchanged by `?days`). |
| **Task collection (no {id})** | |
| `GET /api/tasks` | List all tasks (optionally including archived) |
| `GET /api/tasks/stream` | SSE: full snapshot then incremental task-updated/task-deleted events |
| `POST /api/tasks` | Create a new task in the backlog. **Does not accept `sandbox` or `sandbox_by_activity`** — harness (Claude vs Codex) is selected by the agent a flow step references; the per-task override is applied via `PATCH /api/tasks/{id}` after creation. |
| `POST /api/tasks/batch` | Create multiple tasks atomically with symbolic dependency wiring. Same sandbox-rejection policy as the singular endpoint. |
| `POST /api/tasks/generate-titles` | Bulk-generate titles for tasks that lack one |
| `POST /api/tasks/generate-oversight` | Bulk-generate oversight summaries for eligible tasks |
| `GET /api/tasks/search` | Search tasks by keyword |
| `POST /api/tasks/archive-done` | Archive all tasks in the done state |
| `GET /api/tasks/summaries` | List immutable task summaries for completed tasks (cost dashboard) |
| `GET /api/tasks/deleted` | List soft-deleted (tombstoned) tasks within retention window |
| **Task instance operations ({id})** | |
| `PATCH /api/tasks/{id}` | Update task fields: status, prompt, timeout, sandbox, dependencies, fresh_start |
| `DELETE /api/tasks/{id}` | Soft-delete a task (tombstone); data retained within retention window |
| `GET /api/tasks/{id}/events` | Task event timeline; supports cursor pagination (`after`, `limit`) and type filtering (`types`) |
| `POST /api/tasks/{id}/feedback` | Submit a feedback message to a waiting task |
| `POST /api/tasks/{id}/done` | Mark a waiting task as done and trigger commit-and-push |
| `POST /api/tasks/{id}/cancel` | Cancel a task: kill container and discard worktrees |
| `POST /api/tasks/{id}/resume` | Resume a failed or waiting task using its existing session |
| `POST /api/tasks/{id}/restore` | Restore a soft-deleted task by removing its tombstone |
| `POST /api/tasks/{id}/archive` | Move a done/cancelled task to the archived state |
| `POST /api/tasks/{id}/unarchive` | Restore an archived task |
| `POST /api/tasks/{id}/sync` | Rebase task worktrees onto the latest default branch |
| `POST /api/tasks/{id}/test` | Trigger the test agent for a task |

| `GET /api/tasks/{id}/diff` | Git diff of task worktrees versus the default branch |
| `GET /api/tasks/{id}/logs` | SSE stream of live container logs for a running task |
| `GET /api/tasks/{id}/outputs/{filename}` | Raw Claude Code output file for a single agent turn |
| `GET /api/tasks/{id}/turn-usage` | Per-turn token usage breakdown for a task |
| `GET /api/tasks/{id}/spans` | Span timing statistics for a task |
| `GET /api/tasks/{id}/oversight` | Oversight summary for a completed task |
| `GET /api/tasks/{id}/oversight/test` | Test oversight summary for a task |
| **File Explorer** | |
| `GET /api/explorer/tree` | List one level of a workspace directory |
| `GET /api/explorer/stream` | SSE stream of file tree change notifications |
| `GET /api/explorer/file` | Read file contents from a workspace |
| `PUT /api/explorer/file` | Write file contents to a workspace |
| **OAuth authentication** | |
| `POST /api/auth/{provider}/start` | Start OAuth flow; returns `{authorize_url}` |
| `GET /api/auth/{provider}/status` | Poll flow status; returns `{state, error?}` |
| `POST /api/auth/{provider}/cancel` | Cancel an in-progress flow |
| **Admin** | |
| `POST /api/admin/rebuild-index` | Rebuild the in-memory search index from disk |
| **Spec tree** | |
| `GET /api/specs/tree` | Full spec tree with metadata, progress, and dependency edges |
| `GET /api/specs/stream` | SSE: spec tree change notifications |
| **Planning sandbox & chat** | |
| `GET /api/planning` | Planning sandbox status (running or not) |
| `POST /api/planning` | Start the planning sandbox container (idempotent) |
| `DELETE /api/planning` | Stop the planning sandbox container |
| `GET /api/planning/messages` | Retrieve conversation history. `?thread=<id>` selects the thread; defaults to the active thread. |
| `POST /api/planning/messages` | Send user message (triggers agent execution). Body `thread` field (or `?thread=`) selects the thread. |
| `DELETE /api/planning/messages` | Clear a thread's conversation history and session (`?thread=<id>`). |
| `GET /api/planning/messages/stream` | Stream agent response tokens for the in-flight thread. Returns 204 when `?thread=<id>` does not match the thread that owns the exec. |
| `POST /api/planning/messages/interrupt` | Interrupt current agent turn. `?thread=<id>` must match the in-flight thread or 409. |
| `POST /api/planning/undo` | Undo the caller thread's most recent planning round via a forward `git revert` commit (original commit stays in history; revert commit carries `Plan-Thread: <id>` and an incremented `Plan-Round`). `?thread=<id>` selects the caller's thread. Cancels dispatched board tasks whose linkage was added by the reverted commit. 409 on revert conflict. |
| `GET /api/planning/commands` | List available slash commands |
| **Planning chat threads** | |
| `GET /api/planning/threads` | List non-archived threads; `?includeArchived=true` includes archived ones. Returns `{threads, active_id}`. |
| `POST /api/planning/threads` | Create a new thread. Body `{name?}`; omitted name auto-generates `Chat N`. |
| `PATCH /api/planning/threads/{id}` | Rename a thread. Body `{name}`. |
| `POST /api/planning/threads/{id}/archive` | Archive a thread (hide from tab bar; files retained). 409 if the thread is in-flight. |
| `POST /api/planning/threads/{id}/unarchive` | Restore an archived thread. |
| `POST /api/planning/threads/{id}/activate` | Record the UI's active thread. Rejects archived/unknown IDs. |
| **Sandbox images** | |
| `GET /api/images` | Check which sandbox images are cached locally |
| `POST /api/images/pull` | Start async pull for a sandbox image |
| `DELETE /api/images` | Remove a cached sandbox image |
| `GET /api/images/pull/stream` | SSE: pull progress |

### Triggering Task Execution

When a `PATCH /api/tasks/{id}` request moves a task to `in_progress`, the handler:

1. Updates the task record (status, session ID)
2. Launches a background goroutine: `go h.runner.Run(id, prompt, sessionID, false)`
3. Returns `200 OK` immediately — the client does not wait for execution

The same pattern applies to feedback resumption and commit-and-push.

## Request Middleware Chain

The HTTP server wraps the `ServeMux` in a layered middleware chain. Each request passes through these layers in order:

```mermaid
flowchart LR
    Request --> Logging["loggingMiddleware<br/>(server.go)"]
    Logging --> CSRF["CSRFMiddleware<br/>(handler/middleware.go)"]
    CSRF --> Cookie["CookiePrincipal<br/>(internal/auth)"]
    Cookie --> Optional["OptionalAuth<br/>(internal/auth)"]
    Optional --> Bearer["BearerAuthMiddleware<br/>(handler/middleware.go)"]
    Bearer --> Force["ForceLogin<br/>(handler/force_login.go)"]
    Force --> Mux["ServeMux route matching"]
    Mux --> BodyLimit["MaxBytesMiddleware<br/>(per-route, handler/middleware.go)"]
    BodyLimit --> StoreGuard["RequireStoreMiddleware<br/>(per-route, handler/handler.go)"]
    StoreGuard --> Handler["Handler method"]
```

The chain is assembled in `internal/cli/server.go` (outermost first: logging → CSRF → CookiePrincipal → OptionalAuth → BearerAuth → ForceLogin → mux):
```go
srvHandler := h.ForceLogin(mux)
srvHandler = handler.BearerAuthMiddleware(envCfg.ServerAPIKey)(srvHandler)
srvHandler = auth.OptionalAuth(jwtValidator, srvHandler)
srvHandler = auth.CookiePrincipal(authClient, jwtValidator, srvHandler)
if !cfg.SkipCSRF {
    srvHandler = handler.CSRFMiddleware(actualHostPort)(srvHandler)
}
srv := &http.Server{Handler: loggingMiddleware(srvHandler, reg), ...}
```

### What each middleware does

| Layer | Location | Behaviour |
|---|---|---|
| **Logging** | `cli/server.go` `loggingMiddleware()` | Wraps the response writer to capture status codes. Logs every API request with method, path, status, and duration. Records `wallfacer_http_requests_total` counter and `wallfacer_http_request_duration_seconds` histogram. Uses `r.Pattern` for route labels. |
| **CSRF** | `handler/middleware.go` `CSRFMiddleware()` | For mutating methods (POST, PUT, PATCH, DELETE), validates that the `Origin` or `Referer` header matches the server's host:port. GET/HEAD/OPTIONS pass through. Requests with no Origin/Referer also pass (for CLI/API clients). |
| **CookiePrincipal** | `internal/auth` `CookiePrincipal()` | Cloud-mode only: resolves the session cookie into a principal (user + org claims) and injects it into the request context. No-op when the request has no cookie or cloud mode is disabled. |
| **OptionalAuth** | `internal/auth` `OptionalAuth()` | Cloud-mode only: if a `Bearer` JWT is present, validates it against the configured JWKS and puts the resulting `*Claims` into the request context. JWT wins over the cookie when both are present; missing tokens pass through. |
| **BearerAuth** | `handler/middleware.go` `BearerAuthMiddleware()` | When `WALLFACER_SERVER_API_KEY` is configured, requires `Authorization: Bearer <key>` on all requests except: the root page (`GET /`), OAuth routes (`/login`, `/callback`, `/logout`), and streaming/WebSocket paths (`/api/tasks/stream`, `/api/git/stream`, `/api/explorer/stream`, `/api/specs/stream`, `*/logs`, `/api/terminal/ws`) which accept `?token=<key>` as a query parameter instead. Bypasses its static-key check when cloud claims are already populated so cookie-only browser requests succeed alongside script clients. No-op when no API key is configured. |
| **ForceLogin** | `handler/force_login.go` `ForceLogin()` | Cloud-mode only: redirects unauthenticated browser requests for the app shell to `/login`. API routes return 401 instead. No-op in local mode. |
| **Body limits** | `handler/middleware.go` `MaxBytesMiddleware()` | Applied per-route via `bodyLimits` map in `BuildMux`. Default: 1 MiB. Instructions: 5 MiB. Feedback: 512 KiB. Wraps `r.Body` with `http.MaxBytesReader` to reject oversized payloads. |
| **Store guard** | `handler/handler.go` `RequireStoreMiddleware()` | Applied per-route via `requiresStore()` check. Returns 503 when no workspace/store is configured. Exempted routes: `GetConfig`, `UpdateConfig`, `BrowseWorkspaces`, `UpdateWorkspaces`, `GetEnvConfig`, `UpdateEnvConfig`, `TestSandbox`, `GitStatus`, `GitStatusStream`. |

## SSE Live Updates

Both task state and git status use the same SSE push pattern:

```mermaid
sequenceDiagram
    participant UI
    participant Handler
    participant Store

    UI->>Handler: GET /api/tasks/stream (SSE)
    Handler->>Handler: Register subscriber channel

    Store->>Store: Write() mutates state
    Store->>Handler: notify() (non-blocking, coalesced)
    Handler->>Handler: Serialise full task list as JSON
    Handler-->>UI: SSE: data: {json}

    Note over Handler: Buffered channel (size 256)
    Note over Handler: Incremental deltas sent,
    Note over Handler: with replay buffer for reconnection
```

`notify()` uses buffered channels of size 256 (`pubsub.DefaultChannelSize`). Each state change produces a `SequencedDelta` that is fanned out to all subscribers. A replay buffer (up to 512 entries, `pubsub.DefaultReplayCapacity`) enables reconnecting clients to catch up on missed deltas.

The same pattern applies to `GET /api/git/stream`, except the source is a time-based ticker (polling `git status` every few seconds) rather than a store write signal.

Live container logs use a different mechanism: `GET /api/tasks/{id}/logs` opens a process pipe to `<runtime> logs -f <name>` and streams its stdout line-by-line as SSE events.

### Task Stream (`GET /api/tasks/stream`)

Implemented in `Handler.StreamTasks()` (`internal/handler/stream.go`).

#### Subscriber Registration

On each SSE connection, the handler calls `store.Subscribe()`, which allocates a buffered channel sized at `pubsub.DefaultChannelSize` (256) and registers it in the store's `subscribers` map under a monotonically increasing integer ID.

The subscription is created **before** reading any state, ensuring no events are missed between the initial snapshot and the live loop.

#### Event Types

Three SSE event types are emitted:

| SSE `event:` | When | `data:` payload |
|---|---|---|
| `snapshot` | Initial connection or gap-too-old reconnect | Full `[]Task` JSON array |
| `task-updated` | Task created or mutated | Single `Task` JSON object |
| `task-deleted` | Task soft-deleted | `{"id": "<uuid>"}` |

Every SSE frame includes an `id:` field set to the delta sequence number, enabling the browser's built-in `Last-Event-ID` reconnection mechanism.

#### Reconnection and Replay

On reconnect, the client provides its last seen sequence via the `?last_event_id` query parameter or the `Last-Event-ID` HTTP header. The store's `DeltasSince(seq)` method binary-searches the replay buffer (up to 512 entries, `replayBufMax`) for deltas newer than the given sequence:

- **Buffer covers the gap**: Missed deltas are replayed individually as `task-updated` / `task-deleted` events. No full snapshot is needed.
- **Gap too old** (oldest buffered delta's Seq > requested seq + 1): Falls back to a full `snapshot` event via `ListTasksAndSeq()`, which reads both the task list and current sequence under the same read lock to guarantee consistency.

#### Backpressure and Dropped Events

`notify()` uses a non-blocking send to each subscriber channel:

```go
select {
case ch <- cloneSequencedDelta(sd):
default:  // channel full — drop this delta for this subscriber
}
```

If a subscriber's buffer (256 slots) is full, the delta is silently dropped for that subscriber. The subscriber will eventually receive a later delta; if it reconnects, the replay buffer provides catch-up. All deltas sent to subscribers are deep clones of the task state, preventing data races.

#### Connection Cleanup

When the client disconnects, `r.Context().Done()` fires in the SSE loop. The deferred `store.Unsubscribe(subID)` removes the channel from the subscribers map and drains any buffered deltas to free memory. The channel is **not** closed — `StreamTasks` is always the caller of `Unsubscribe`, so there is no blocked receiver to wake.

### Wake-Only Subscribers

In addition to the full-delta channel, the store provides a lightweight `SubscribeWake()` mechanism: a `chan struct{}` with capacity 1. Rapid bursts of notifications coalesce — once the channel is full, subsequent sends are no-ops. This is used by watchers (auto-promoter, auto-retrier, etc.) that only need a "something changed" signal, not the full delta payload.

### Git Status Stream (`GET /api/git/stream`)

Implemented in `Handler.GitStatusStream()` (`internal/handler/git.go`). Unlike the task stream, git status uses a **polling ticker** (every 5 seconds) rather than store-driven pub/sub. On each tick, the handler collects `git status` for all workspaces, JSON-marshals the result, compares it byte-for-byte with the previous emission, and only sends an SSE frame if the data has changed.

### Explorer Stream (`GET /api/explorer/stream`)

Implemented in `Handler.ExplorerStream()` (`internal/handler/explorer.go`). Uses a **polling ticker** (every 3 seconds) to fingerprint workspace root directories (hashing entry names, types, sizes, and modification times). Only sends a `refresh` event when a directory's fingerprint changes, so the frontend can re-fetch affected nodes. This replaces the previous approach where the frontend polled `GET /api/explorer/tree` every 3 seconds for each expanded directory.

**Events:** `connected` (on first connect), `refresh` (with `{workspaces: [...]}` payload listing changed workspace paths), `heartbeat` (every 15 seconds).

### Spec Tree Stream (`GET /api/specs/stream`)

Implemented in `Handler.SpecTreeStream()` (`internal/handler/specs.go`). Uses a **polling ticker** (every 3 seconds) to rebuild the spec tree and compare it byte-for-byte with the previous emission. Sends a `snapshot` event with the full tree data only when the content has changed. This replaces the previous frontend polling of `GET /api/specs/tree` every 3 seconds.

**Events:** `snapshot` (initial and on change, with full tree JSON), `heartbeat` (every 15 seconds).

### Live Container Logs (`GET /api/tasks/{id}/logs`)

Not SSE in the strict sense — this endpoint streams raw `text/plain` output. It spawns `<runtime> logs -f --tail 100 <containerName>` as a subprocess, pipes stdout and stderr through a scanner, and writes lines to the HTTP response. A 15-second keepalive ticker sends empty newlines to keep the connection alive and detect client disconnects.

## WebSocket Terminal

`GET /api/terminal/ws` is the project's only WebSocket endpoint. It provides an interactive host shell via a PTY relay. Unlike the REST routes defined in `internal/apicontract/routes.go`, this endpoint is registered directly in `BuildMux` (`internal/cli/server.go`) because WebSocket upgrades don't follow REST request/response semantics.

The handler (`internal/handler/terminal.go`) manages multiple concurrent shell sessions per WebSocket connection via a `sessionRegistry`. On connect, one session is auto-created. The relay dispatcher routes PTY output from the active session to the client and directs client input to the active session's PTY. Session switching re-resolves the active session without reconnecting.

The feature is gated on `WALLFACER_TERMINAL_ENABLED` (default `true`; set to `false` to disable). Authentication uses `?token=` query parameter (same mechanism as SSE paths), since the browser `WebSocket` constructor cannot set custom headers.

### Message Protocol

**Client → Server (JSON text frames):**

| Type | Fields | Description |
|------|--------|-------------|
| `input` | `data` (base64) | Terminal input bytes |
| `resize` | `cols`, `rows` | Resize the active session's PTY |
| `ping` | — | Keep-alive; server responds with `pong` |
| `create_session` | `container` (name, optional) | Spawn a new shell session. If `container` is set, spawns `<runtime> exec -it <container> bash` instead of a host shell. |
| `switch_session` | `session` (ID) | Switch the active session |
| `close_session` | `session` (ID) | Close and remove a session |

**Server → Client (JSON text frames):**

| Type | Fields | Description |
|------|--------|-------------|
| `pong` | — | Keep-alive response |
| `sessions` | `sessions` (array of `{id, active, container?}`) | Full session list; sent on connect and after any session change. `container` is set for exec sessions. |
| `session_created` | `session` (ID) | New session spawned |
| `session_switched` | `session` (ID) | Active session changed |
| `session_closed` | `session` (ID) | Session removed |
| `session_exited` | `session` (ID) | Session's shell process exited |
| `error` | `data` (string) | Error message (e.g., invalid session ID) |

**Server → Client (binary frames):** Raw PTY output from the active session.

### Architecture

- **`sessionRegistry`** (`terminal.go`): manages `map[string]*terminalSession`, tracks the active session, and provides `create`, `switchTo`, `remove`, `closeAll`, and `activeSession` methods. A `switchCh` channel signals the relay dispatcher when the active session changes.
- **`relayDispatcher`**: the PTY→WS goroutine re-resolves the active session on each switch signal. The WS→PTY goroutine resolves `activeSession()` per message.
- **`monitorSession`**: per-session goroutine that waits for shell exit, then calls `handleSessionExit` which removes the session, sends `session_exited`, and auto-switches to a fallback or closes the WebSocket.
- **Frontend** (`ui/js/terminal.js`): tab bar UI with per-session output buffering (~100KB cap). On `session_switched`, xterm is cleared and the target session's buffer is replayed.

## Container Runtime

### Auto-Detection Order

`detectContainerRuntime()` in `main.go` probes for a container runtime in this order:

1. `CONTAINER_CMD` environment variable — if set, used verbatim (highest priority).
2. `/opt/podman/bin/podman` — checks for the file with `os.Stat`.
3. `podman` on `$PATH` — found via `exec.LookPath`.
4. `docker` on `$PATH` — found via `exec.LookPath`.
5. Falls back to `/opt/podman/bin/podman` as a hardcoded default (so the error message is clear when nothing is found).

The `-container` CLI flag can also override the detected runtime.

### Podman vs Docker Format Differences

The server handles format differences transparently in `parseContainerList()` (`internal/runner/runner.go`):

| Aspect | Podman | Docker |
|---|---|---|
| `ps --format json` output | JSON array (`[{...}, ...]`) | NDJSON (one `{...}` per line) |
| `Names` field | `[]string` | `string` |
| `Created` field | `int64` (unix timestamp) | `string` (formatted datetime) |

The `containerJSON` struct uses `json.RawMessage` for `Names` and `any` for `Created`, then tries both formats in sequence.

### Image Pull Logic

`ensureImage()` in `server.go` runs at startup:

1. **Check local**: `<runtime> images -q <image>` — if output is non-empty, the image exists locally and is used as-is.
2. **Pull from registry**: `<runtime> pull <image>` — streams stdout/stderr to the terminal. The default image is `ghcr.io/latere-ai/sandbox-agents:latest`.
3. **Fallback to local**: If the pull fails and the requested image differs from `sandbox-agents:latest`, check whether `sandbox-agents:latest` exists locally. If so, use it instead.
4. **No image**: If neither the remote nor local fallback is available, the server starts anyway but warns that tasks may fail.

### Container Labels

Every task container is labeled with metadata for monitoring and correlation:

```
--label wallfacer.task.id=<uuid>
--label wallfacer.task.prompt=<first 80 chars>
```

These labels are set in `buildContainerArgsForSandbox()` (`internal/runner/container.go`). The `ListContainers()` method reads these labels to correlate containers to tasks without relying on container name parsing — this is the primary lookup path, with name-based UUID extraction as a legacy fallback.

### Resource Limits

Container resource limits follow a three-tier resolution order (in `resolvedContainerCPUs()`, `resolvedContainerMemory()`, `resolvedContainerNetwork()`):

1. Explicit `RunnerConfig` value passed at construction time.
2. Value from `~/.wallfacer/.env` (re-read on each container launch).
3. Default: no CPU/memory limit; `host` network.

```
[--cpus 2.0]       # from WALLFACER_CONTAINER_CPUS
[--memory 4g]      # from WALLFACER_CONTAINER_MEMORY
[--network mynet]   # from WALLFACER_CONTAINER_NETWORK, default "host"
```

## Metrics Reference

All metrics are served at `GET /metrics` in Prometheus text exposition format via `metrics.Registry.WritePrometheus()`.

### Counters

| Metric | Labels | Description |
|---|---|---|
| `wallfacer_http_requests_total` | `method`, `route`, `status` | Total HTTP requests. Route uses `r.Pattern` (Go 1.22+) to collapse parameterised paths. |
| `wallfacer_autopilot_actions_total` | `watcher`, `outcome` | Autonomous actions taken by autopilot watchers (e.g. promote, retry, test, submit, refine). |

### Histograms

| Metric | Labels | Buckets | Description |
|---|---|---|---|
| `wallfacer_http_request_duration_seconds` | `method`, `route` | 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s | HTTP request latency distribution. |

### Gauges (scrape-time)

These are computed on each `/metrics` scrape via registered collector functions:

| Metric | Labels | Description |
|---|---|---|
| `wallfacer_tasks_total` | `status`, `archived` | Number of tasks grouped by status and archived flag. |
| `wallfacer_running_containers` | — | Number of sandbox containers currently tracked by the container runtime. |
| `wallfacer_background_goroutines` | — | Number of outstanding background goroutines tracked by the runner's `trackedWg`. |
| `wallfacer_store_subscribers` | — | Number of active SSE subscribers listening for task state changes. |
| `wallfacer_failed_tasks_by_category` | `category` | Number of currently-failed (non-archived) tasks grouped by failure category. |
| `wallfacer_circuit_breaker_open` | — | 1 when the container launch circuit breaker is open (runtime unavailable), 0 when closed. |

## Token Tracking & Cost

Per-turn usage is extracted from the agent JSON output and accumulated on the `Task`:

```
TaskUsage {
  InputTokens              int
  OutputTokens             int
  CacheReadInputTokens     int
  CacheCreationTokens      int
  CostUSD                  float64
}
```

Usage is displayed on task cards and aggregated in the Done column header. It persists in `task.json` across server restarts.

In addition to the aggregate `TaskUsage`, each task records:

- `UsageBreakdown map[string]TaskUsage` keyed by activity: `implementation`, `testing`, `refinement`, `title`, `oversight`, `commit_message`, `idea_agent`. This lets the Usage tab in the task detail panel show cost per sub-agent rather than a single lump sum.
- Per-turn `TurnUsageRecord` entries accessible via `GET /api/tasks/{id}/turn-usage`, providing detailed per-turn token consumption, stop reasons, and sub-agent labels.

## Task Search

`GET /api/tasks/search?q=<keyword>` searches across task titles, prompts, tags, and oversight text. Results are returned as `TaskSearchResult` objects with the matched field and a context snippet.

The search index is maintained in-memory and updated on task changes. Use `POST /api/admin/rebuild-index` to manually rebuild if needed.

## Span Instrumentation

Key execution phases are instrumented with `span_start` / `span_end` trace events. Each span carries a `SpanData` payload with a `Phase` (e.g. `worktree_setup`, `agent_turn`, `container_run`, `commit`) and an optional `Label` to differentiate multiple spans of the same phase.

- `GET /api/tasks/{id}/spans` — returns all span events for a task, useful for profiling turn latency
- `GET /api/debug/spans` — aggregate span timing statistics across all tasks

## Event Pagination

`GET /api/tasks/{id}/events` supports two modes:

**No query params (backward-compatible)** — returns the full event list as a plain JSON array:

```json
[{"id": 1, "event_type": "state_change", ...}, ...]
```

**With any of `after`, `limit`, or `types` present** — returns a paginated envelope:

```json
{
  "events": [...],
  "next_after": 42,
  "has_more": true,
  "total_filtered": 150
}
```

### Query Params

| Param | Type | Default | Description |
|---|---|---|---|
| `after` | int64 | `0` | Exclusive event ID cursor. Only events with `id > after` are returned. Use `next_after` from the previous response to advance the cursor. |
| `limit` | int | `200` | Maximum events per page. Must be >= 1; values > 1000 are silently capped to 1000. |
| `types` | string | (all) | Comma-separated list of event types to include. Unknown types return 400. Valid values: `state_change`, `output`, `error`, `system`, `feedback`, `span_start`, `span_end`. |

### Response Fields

| Field | Description |
|---|---|
| `events` | The current page of events, ordered by ascending ID. |
| `next_after` | The ID of the last event in this page; pass as `after` to get the next page. `0` when the page is empty. |
| `has_more` | `true` if there are additional events beyond this page. |
| `total_filtered` | Total number of events matching the query (respecting `after` and `types` but ignoring `limit`). Useful for progress display. |

### Pagination Walk Example

```
GET /api/tasks/{id}/events?limit=100&types=output
-> { events: [...100 items], next_after: 347, has_more: true, total_filtered: 250 }

GET /api/tasks/{id}/events?after=347&limit=100&types=output
-> { events: [...100 items], next_after: 503, has_more: true, total_filtered: 250 }

GET /api/tasks/{id}/events?after=503&limit=100&types=output
-> { events: [...50 items], next_after: 553, has_more: false, total_filtered: 250 }
```

### Validation

The handler returns 400 for:
- `after` that is not a non-negative integer
- `limit` that is not a positive integer (including 0)
- Any unrecognised value in `types`

## Store Concurrency

`store.go` manages an in-memory `map[uuid.UUID]*Task` behind a `sync.RWMutex`:

- Reads (`List`, `Get`) acquire a read lock
- Writes (`Create`, `Update`, `UpdateStatus`) acquire a write lock, mutate memory, then atomically persist to disk (temp file + `os.Rename`)
- After every write, `notify()` is called to wake SSE subscribers

Event traces are append-only. Each event is written as a separate file (`traces/NNNN.json`) using the same atomic write pattern. Files are never modified after creation.

## Graceful Shutdown

The server handles `SIGTERM` and `SIGINT` via `signal.NotifyContext`, which creates a cancellable context shared by all background goroutines and the HTTP server's `BaseContext`.

```mermaid
sequenceDiagram
    participant OS as OS Signal
    participant Ctx as signal.NotifyContext
    participant Srv as http.Server
    participant Watchers as Automation Watchers
    participant Runner as Runner

    OS->>Ctx: SIGTERM / SIGINT
    Ctx->>Ctx: cancel context
    Note over Ctx: All SSE handlers exit<br/>(request contexts derived from base)
    Note over Ctx: All automation watchers exit<br/>(select on ctx.Done())

    Ctx->>Srv: srv.Shutdown(5s timeout)
    Note over Srv: Stop accepting new connections<br/>Wait up to 5s for in-flight requests

    Srv->>Runner: r.Shutdown()
    Note over Runner: 1. Cancel shutdownCtx<br/>2. Close shutdownCh<br/>3. Wait for board-cache goroutine<br/>4. Wait for backgroundWg<br/>   (logs pending labels every 3s)

    Note over Srv: "shutdown complete" logged
```

### Shutdown sequence in detail

1. **Signal received** — `signal.NotifyContext` cancels the base context. All SSE handlers and automation watchers detect `ctx.Done()` and exit their loops.

2. **HTTP server shutdown** — `srv.Shutdown()` is called with a 5-second timeout. This stops accepting new connections and waits for in-flight requests to complete. SSE handlers exit promptly because their request contexts (derived from `BaseContext`) are already cancelled.

3. **Runner shutdown** — `r.Shutdown()` performs:
   - Cancels `shutdownCtx` via `shutdownCancel()`.
   - Closes `shutdownCh` to signal the board-cache-invalidator goroutine to exit.
   - Waits for the board subscription goroutine via `boardSubscriptionWg.Wait()`.
   - Waits for all tracked background goroutines via `backgroundWg.Wait()`, logging pending labels every 3 seconds so operators can see what is still running.

4. **In-progress tasks survive** — Running task containers are intentionally left alive. They continue to completion independently and will be recovered on the next server start via `RecoverOrphanedTasks`.

## See Also

- [Architecture](architecture.md) — System overview, design decisions, component responsibilities, concurrency model
- [Automation](automation.md) — Autopilot watchers, auto-retry, circuit breakers, oversight, ideation
- [Task Lifecycle](task-lifecycle.md) — State machine, turn loop, data models
- [Git Worktrees](git-worktrees.md) — Per-task worktree isolation and commit pipeline
