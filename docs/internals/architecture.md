# Architecture

Wallfacer is a host-native Go service that coordinates autonomous coding agents, with per-task git worktree isolation and a web task board for human oversight.

## Host-Process Execution (the core decision)

Each agent turn runs as a host `os/exec` of the selected CLI (`claude`, `codex`, `cursor`, `opencode`, or `pi`) with the task's git worktree as the working directory. Isolation comes from the worktree, not a container: there is no daemon, no image pull, and no `/workspace` bind-mount in the shipping runtime.

The runner unconditionally selects `executor.HostBackend` (the only `executor.Backend` implementation) and sets `hostMode = true` (`internal/runner/runner.go:491-498`). The backend execs the CLI directly (`internal/executor/host.go`). Cancellation is `SIGTERM` then `SIGKILL` on the host process (`internal/executor/host.go`), not a runtime kill command.

Several Go symbols keep the word "Container" as deliberate legacy vocabulary: `ContainerSpec`, `ContainerInfo`, and `buildContainerSpecForSandbox`. These name code, not behavior. The behavior is a host process.

## System Overview

```mermaid
graph TB
    subgraph Browser
        UI["Browser UI<br/>(Vue 3 + TypeScript SPA)<br/>Board + Plan (spec explorer, minimap, agent chat)"]
    end

    subgraph Server["Go Server (stdlib net/http)"]
        Handler["Handler<br/>REST API + SSE"]
        Runner["Runner<br/>orchestration + commit"]
        Store["Store<br/>state + persistence"]
        Plan["Plan Mode<br/>spec tree + agent session<br/>dispatch + undo"]

        Handler --> Runner
        Handler --> Plan
        Runner --> Store
        Plan --> Store
    end

    subgraph Infra["Host"]
        Agents["Agent CLIs (host os/exec)<br/>claude / codex / cursor / opencode / pi<br/>CWD = task worktree"]
        Worktrees["Per-task Git Worktrees<br/>~/.wallfacer/worktrees/<br/>task/ID branches"]
        SpecsFS["specs/ (markdown + frontmatter)<br/>agent sessions<br/>~/.wallfacer/agent-sessions/&lt;fp&gt;/"]
        Agents --- Worktrees
    end

    UI -->|"HTTP / SSE"| Handler
    Runner -->|os/exec| Agents
    Plan -->|os/exec| Agents
    Plan -->|read/write| SpecsFS
```

## Design Decisions

**Filesystem-first persistence.** No database by default. Each task is a directory (`data/<key>/<uuid>/`) containing `task.json`, traces, outputs, and oversight blobs. Writes are atomic (temp file + rename). Easy to inspect, back up, and debug. Persistence goes through the `StorageBackend` seam (see below), so the filesystem layout is one implementation, not a hard dependency.

**Worktree isolation, not container isolation.** Every agent turn is a host process whose CWD is the task's git worktree. Tasks isolate from each other through separate worktrees and `task/<id>` branches, not through a sandboxed runtime. Tasks work in parallel without merge conflicts during execution; rebase and merge happen at commit time.

**Activity-routed harness + model.** Different activities (implementation, oversight, commit-msg) can route to different harnesses (`claude`, `codex`, `cursor`, `opencode`, `pi`) and models, so cheap operations use smaller models. Routing selects a CLI and model, not an image.

**Runs start on request.** No background loop starts, submits, syncs, or retries a task. Each run begins with a request (start, feedback, sync, Mark as Done), and the parallel limit is checked when a task starts.

### Recent migrations

The codebase moved off three older designs. The docs and symbols below reflect the current state:

- `sandbox.Type` -> `harness.ID`. The `internal/sandbox` package is deleted; harness identities now live in `internal/harness` (`claude`, `codex`, `cursor`, `opencode`, `pi`).
- Container -> host process. Execution is a host `os/exec`; the `Container*` Go names are kept as legacy vocabulary.
- Refine retired. There is no `refine` agent or `refine-only` flow. Prompt refinement is the Plan task-mode chat (`POST /api/agent/tool/update_task_prompt`).

## Task State Machine

States: `backlog`, `in_progress`, `waiting`, `committing`, `done`, `failed`, `cancelled`.
`archived` is a boolean flag on done/cancelled tasks, not a separate state.

See [Task Lifecycle](task-lifecycle.md) for the transition diagram and what each
state means.

## Turn Loop

```mermaid
flowchart TD
    Start["Start turn<br/>(increment N)"] --> Launch["Launch agent CLI<br/>(host os/exec)<br/>with prompt + session ID"]
    Launch --> Save["Save output to<br/>turn-NNNN.json"]
    Save --> Usage["Accumulate<br/>usage/cost"]
    Usage --> Budget{"Check budgets<br/>MaxCost / MaxTokens"}

    Budget -->|over budget| WaitingBudget["WAITING<br/>(budget_exceeded)"]
    Budget -->|within budget| Parse{"Parse stop_reason"}

    Parse -->|end_turn| Waiting2["WAITING<br/>awaiting review"]
    Parse -->|"max_tokens / pause_turn"| Start
    Parse -->|"empty / unknown"| Waiting["WAITING<br/>blocks until<br/>user feedback"]
    Parse -->|"error / timeout"| FailedError["FAILED<br/>(classify failure<br/>category)"]

    Waiting -->|feedback received| Start
```

## Background Work

No background loop starts, submits, syncs, or retries a task; each run begins with a request. Besides the goroutines those requests start, the server runs the startup recovery pass and two worktree maintenance goroutines (`StartWorktreeGC`, `StartWorktreeHealthWatcher`). See [Background Work and Recovery](background-work.md).

## Agent Roles and the Dispatch Layer

Every task runs one built-in pipeline. `internal/agents/` holds its **Role** descriptors: exactly four built-ins (`oversight`, `commit-msg`, `impl`, `review`; `internal/agents/builtins.go`). A role names what an agent is and which prompt template it renders; the runner's binding table (`internal/runner/agent_bindings.go`) holds the dispatch plumbing per slug (activity, mount mode, timeout, result parser). Each role's harness follows the task's harness, its per-activity override, or the `WALLFACER_SANDBOX_*` settings.

Task execution runs the turn loop in `internal/runner/execute.go`: impl, then the commit pipeline, with full session-recovery semantics. A task or setting that names a harness no longer registered resolves to the next routing tier and, last, to `harness.Default()`.

`Task.FlowID` is a record field kept for stored tasks and is not read for dispatch; a stored task that names a fleet gets one notice on its timeline and runs the same pipeline.

## Component Responsibilities

**Store** (`internal/store/`), In-memory task state guarded by `sync.RWMutex`, persisted through the `StorageBackend` seam. Enforces the state machine via a transition table. Provides pub/sub for live deltas and a full-text search index.

**Runner** (`internal/runner/`), Orchestration engine. Creates worktrees, builds launch specs, execs the agent CLI as a host process, runs the turn loop, accumulates usage, enforces budgets, runs the commit pipeline, and generates oversight in the background.

**Handler** (`internal/handler/`), REST API and SSE endpoints organized by concern.

**Executor** (`internal/executor/`), The `Backend` seam (`Launch`/cancellation) plus `HostBackend`, the single shipping implementation that execs the CLI as a host process and relays its stream-json stdout.

**Harness** (`internal/harness/`), Harness identities, capabilities, and stream parsers for the five subprocess harnesses (`claude`, `codex`, `cursor`, `opencode`, `pi`). `harness.Default()` returns Claude. The `cursor` harness adapts the `cursor-agent` CLI and emits Claude-style stream-json.

**Webserver** (`internal/webserver/`), Owns the catch-all routes `wallfacer run` mounts behind its API (`MountCatchAll`, `internal/webserver/catchall.go`): `index.html` for client-side routes on `GET` and `HEAD`, and the JSON error envelope for a path under `/api` that no route serves. The SPA itself is embedded from `frontend/dist` and served by `mountVueSPA` in `internal/cli/server.go`.

**Frontend** (`frontend/`), Vue 3 + TypeScript SPA (Vite, Vue Router, Pinia). Task board, modals, timeline/flamegraph, diff viewer, usage dashboard. All live updates via SSE.

**Workspace Manager** (`internal/workspace/`), Manages workspace records (stable UUID identity + one folder, persisted in `workspaces.json`), DataKey-scoped stores, and hot-swapping between workspaces without server restart.

### Two storage seams

- `store.StorageBackend` (`internal/store/backend.go`) abstracts the three persistence concerns: tasks (structured, indexed), events (ordered, append-heavy), and blobs (named bytes per task). Domain concepts map onto these primitives; for example `SaveOversight` becomes `SaveBlob(id, "oversight", data)`. Oversight persists as a single blob (`oversight.json`) via `SaveBlob`, not a per-id file under `oversights/`.
- `executor.Backend` (`internal/executor/`) abstracts agent launch. `HostBackend` is the only implementation.

### Request middleware

The request handler chain is wired in `RunServer` (`internal/cli/server.go`) and wraps the mux outside-in, so requests flow logging -> CSRF -> server key -> mux:

- `handler.CSRFMiddleware(hostPort)`, unconditional CSRF protection (no skip flag).
- `handler.BearerAuthMiddleware(serverAPIKey)`, the per-machine server key check. UI shell routes and embedded assets are public; SSE paths take the key as `?token=`. The browser receives the key in `index.html` on a loopback request (`indexKeyAllowed`, `internal/cli/serverkey.go`).

## End-to-End Walkthrough: Task Creation to Merge

This section traces a single task through every component from browser click to merged commit. The sequence diagram shows the full flow; the prose below explains each step.

```mermaid
sequenceDiagram
    participant B as Browser
    participant H as Handler
    participant S as Store
    participant SSE as SSE Subscribers
    participant R as Runner
    participant C as Agent CLI (host process)
    participant G as Git

    B->>H: POST /api/tasks {prompt}
    H->>S: CreateTaskWithOptions()
    S->>S: saveTask() + notify()
    S-->>SSE: SequencedDelta (new task)

    B->>H: PATCH /api/tasks/{id} {status: in_progress}
    H->>S: UpdateTaskStatus()
    S-->>SSE: SequencedDelta (status change)
    H->>R: RunBackground()

    R->>R: setupWorktrees() under worktreeMu
    R->>G: CreateWorktree per workspace
    R->>S: UpdateTaskWorktrees()

    loop Turn loop
        R->>R: generateBoardContextAndMounts()
        R->>C: buildContainerSpecForSandbox() + backend.Launch() (os/exec)
        C-->>R: stream-json stdout (agentOutput)
        R->>S: SaveTurnOutput() + AccumulateSubAgentUsage()
        R->>R: parse stop_reason
    end

    alt end_turn
        R->>S: UpdateTaskStatus(waiting)
        R->>R: GenerateOversightBackground()
    end

    B->>H: POST /api/tasks/{id}/done
    H->>H: CompleteTask()
    H->>S: ForceUpdateTaskStatus(committing)
    H->>H: runCommitTransition()

    R->>R: commit() - Phase 1: hostStageAndCommit()
    R->>C: generateCommitMessage() (host-process agent run)
    R->>G: git add + git commit in worktree

    R->>R: commit() - Phase 2: rebaseAndMerge()
    R->>G: RebaseOntoDefault() + FFMerge()

    R->>R: commit() - Phase 3: cleanup
    R->>R: cleanupWorktrees() under worktreeMu
    R->>G: RemoveWorktree + delete branch

    R->>S: UpdateTaskStatus(done)
    S-->>SSE: SequencedDelta (done)
```

### 1. Task creation

The browser sends `POST /api/tasks` with a prompt and optional goal. `Handler.CreateTask` (`internal/handler/tasks.go`) decodes the request, validates harness availability, and calls `Store.CreateTaskWithOptions` (`internal/store/tasks_create_delete.go`). The store assigns a UUID, takes the title from the request or else from the prompt's first non-blank line (`store.DeriveTitle`), writes `task.json` atomically (temp file + rename), adds the task to the in-memory map, and calls `notify()` which fans the new `SequencedDelta` to all SSE subscribers.

### 2. Move to in_progress

The browser sends `PATCH /api/tasks/{id}` with `{status: "in_progress"}`. `Handler.UpdateTask` (`internal/handler/tasks.go`) checks concurrency limits via `checkConcurrencyAndUpdateStatus`, transitions the store status, inserts a `state_change` event, and calls `Runner.RunBackground` (`internal/runner/runner.go`). `RunBackground` registers the goroutine label with `backgroundWg.Add` and launches `Runner.Run` in a new goroutine. Inside `Run` (`internal/runner/execute.go`), the first thing is worktree setup: `setupWorktrees` (`internal/runner/worktree.go`) acquires `worktreeMu`, creates one git worktree per workspace via `gitutil.CreateWorktree`, and returns the worktree-path map and branch name (e.g. `task/abcd1234`). The runner persists these paths via `Store.UpdateTaskWorktrees`.

### 3. Turn loop

The turn loop in `Run` increments the turn counter, refreshes the board context via `generateBoardContextAndMounts` (`internal/runner/board.go`), and calls `runContainer` (`internal/runner/container.go`). That function builds the launch spec via `buildContainerSpecForSandbox`, resolves the harness and model per activity, and invokes `backend.Launch`, which execs the agent CLI directly via `os/exec` with the worktree as CWD. The stream-json stdout is parsed into an `agentOutput` struct. The runner saves raw output via `Store.SaveTurnOutput`, accumulates token usage via `Store.AccumulateSubAgentUsage` and `Store.AppendTurnUsage`, then inspects `output.StopReason` to decide the next step.

### 4. Waiting state

When `stop_reason` is `"end_turn"`, the runner transitions the task to `waiting` via `Store.UpdateTaskStatus`, inserts a `state_change` event, and opens a `feedback_waiting` span. `GenerateOversightBackground` fires an asynchronous oversight summary generation (a host-process agent run). The `notify()` call inside the status update fans a delta to SSE subscribers. If `stop_reason` is `"max_tokens"` or `"pause_turn"`, the loop auto-continues by setting `prompt = ""` and resuming the same session.

### 5. Mark done and commit pipeline

The user clicks "Mark as Done", sending `POST /api/tasks/{id}/done`. `Handler.CompleteTask` (`internal/handler/execute.go`) verifies the task is in `waiting`, restores any missing worktrees, transitions to `committing` via `Store.ForceUpdateTaskStatus`, and calls `runCommitTransition` which launches `Runner.Commit` (`internal/runner/commit.go`) in a background goroutine. The commit pipeline has three phases. **Phase 1** (`hostStageAndCommit`) stages and commits host-side: it runs `git add` and `git commit` in each worktree on the host, using a commit message produced by `generateCommitMessage`, which is itself a host-process agent run (the `commit-msg` role). **Phase 2** (`rebaseAndMerge`) acquires the per-repo mutex via `repoLock()`, calls `gitutil.RebaseOntoDefault` with up to 3 conflict-resolution retries (each retry runs a host-process conflict-resolver agent), then `gitutil.FFMerge` to fast-forward the default branch. **Phase 3** persists commit hashes, and cleans up worktrees via `cleanupWorktrees` (under `worktreeMu`).

### 6. Done

After the commit pipeline succeeds, `runCommitTransition` transitions the task to `done` via `Store.ForceUpdateTaskStatus`. The store persists the status and notifies SSE subscribers. The worktree directories and task branch have already been removed in Phase 3. A `TaskSummary` is written for the cost dashboard.

## Concurrency Model

### Mutex domains

| Mutex | Location | Protects | Lock pattern | Typical hold |
|---|---|---|---|---|
| `Store.mu` | `internal/store/store.go` | In-memory task map, status index, search index, event maps | Write lock for all mutations (`mutateTask`, `CreateTaskWithOptions`, status updates); read lock for queries (`ListTasks`, `GetTask`) | Microseconds (in-memory map ops + atomic file write) |
| `Runner.worktreeMu` | `internal/runner/runner.go` | All worktree filesystem operations on `worktreesDir` | Exclusive lock in `setupWorktrees`, `ensureTaskWorktrees`, `cleanupWorktrees`, `CleanupWorktrees`, `PruneUnknownWorktrees` | Milliseconds to seconds (git worktree create/remove) |
| `Runner.repoMu` (per-repo) | `internal/runner/runner.go` | Rebase + merge serialization per repository | Exclusive lock via `repoLock(repoPath)` in `rebaseAndMerge`; tasks on different repos run concurrently | Seconds (rebase + merge + optional conflict resolution) |
| `Runner.oversightMu` (per-task) | `internal/runner/runner.go` | Serializes oversight generation per task | Exclusive lock via `oversightLock(taskID)` in `GenerateOversight` | Seconds (host-process agent run) |
| `Store.subMu` | `internal/store/subscribe.go` | SSE subscriber map | Exclusive lock during `Subscribe`, `Unsubscribe`, and the fan-out in `notify()` | Microseconds |
| `Store.replayMu` | `internal/store/subscribe.go` | Replay buffer (ring of recent deltas) | Write lock in `notify()`; read lock in `DeltasSince()` | Microseconds |
| `Runner.boardCache.mu` | `internal/runner/runner.go` | Board context JSON cache and mount cache | Exclusive lock for cache read/write in `generateBoardContextAndMounts` | Microseconds |
| `Runner.storeMu` | `internal/runner/runner.go` | Runner's pointer to the active `*store.Store` (swapped on workspace switch) | Write lock in `applyWorkspaceSnapshot`; read lock in `currentStore` | Microseconds |

### Goroutine model

There is no worker pool. Each task execution gets its own goroutine via `Runner.RunBackground`, which calls `backgroundWg.Add(label)` before launching `go r.Run(...)` and `backgroundWg.Done(label)` in a deferred cleanup. The same `backgroundWg` (`trackedwg.WaitGroup`) tracks all fire-and-forget background work: oversight generation (`GenerateOversightBackground`) and worktree sync (`SyncWorktreesBackground`). Each goroutine registers with a human-readable label (e.g. `"run:abcd1234"`, `"oversight:abcd1234"`). `Runner.PendingGoroutines()` returns the sorted list of outstanding labels for diagnostics.

### Pub/sub channels

The store fans every change out to full-delta subscribers:

- **Full-delta channels** (`Subscribe`): returns `(int, <-chan SequencedDelta)`. Channels are buffered at 256 (`pubsub.DefaultChannelSize`). Each mutation calls `notify()` which stamps a monotonic `deltaSeq`, appends to a bounded replay buffer (512 entries), and fans out a deep-copied `SequencedDelta` to every subscriber. If a subscriber's buffer is full, the delta is silently dropped. SSE reconnection uses `DeltasSince(seq)` to replay missed deltas from the buffer before falling back to a full snapshot.

The fan-out happens inside `notify()` (`internal/store/subscribe.go`), which is always called while `Store.mu` is held, ensuring the delta sequence is consistent with the in-memory state.

### Shutdown coordination

```mermaid
sequenceDiagram
    participant Sig as OS Signal
    participant Srv as HTTP Server
    participant R as Runner
    participant BG as Background Goroutines

    Sig->>Srv: SIGTERM / SIGINT (signal.NotifyContext)
    Srv->>Srv: ctx.Done() -> srv.Shutdown(5s timeout)
    Note over Srv: SSE handlers exit via cancelled base context
    Srv->>R: r.Shutdown()
    R->>R: shutdownCancel() -> cancel shutdownCtx
    R->>R: close(shutdownCh) -> board subscription exits
    R->>R: boardSubscriptionWg.Wait()
    R->>BG: backgroundWg.Wait()
    Note over R: Logs pending goroutines every 3s while waiting
    R-->>Srv: Shutdown() returns
```

The shutdown sequence is driven by `signal.NotifyContext(ctx, SIGTERM, Interrupt)` in `RunServer` (`internal/cli/server.go`). When a signal arrives, `ctx.Done()` fires. The HTTP server gets `srv.Shutdown(5s)` to drain in-flight requests; SSE handlers exit immediately because their request contexts derive from the now-cancelled base context. Then `Runner.Shutdown()` (`internal/runner/runner.go`) is called: it invokes `shutdownCancel()` to cancel `shutdownCtx` (which propagates to any agent launches or store operations using it), closes `shutdownCh` to stop the board-cache subscription goroutine, waits on `boardSubscriptionWg`, then waits on `backgroundWg` with a 3-second ticker that logs still-pending goroutine labels. Running agent processes are not waited for; on the next startup `RecoverOrphanedTasks` (`internal/runner/recovery.go`) moves their tasks to `waiting`.

## Where to Look

Quick-reference for common maintenance tasks. Each entry names the starting file and the typical next steps.

| If you need to... | Start here |
|---|---|
| Add a new API endpoint | `internal/apicontract/routes.go` -> `internal/handler/<concern>.go` -> run `make api-contract` |
| Add a field to Task | `internal/store/models.go` -> `internal/store/migrate.go` |
| Change the turn loop | `internal/runner/execute.go` (`Run()`) |
| Change the commit pipeline | `internal/runner/commit.go` (`commit()`, `hostStageAndCommit()`, `rebaseAndMerge()`) + `latere.ai/x/pkg/gitutil/ops.go` |
| Change the agent launch spec | `internal/runner/container.go` (`buildContainerSpecForSandbox()`) + `internal/executor/host.go` |
| Add or change a harness | `internal/harness/` (`claude.go`, `codex.go`, `cursor.go`, `opencode.go`, `pi.go`, `registry.go`) |
| Add a new env config variable | `internal/envconfig/envconfig.go` |
| Change workspace switching | `internal/workspace/manager.go` (`Switch()`) |
| Debug a failing rebase | `latere.ai/x/pkg/gitutil/ops.go` + `latere.ai/x/pkg/gitutil/stash.go` |
| Understand why a task failed | `data/<key>/<uuid>/traces/` + `outputs/turn-NNNN.json` |
| Add a new system prompt | a `.tmpl` file in `internal/prompts/`, its data type and rendering function in `internal/prompts/prompts.go` |
| Change the UI | `frontend/src/` (Vue components, composables, Pinia stores) |
| Debug startup recovery | `internal/runner/recovery.go` (`RecoverOrphanedTasks()`) |
| Change pub/sub behavior | `internal/store/subscribe.go` (`notify()`, `Subscribe()`) |

## Package Map

Every `internal/` package and its role in the system:

| Package | Purpose | Key exported types / functions |
|---|---|---|
| `agents` | The four built-in role descriptors: `oversight`, `commit-msg`, `impl`, `review` | `Role`, `BuiltinAgents` |
| `apicontract` | Single source of truth for all HTTP API routes; generates `docs/internals/api-contract.json` | `Route`, `Routes` (slice), `Route.FullPattern()` |
| `cli` | CLI subcommand implementations (run, status, doctor/env, spec) and shared helpers | `RunServer()`, `RunStatus()`, `RunDoctor()`, `RunSpec()`, `BuildMux()`, `ConfigDir()` |
| `envconfig` | `.env` file parsing and atomic update | `Config`, `Parse()`, `Update()` |
| `executor` | Agent-launch seam plus the single host-process implementation | `Backend`, `HostBackend`, `NewHostBackend()`, `ContainerSpec`, `Request` |
| `handler` | HTTP API handlers organized by concern | `Handler`, `NewHandler()`, `CSRFMiddleware()`, `BearerAuthMiddleware()`, `MaxBytesMiddleware()` |
| `harness` | Harness identities, capabilities, and stream parsers for the five subprocess harnesses (`claude`, `codex`, `cursor`, `opencode`, `pi`); replaces the deleted `sandbox` package | `ID`, `Claude`, `Codex`, `Cursor`, `OpenCode`, `Pi`, `Harness`, `Register()`, `Lookup()`, `Default()` |
| `logger` | Structured logging via `log/slog` with per-component named loggers | `Init()`, `Fatal()`, `Main`, `Runner`, `Store`, `Git`, `Handler`, `Recovery` |
| `runner` | Orchestration, turn loop, commit pipeline, worktree management (execs agents as host processes) | `Runner`, `NewRunner()`, `RunnerConfig`, `ContainerInfo`, `Interface` |
| `store` | Per-task persistence (via `StorageBackend`), data models, event sourcing, pub/sub | `Store`, `Task`, `TaskEvent`, `TaskUsage`, `SandboxActivity`, `TaskDelta`, `StorageBackend` |
| `testenv` | Test-only: runs a package's tests with the per-user directories pointed at a temporary directory and the coding-agent CLIs removed from `PATH`, so tests never touch the real configuration and never launch a real agent | `RunIsolated()`, `AgentCLIs`, `AgentCLIsOnPath()`, `Within()` |
| `webserver` | The catch-all routes beside the API: the SPA shell for client routes, the JSON error envelope for unmatched `/api` paths | `MountCatchAll()` |
| `workspace` | Workspace lifecycle manager; stable-identity workspace records (`workspaces.json`, migrated from `workspace-groups.json`); DataKey-scoped data directories; hot-swap and per-workspace parallelism settings | `Manager`, `Workspace`, `Snapshot`, `NewManager()`, `LoadGroups()`, `SaveGroups()`, `MigrateToWorkspaces()` |
| `constants` | Consolidated system parameters: timeouts, intervals, retry counts, size limits | Named constants grouped by concern |
| `agentsession` | Long-lived workspace-scoped agent-session lifecycle; per-session `messages.jsonl` + `session.json` under `~/.wallfacer/agent-sessions/<fp>/`; slash-command template expansion; single-turn-at-a-time coordination | `Runtime`, `Manager`, `ConversationStore`, `CommandRegistry`, `SessionMeta`, `Slugify`, `Expand` |
| `spec` | Spec document model: YAML frontmatter parse/write round-trip; seven-state lifecycle state machine; recursive tree builder; per-spec + cross-spec validation; atomic scaffold (`O_CREATE\|O_EXCL`); progress aggregation; impact analysis; roadmap README index resolution | `Spec`, `Status`, `Effort`, `StatusMachine`, `Tree`, `BuildTree()`, `ParseFile()`, `Scaffold()`, `ValidateSpec()`, `UpdateFrontmatter()`, `ResolveIndex()` |
| `prompts` | Embedded system prompt templates (commit, oversight, conflict, drift) and the data-key helpers used to scope per-workspace data directories | `CommitMessage()`, `Oversight()`, `WorkspaceDataKey()`, `NewDataKey()` |

Shared utility packages under `internal/pkg/`:

| Package | Purpose | Key exported types / functions |
|---|---|---|
| `pkg/livelog` | Concurrency-safe append-only byte buffer with multiple readers for live streaming | `Log`, `New()`, `Log.NewReader()`, `Reader.ReadChunk()` |
| `pkg/sse` | Server-Sent Events writer for `http.ResponseWriter` | `Writer`, `NewWriter()`, `Writer.Event()`, `Writer.JSON()`, `Writer.Heartbeat()` |

General-purpose helpers shared with other latere.ai services, such as atomic file writes, JSON request and response helpers, caches, circuit breakers, state machines, DAG operations, and git operations (`gitutil`), come from the `latere.ai/x/pkg` module.

## Handler Organization

Each handler file in `internal/handler/` owns a specific concern area. The table below lists representative non-test `.go` files.

| File | Concern | Key endpoints |
|---|---|---|
| `handler.go` | Core `Handler` struct, constructor, JSON helpers, workspace snapshot subscription |, (shared infrastructure) |
| `middleware.go` | Request middleware: `CSRFMiddleware`, `BearerAuthMiddleware`, `MaxBytesMiddleware` |, (middleware, not endpoints) |
| `tasks.go` | Task CRUD, status transitions. Cancel/archive/unarchive/restore fold into `PATCH /api/tasks/{id}`; resume/sync/done stay dedicated side-effect endpoints | `POST /api/tasks`, `PATCH /api/tasks/{id}`, `POST /api/tasks/{id}/resume`, etc. |
| `tasks_events.go` | Task event timeline, per-turn output serving, turn usage | `GET /api/tasks/{id}/events`, `GET /api/tasks/{id}/outputs/{filename}`, `GET /api/tasks/{id}/turn-usage` |
| `tasks_concurrency.go` | The transition lock and the parallel-task limit | `checkConcurrencyAndUpdateStatus()` (internal helper) |
| `stream.go` | SSE streaming for live task updates and agent logs | `GET /api/tasks/stream`, `GET /api/tasks/{id}/logs` |
| `config.go` | Server configuration (workspaces, workspace groups and their parallel limits, harness list) | `GET /api/config`, `PUT /api/config` |
| `env.go` | Environment configuration (API tokens, model settings, harness routing) | `GET /api/env`, `PUT /api/env`, `POST /api/env/test` |
| `git.go` | Git workspace operations (status, push, sync, rebase, branches, checkout) | `GET /api/git/status`, `POST /api/git/push`, `POST /api/git/sync`, etc. |
| `execute.go` | Task execution trigger (delegates to runner) |, (internal, called by task status transitions) |
| `oversight.go` | Task oversight summary retrieval | `GET /api/tasks/{id}/oversight` (impl + test phases via `?phase=`) |
| `spans.go` | Per-task span timing | `GET /api/tasks/{id}/spans` |
| `debug.go` | Health check and board manifest | `GET /api/debug/health`, `GET /api/debug/board`, `GET /api/tasks/{id}/board` |
| `sandbox_gate.go` | Harness usability checks (auth validation before task launch) |, (internal helpers) |
| `agentsession.go` | Agent session chat: messages, streaming, interrupt, commands | `GET/POST/DELETE /api/agent/messages`, `GET /api/agent/messages/stream`, `POST /api/agent/messages/interrupt`, `GET /api/agent/commands` |
| `agentsession_tool.go` | Plan task-mode tools, including prompt refinement | `POST /api/agent/tool/update_task_prompt` |
| `agentsession_threads.go` | Agent session CRUD (list, create, rename, archive, unarchive, activate) | `GET/POST /api/agent/sessions`, `PATCH /api/agent/sessions/{id}` |
| `planning_undo.go` | Undo the caller session's most recent planning round via `git revert`; cancels board tasks whose `dispatched_task_id` was added in the reverted commit | `POST /api/agent/undo` |
| `specs.go` | Spec tree with metadata, progress, and archive/unarchive transitions | `GET /api/specs/tree`, `GET /api/specs/stream`, `POST /api/specs/transition` |
| `specs_dispatch.go` | Atomic dispatch/undispatch pipeline that creates board tasks from validated leaf specs and writes `dispatched_task_id` back into the spec frontmatter | `POST /api/specs/transition` (`action: dispatch\|undispatch`) |

There is no `refine.go` (prompt refinement is the agent session tool above) and no `containers.go` / `GET /api/containers` endpoint.

## Structured Logging

The `internal/logger` package provides named loggers built on `log/slog`:

| Logger | Component tag | Used by |
|---|---|---|
| `logger.Main` | `main` | CLI startup, server lifecycle, shutdown |
| `logger.Runner` | `runner` | Orchestration, turn loop, commit pipeline |
| `logger.Store` | `store` | Task persistence, state transitions |
| `logger.Git` | `git` | Worktree and git operations |
| `logger.Handler` | `handler` | HTTP request handling |
| `logger.Recovery` | `recovery` | Orphaned task recovery on startup |

`logger.Init(format)` configures all loggers. Two formats are supported:
- **`"text"`** (default), Human-friendly output with ANSI colors (when stdout is a terminal), aligned columns: timestamp, 3-char level badge, 8-char component, source file:line, bold message, dim key=value pairs. Respects `NO_COLOR` and `TERM=dumb`.
- **`"json"`**, Structured JSON via `slog.NewJSONHandler`, suitable for log aggregation.

`logger.Fatal(msg, args...)` prints a user-friendly error to stderr and exits with code 1 (used for startup errors, not for runtime failures).

## Cross-Cutting Concerns

**Concurrency**, `Store.mu` for task map integrity; `Runner.worktreeMu` for filesystem ops; per-repo mutex for rebase serialization; per-task mutex for oversight generation. See [Data & Storage](data-and-storage.md) for the concurrency model.

**Recovery**, On startup, `RecoverOrphanedTasks` moves `in_progress` tasks to `waiting` (or `failed` when a worktree is missing) and settles `committing` tasks by their branch tips. See [Background Work and Recovery](background-work.md).

**Security**, a per-machine server key, SSRF-hardened gateway URLs, path traversal guards, CSRF protection, request body size limits.

**Observability**, SSE event streams, append-only trace timeline per task, span timing, structured logs.

**Middleware**, See [API & Transport](api-and-transport.md) for the middleware chain.

**Harness routing**, See [Workspaces & Configuration](workspaces-and-config.md) for per-activity harness and model routing.

**Graceful shutdown**, See [API & Transport](api-and-transport.md) for the shutdown sequence.

## See Also

- [Development Setup](development.md), building from source, tests, make targets, releases
- [Data & Storage](data-and-storage.md), persistence, models, migrations, search index
- [Task Lifecycle](task-lifecycle.md), states, turn loop, board context
- [Git Operations](git-worktrees.md), worktrees, commit pipeline, branch management
- [Workspaces & Configuration](workspaces-and-config.md), workspace manager, AGENTS.md, harnesses, templates
- [API & Transport](api-and-transport.md), HTTP routes, SSE, metrics, middleware
- [Background Work and Recovery](background-work.md), goroutine model, startup recovery, oversight generation