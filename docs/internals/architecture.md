# Architecture

Wallfacer is a host-native Go service that coordinates autonomous coding agents running in ephemeral containers, with per-task git worktree isolation and a web task board for human oversight.

## System Overview

```mermaid
graph TB
    subgraph Browser
        UI["Browser UI<br/>(Vanilla JS + Tailwind + Sortable.js)<br/>Board + Plan (spec explorer, minimap, planning chat)"]
    end

    subgraph Server["Go Server (stdlib net/http)"]
        Handler["Handler<br/>REST API + SSE"]
        Runner["Runner<br/>orchestration + commit"]
        Store["Store<br/>state + persistence"]
        Automation["Automation Loops<br/>promote / test / submit<br/>sync / retry / refine / ideation"]
        Plan["Plan Mode<br/>spec tree + planner<br/>dispatch + undo"]

        Handler --> Runner
        Handler --> Plan
        Runner --> Store
        Automation --> Store
        Plan --> Store
        Store -.->|pub/sub| Automation
    end

    subgraph Infra["Host Infrastructure"]
        Containers["Sandbox Containers<br/>Claude / Codex images<br/>ephemeral task + long-lived planning"]
        Worktrees["Per-task Git Worktrees<br/>~/.wallfacer/worktrees/<br/>task/ID branches"]
        SpecsFS["specs/ (markdown + frontmatter)<br/>planning threads<br/>~/.wallfacer/planning/&lt;fp&gt;/"]
        Containers --- Worktrees
    end

    UI -->|"HTTP / SSE"| Handler
    Runner -->|os/exec| Containers
    Plan -->|os/exec| Containers
    Plan -->|read/write| SpecsFS
```

## Design Decisions

**Filesystem-first persistence.** No database. Each task is a directory (`data/<uuid>/`) containing `task.json`, traces, outputs, and oversight summaries. Writes are atomic (temp file + rename). Easy to inspect, back up, and debug.

**Container isolation.** Every agent turn runs in a fresh ephemeral container launched via `os/exec`. The container sees only its task's worktree mounted at `/workspace`. Tasks cannot interfere with each other or the host.

**Git worktree isolation.** Each task gets its own worktree on a `task/<id>` branch. Tasks work in parallel without merge conflicts during execution. Rebase/merge happens at commit time.

**Activity-routed sandboxes.** Different activities (implementation, testing, oversight, title, etc.) can route to different sandbox images and models, so cheap operations use smaller models.

**Automation with guardrails.** Background loops handle promotion, testing, submission, and retry — each with explicit controls (toggles, budgets, thresholds).

## Task State Machine

```mermaid
stateDiagram-v2
    [*] --> backlog

    backlog --> in_progress : drag / autopilot

    in_progress --> in_progress : max_tokens / pause_turn (auto-continue)
    in_progress --> waiting : end_turn
    in_progress --> waiting : empty stop_reason
    in_progress --> failed : error / timeout / budget
    in_progress --> backlog : reset
    in_progress --> cancelled : cancel

    committing --> done : commit success
    committing --> failed : commit failure

    waiting --> in_progress : feedback
    waiting --> in_progress : test (IsTestRun)
    waiting --> committing : mark done
    waiting --> cancelled : cancel

    failed --> backlog : retry / auto_retry
    failed --> cancelled : cancel

    done --> cancelled : cancel
    cancelled --> backlog : retry

    note right of waiting
        sync: rebase onto default branch
    end note
```

States: `backlog`, `in_progress`, `waiting`, `committing`, `done`, `failed`, `cancelled`.
`archived` is a boolean flag on done/cancelled tasks, not a separate state.

## Turn Loop

```mermaid
flowchart TD
    Start["Start turn<br/>(increment N)"] --> Launch["Launch container<br/>with prompt + session ID"]
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

## Background Automation

```mermaid
flowchart LR
    PubSub["Store<br/>pub/sub on<br/>state changes"]

    PubSub --> Promoter["Auto-promoter<br/>backlog to in_progress<br/>when capacity available<br/>+ deps met + scheduled"]
    PubSub --> Tester["Auto-tester<br/>launch test verification<br/>on untested waiting tasks"]
    PubSub --> Submitter["Auto-submitter<br/>waiting to done<br/>when test passed<br/>+ conflict-free"]
    PubSub --> Sync["Waiting-sync<br/>rebase worktrees<br/>behind default branch"]
    PubSub --> Retry["Auto-retry<br/>failed to backlog<br/>if retry budget > 0"]
    PubSub --> Refiner["Auto-refiner<br/>launch refinement agent<br/>on unrefined backlog tasks"]
    PubSub --> Routines["Routine engine<br/>fire scheduled routines<br/>spawn tasks against flow"]
```

### Agents, flows, and the dispatch layer

At task execution time the runner consults two registries before it invokes any CLI:

- `internal/agents/` holds the **Role** descriptors (`impl`, `test`, `refine`, `title`, `oversight`, `commit-msg`, `ideate`) plus any user-authored clones loaded from `~/.wallfacer/agents/`. A role pins a harness (Claude or Codex), declares capabilities, and optionally carries a system-prompt preamble.
- `internal/flow/` holds **Flow** definitions: ordered step chains that reference roles by slug. Four built-ins ship (`implement`, `brainstorm`, `refine-only`, `test-only`); user flows live under `~/.wallfacer/flows/`.

Both directories are fsnotify-watched; edits reload the merged registry within ~150 ms without restarting the server.

Task execution picks one of three dispatch paths:

- `flow == "implement"` → the turn-loop path in `execute.go` (refine → impl → test → commit pipeline with full session-recovery semantics).
- `flow == "brainstorm"` (or legacy `Kind = idea-agent`) → `runIdeationTask`, which parses ideate output and creates backlog tasks.
- any other flow slug → the flow engine in `internal/flow/engine.go`. It walks steps linearly, fans parallel-sibling groups through an `errgroup`, and launches each role via `Runner.RunAgent`.

See [Agents & Flows](../guide/agents-and-flows.md) for the full user-facing model.

## Component Responsibilities

**Store** (`internal/store/`) — In-memory task state guarded by `sync.RWMutex`, backed by per-task directory persistence. Enforces the state machine via a transition table. Provides pub/sub for live deltas and a full-text search index.

**Runner** (`internal/runner/`) — Orchestration engine. Creates worktrees, builds container specs, executes the turn loop, accumulates usage, enforces budgets, runs the commit pipeline, and generates titles/oversight in the background.

**Handler** (`internal/handler/`) — REST API and SSE endpoints organized by concern. Hosts automation toggle controls.

**Frontend** (`ui/`) — Vanilla JS modules. Task board, modals, timeline/flamegraph, diff viewer, usage dashboard. All live updates via SSE.

**Workspace Manager** (`internal/workspace/`) — Manages workspace configuration, workspace groups, and hot-swapping between workspace sets without server restart.

## End-to-End Walkthrough: Task Creation to Merge

This section traces a single task through every component from browser click to merged commit. The sequence diagram shows the full flow; the prose below explains each step.

```mermaid
sequenceDiagram
    participant B as Browser
    participant H as Handler
    participant S as Store
    participant SSE as SSE Subscribers
    participant R as Runner
    participant C as Container
    participant G as Git

    B->>H: POST /api/tasks {prompt}
    H->>S: CreateTaskWithOptions()
    S->>S: saveTask() + notify()
    S-->>SSE: SequencedDelta (new task)
    H->>R: GenerateTitleBackground()

    B->>H: PATCH /api/tasks/{id} {status: in_progress}
    H->>S: UpdateTaskStatus()
    S-->>SSE: SequencedDelta (status change)
    H->>R: RunBackground()

    R->>R: setupWorktrees() under worktreeMu
    R->>G: CreateWorktree per workspace
    R->>S: UpdateTaskWorktrees()

    loop Turn loop
        R->>R: generateBoardContextAndMounts()
        R->>C: buildContainerArgsForSandbox() + executor.RunArgs()
        C-->>R: NDJSON stdout (agentOutput)
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

    R->>R: commit() — Phase 1: hostStageAndCommit()
    R->>C: generateCommitMessage() container
    R->>G: git add + git commit in worktree

    R->>R: commit() — Phase 2: rebaseAndMerge()
    R->>G: RebaseOntoDefault() + FFMerge()

    R->>R: commit() — Phase 3: cleanup
    R->>R: cleanupWorktrees() under worktreeMu
    R->>G: RemoveWorktree + delete branch

    R->>S: UpdateTaskStatus(done)
    S-->>SSE: SequencedDelta (done)
```

### 1. Task creation

The browser sends `POST /api/tasks` with a prompt and optional goal. `Handler.CreateTask` (`internal/handler/tasks.go`) decodes the request, validates sandbox availability, and calls `Store.CreateTaskWithOptions` (`internal/store/tasks_create_delete.go`). The store assigns a UUID, writes `task.json` atomically (temp file + rename), adds the task to the in-memory map, and calls `notify()` which fans the new `SequencedDelta` to all SSE subscribers. Back in the handler, `Runner.GenerateTitleBackground` (`internal/runner/runner.go`) fires a background goroutine tracked by `backgroundWg` that runs a lightweight container to generate a short title from the prompt.

### 2. Move to in_progress

The browser sends `PATCH /api/tasks/{id}` with `{status: "in_progress"}`. `Handler.UpdateTask` (`internal/handler/tasks.go`) checks concurrency limits via `checkConcurrencyAndUpdateStatus`, transitions the store status, inserts a `state_change` event, and calls `Runner.RunBackground` (`internal/runner/runner.go`). `RunBackground` registers the goroutine label with `backgroundWg.Add` and launches `Runner.Run` in a new goroutine. Inside `Run` (`internal/runner/execute.go`), the first thing is worktree setup: `setupWorktrees` (`internal/runner/worktree.go`) acquires `worktreeMu`, creates one git worktree per workspace via `gitutil.CreateWorktree`, and returns the worktree-path map and branch name (e.g. `task/abcd1234`). The runner persists these paths via `Store.UpdateTaskWorktrees`.

### 3. Turn loop

The turn loop in `Run` increments the turn counter, refreshes the board context via `generateBoardContextAndMounts` (`internal/runner/board.go`), and calls `runContainer` (`internal/runner/container.go`). That function builds the container spec via `buildContainerArgsForSandbox`, resolves the sandbox type per activity, checks the circuit breaker, and invokes `executor.RunArgs` which runs `podman/docker run` via `os/exec`. The NDJSON stdout is parsed into an `agentOutput` struct. The runner saves raw output via `Store.SaveTurnOutput`, accumulates token usage via `Store.AccumulateSubAgentUsage` and `Store.AppendTurnUsage`, then inspects `output.StopReason` to decide the next step.

### 4. Waiting state

When `stop_reason` is `"end_turn"`, the runner transitions the task to `waiting` via `Store.UpdateTaskStatus`, inserts a `state_change` event, and opens a `feedback_waiting` span. `GenerateOversightBackground` fires an asynchronous oversight summary generation. The `notify()` call inside the status update fans a delta to SSE subscribers and wakes automation watchers (auto-tester, auto-submitter) via the `SubscribeWake` channels. If `stop_reason` is `"max_tokens"` or `"pause_turn"`, the loop auto-continues by setting `prompt = ""` and resuming the same session.

### 5. Mark done and commit pipeline

The user clicks "Mark as Done", sending `POST /api/tasks/{id}/done`. `Handler.CompleteTask` (`internal/handler/execute.go`) verifies the task is in `waiting`, restores any missing worktrees, transitions to `committing` via `Store.ForceUpdateTaskStatus`, and calls `runCommitTransition` which launches `Runner.Commit` (`internal/runner/commit.go`) in a background goroutine. The commit pipeline has three phases: **Phase 1** (`hostStageAndCommit`) runs `git add -A` and `git commit` in each worktree using a commit message generated by `generateCommitMessage` (a lightweight container invocation). **Phase 2** (`rebaseAndMerge`) acquires the per-repo mutex via `repoLock()`, calls `gitutil.RebaseOntoDefault` with up to 3 conflict-resolution retries (each retry runs a conflict-resolver container), then `gitutil.FFMerge` to fast-forward the default branch. **Phase 3** persists commit hashes, cleans up worktrees via `cleanupWorktrees` (under `worktreeMu`), and optionally auto-pushes.

### 6. Done

After the commit pipeline succeeds, `runCommitTransition` transitions the task to `done` via `Store.ForceUpdateTaskStatus`. The store persists the status, notifies SSE subscribers, and wakes watchers. The worktree directories and task branch have already been removed in Phase 3. A `TaskSummary` is written to `summary.json` for the cost dashboard.

## Concurrency Model

### Mutex domains

| Mutex | Location | Protects | Lock pattern | Typical hold |
|---|---|---|---|---|
| `Store.mu` | `internal/store/store.go` | In-memory task map, status index, search index, event maps | Write lock for all mutations (`mutateTask`, `CreateTaskWithOptions`, status updates); read lock for queries (`ListTasks`, `GetTask`) | Microseconds (in-memory map ops + atomic file write) |
| `Runner.worktreeMu` | `internal/runner/runner.go` | All worktree filesystem operations on `worktreesDir` | Exclusive lock in `setupWorktrees`, `ensureTaskWorktrees`, `cleanupWorktrees`, `CleanupWorktrees`, `PruneUnknownWorktrees` | Milliseconds to seconds (git worktree create/remove) |
| `Runner.repoMu` (per-repo) | `internal/runner/runner.go` | Rebase + merge serialization per repository | Exclusive lock via `repoLock(repoPath)` in `rebaseAndMerge`; tasks on different repos run concurrently | Seconds (rebase + merge + optional conflict resolution) |
| `Runner.oversightMu` (per-task) | `internal/runner/runner.go` | Serializes oversight generation per task | Exclusive lock via `oversightLock(taskID)` in `GenerateOversight` | Seconds (container invocation) |
| `Store.subMu` | `internal/store/subscribe.go` | SSE subscriber map | Exclusive lock during `Subscribe`, `Unsubscribe`, and the fan-out in `notify()` | Microseconds |
| `Store.wakeSubMu` | `internal/store/subscribe.go` | Wake-only subscriber map | Exclusive lock during `SubscribeWake`, `UnsubscribeWake`, and the fan-out in `notify()` | Microseconds |
| `Store.replayMu` | `internal/store/subscribe.go` | Replay buffer (ring of recent deltas) | Write lock in `notify()`; read lock in `DeltasSince()` | Microseconds |
| `Runner.boardCache.mu` | `internal/runner/runner.go` | Board context JSON cache and mount cache | Exclusive lock for cache read/write in `generateBoardContextAndMounts` | Microseconds |
| `Runner.storeMu` | `internal/runner/runner.go` | Runner's pointer to the active `*store.Store` (swapped on workspace switch) | Write lock in `applyWorkspaceSnapshot`; read lock in `currentStore` | Microseconds |

### Goroutine model

There is no worker pool. Each task execution gets its own goroutine via `Runner.RunBackground`, which calls `backgroundWg.Add(label)` before launching `go r.Run(...)` and `backgroundWg.Done(label)` in a deferred cleanup. The same `backgroundWg` (`trackedWg`) tracks all fire-and-forget background work: title generation (`GenerateTitleBackground`), oversight generation (`GenerateOversightBackground`), worktree sync (`SyncWorktreesBackground`), and refinement (`RunRefinementBackground`). Each goroutine registers with a human-readable label (e.g. `"run:abcd1234"`, `"title:abcd1234"`). `Runner.PendingGoroutines()` returns the sorted list of outstanding labels for diagnostics.

Automation watchers (`StartAutoPromoter`, `StartAutoRetrier`, `StartAutoTester`, `StartAutoSubmitter`, `StartAutoRefiner`, `StartWaitingSyncWatcher`, `StartIdeationWatcher`) each run as a single long-lived goroutine started in `RunServer` (`internal/cli/server.go`). They block on `SubscribeWake` channels and wake when any task mutates, then inspect the current task list to decide whether to act.

### Pub/sub channels

The store provides two subscriber tiers:

- **Full-delta channels** (`Subscribe`): returns `(int, <-chan SequencedDelta)`. Channels are buffered at 256 (`pubsub.DefaultChannelSize`). Each mutation calls `notify()` which stamps a monotonic `deltaSeq`, appends to a bounded replay buffer (512 entries), and fans out a deep-copied `SequencedDelta` to every subscriber. If a subscriber's buffer is full, the delta is silently dropped. SSE reconnection uses `DeltasSince(seq)` to replay missed deltas from the buffer before falling back to a full snapshot.

- **Wake-only channels** (`SubscribeWake`): returns `(int, <-chan struct{})`. Channels are buffered at 1. The capacity-1 design coalesces rapid bursts: once a signal is pending, further sends are no-ops. Automation watchers use this tier to avoid allocating full `SequencedDelta` copies when they only need a "something changed" signal.

Both fan-outs happen inside `notify()` (`internal/store/subscribe.go`), which is always called while `Store.mu` is held, ensuring the delta sequence is consistent with the in-memory state.

### Shutdown coordination

```mermaid
sequenceDiagram
    participant Sig as OS Signal
    participant Srv as HTTP Server
    participant R as Runner
    participant BG as Background Goroutines

    Sig->>Srv: SIGTERM / SIGINT (signal.NotifyContext)
    Srv->>Srv: ctx.Done() → srv.Shutdown(5s timeout)
    Note over Srv: SSE handlers exit via cancelled base context
    Srv->>R: r.Shutdown()
    R->>R: shutdownCancel() → cancel shutdownCtx
    R->>R: close(shutdownCh) → board subscription exits
    R->>R: boardSubscriptionWg.Wait()
    R->>BG: backgroundWg.Wait()
    Note over R: Logs pending goroutines every 3s while waiting
    R-->>Srv: Shutdown() returns
```

The shutdown sequence is driven by `signal.NotifyContext(ctx, SIGTERM, Interrupt)` in `RunServer` (`internal/cli/server.go`). When a signal arrives, `ctx.Done()` fires. The HTTP server gets `srv.Shutdown(5s)` to drain in-flight requests; SSE handlers exit immediately because their request contexts derive from the now-cancelled base context. Then `Runner.Shutdown()` (`internal/runner/runner.go`) is called: it invokes `shutdownCancel()` to cancel `shutdownCtx` (which propagates to any container launches or store operations using it), closes `shutdownCh` to stop the board-cache subscription goroutine, waits on `boardSubscriptionWg`, then waits on `backgroundWg` with a 3-second ticker that logs still-pending goroutine labels. In-progress task containers are intentionally left running; they continue independently and are recovered by `RecoverOrphanedTasks` (`internal/runner/recovery.go`) on the next startup.

## Where to Look

Quick-reference for common maintenance tasks. Each entry names the starting file and the typical next steps.

| If you need to... | Start here |
|---|---|
| Add a new API endpoint | `internal/apicontract/routes.go` → `internal/handler/<concern>.go` → run `make api-contract` |
| Add a field to Task | `internal/store/models.go` → `internal/store/migrate.go` |
| Change the turn loop | `internal/runner/execute.go` (`Run()`) |
| Change the commit pipeline | `internal/runner/commit.go` (`commit()`, `hostStageAndCommit()`, `rebaseAndMerge()`) + `internal/gitutil/ops.go` |
| Add a new automation watcher | `internal/handler/tasks_autopilot.go` (follow `SubscribeWake` pattern) |
| Change container arguments | `internal/runner/container.go` (`buildContainerArgsForSandbox()`) |
| Add a new env config variable | `internal/envconfig/envconfig.go` |
| Change workspace switching | `internal/workspace/manager.go` (`Switch()`) |
| Debug a failing rebase | `internal/gitutil/ops.go` + `internal/gitutil/stash.go` |
| Understand why a task failed | `data/<key>/<uuid>/traces/` + `outputs/turn-NNNN.json` |
| Add a new system prompt | `internal/prompts/` dir + `internal/prompts/prompts.go` |
| Change the UI | `ui/js/` (vanilla JS modules) + `ui/index.html` |
| Debug startup recovery | `internal/runner/recovery.go` (`RecoverOrphanedTasks()`) |
| Change pub/sub behaviour | `internal/store/subscribe.go` (`notify()`, `Subscribe()`, `SubscribeWake()`) |

## Package Map

Every `internal/` package and its role in the system:

| Package | Purpose | Key exported types / functions |
|---|---|---|
| `agents` | Merged built-in + user-authored agent registry backed by YAML under `~/.wallfacer/agents/`; fsnotify reload | `Registry`, `Agent`, `NewRegistry()`, `Load()` |
| `apicontract` | Single source of truth for all HTTP API routes; generates `ui/js/generated/routes.js` | `Route`, `Routes` (slice), `Route.FullPattern()` |
| `auth` | Cookie-based principal resolution and session validation for cloud mode | `CookiePrincipal`, `Middleware()` |
| `cli` | CLI subcommand implementations (run, exec, status, doctor, spec) and shared helpers | `RunServer()`, `RunExec()`, `RunStatus()`, `RunDoctor()`, `RunSpec()`, `BuildMux()`, `ConfigDir()` |
| `envconfig` | `.env` file parsing and atomic update | `Config`, `Parse()`, `Update()` |
| `eval` | Offline evaluation trajectories for planning/agent regression checks | `Trajectory`, `Run()` |
| `flow` | Merged built-in + user-authored flow registry; composes agents into ordered step chains | `Registry`, `Flow`, `Step`, `NewRegistry()` |
| `gitutil` | Git utility operations: worktrees, rebase, merge, status | `RebaseOntoDefault()`, `FFMerge()`, `CommitsBehind()`, `WorkspaceStatus()`, `WorkspaceGitStatus` |
| `handler` | HTTP API handlers organised by concern; automation watchers | `Handler`, `NewHandler()`, `CSRFMiddleware()`, `BearerAuthMiddleware()`, `MaxBytesMiddleware()` |
| `logger` | Structured logging via `log/slog` with per-component named loggers | `Init()`, `Fatal()`, `Main`, `Runner`, `Store`, `Git`, `Handler`, `Recovery`, `Prompts` |
| `metrics` | Lightweight Prometheus-compatible metrics registry (no external deps) | `Registry`, `Counter`, `Histogram`, `LabeledValue`, `NewRegistry()` |
| `runner` | Container orchestration, turn loop, commit pipeline, worktree management | `Runner`, `NewRunner()`, `RunnerConfig`, `ContainerInfo`, `CircuitBreaker`, `Interface` |
| `sandbox` | Sandbox type enumeration (Claude vs Codex) | `Type`, `Claude`, `Codex`, `All()`, `Parse()`, `Default()` |
| `store` | Per-task directory persistence, data models, event sourcing, pub/sub | `Store`, `Task`, `TaskEvent`, `TaskUsage`, `SandboxActivity`, `SequencedDelta` |
| `workspace` | Workspace lifecycle manager; scoped data directories; hot-swap support; persistent workspace group configurations | `Manager`, `Snapshot`, `Group`, `NewManager()`, `NewStatic()`, `LoadGroups()`, `SaveGroups()` |
| `constants` | Consolidated system parameters: timeouts, intervals, retry counts, size limits | Named constants grouped by concern |
| `oauth` | OAuth 2.0 PKCE flow engine, ephemeral callback server, provider configs (Claude, Codex) | `Flow`, `StartFlow()`, `Provider`, `Claude`, `Codex` |
| `planner` | Long-lived workspace-scoped planning sandbox lifecycle; per-thread `messages.jsonl` + `session.json` persistence under `~/.wallfacer/planning/<fp>/`; slash-command template expansion; single-turn-at-a-time coordination so only one thread runs at once | `Planner`, `ThreadManager`, `ConversationStore`, `CommandRegistry`, `ThreadMeta`, `Slugify`, `Expand` |
| `routine` | Routine scheduler engine that fires routine-kind tasks (ideation, user-defined) on their configured cadence | `Engine`, `Start()`, `Trigger()` |
| `spec` | Spec document model: YAML frontmatter parse/write round-trip; six-state lifecycle state machine; recursive tree builder with companion-directory convention; per-spec + cross-spec validation; atomic scaffold (`O_CREATE|O_EXCL`); progress aggregation; impact analysis; roadmap README index resolution | `Spec`, `Status`, `Effort`, `StatusMachine`, `Tree`, `BuildTree()`, `ParseFile()`, `Scaffold()`, `ValidateSpec()`, `UpdateFrontmatter()`, `ResolveIndex()` |

Top-level packages:

| Package | Purpose | Key exported types / functions |
|---|---|---|
| `prompts` | System prompt templates (title, commit, refinement, oversight, test, ideation, conflict, instructions) and workspace-level AGENTS.md management (`~/.wallfacer/instructions/`) | `Manager`, `NewManager()`, `InstructionsKey()`, `EnsureInstructions()`, `BuildInstructionsContent()`, `InstructionsData` |

Shared utility packages under `internal/pkg/`:

| Package | Purpose | Key exported types / functions |
|---|---|---|
| `pkg/atomicfile` | Atomic file writes (temp + rename) | `Write()` |
| `pkg/cache` | TTL cache with expiration | `TTLCache[K,V]` |
| `pkg/circuitbreaker` | Circuit breakers (lock-free and backoff variants) | `Breaker`, `BackoffBreaker` |
| `pkg/cmdexec` | `os/exec` wrapper for container commands | `Runner`, `Run()` |
| `pkg/dagscorer` | DAG-based task dependency scoring | `Score()` |
| `pkg/dircp` | Directory tree copy with filters | `Copy()` |
| `pkg/envutil` | Environment variable parsing with defaults and validation | `String()`, `Int()`, `Bool()`, `Duration()` |
| `pkg/httpjson` | JSON request/response helpers for HTTP handlers | `Decode()`, `Respond()`, `Error()` |
| `pkg/keyedmu` | Per-key mutex map for fine-grained locking | `Map[K]` |
| `pkg/lazyval` | Lazily-computed cached value with invalidation | `Value[T]`, `New()` |
| `pkg/logpipe` | Streaming log pipe for container output | `Pipe` |
| `pkg/ndjson` | Newline-delimited JSON reader | `Reader` |
| `pkg/pagination` | Cursor-based pagination helpers | `Paginate()` |
| `pkg/pty` | PTY relay for the WebSocket terminal integration | `PTY`, `Start()` |
| `pkg/pubsub` | Generic fan-out notification hub with replay | `Hub[T]` |
| `pkg/sanitize` | Input sanitization helpers | `String()` |
| `pkg/set` | Generic set type | `Set[T]` |
| `pkg/sortedkeys` | Sorted map key iteration | `Of()` |
| `pkg/syncmap` | Type-safe generic wrapper around `sync.Map` | `Map[K,V]` |
| `pkg/systray` | Optional system-tray integration for the desktop build | `Start()` |
| `pkg/tail` | Tail-follow for log files | `Follow()` |
| `pkg/trackedwg` | `sync.WaitGroup` with pending-task labels | `WaitGroup` |
| `pkg/uuidutil` | UUID parsing/generation helpers | `New()`, `Parse()` |
| `pkg/watcher` | Event-loop background watcher | `Start()` |
| `pkg/dag` | Generic DAG operations (ReverseEdges, DetectCycles, Reachable) | `ReverseEdges()`, `DetectCycles()`, `Reachable()` |
| `pkg/livelog` | Concurrency-safe append-only byte buffer with multiple readers for live streaming | `Buffer`, `NewBuffer()`, `Reader` |
| `pkg/statemachine` | Generic state machine with transition validation | `Machine[S]`, `New()`, `Transition()` |
| `pkg/tree` | Generic tree data structure with `iter.Seq` walk | `Node[T]`, `Walk()` |

## Handler Organisation

Each handler file in `internal/handler/` owns a specific concern area. The table below lists every non-test `.go` file:

| File | Concern | Key endpoints |
|---|---|---|
| `handler.go` | Core `Handler` struct, constructor, autopilot toggle state, JSON helpers, workspace snapshot subscription | — (shared infrastructure) |
| `middleware.go` | Request middleware: `CSRFMiddleware`, `BearerAuthMiddleware`, `MaxBytesMiddleware` | — (middleware, not endpoints) |
| `principal.go` | Request principal plumbing used by auth/cloud middleware | — (internal) |
| `agents.go` | User-authored agent catalog CRUD backed by `~/.wallfacer/agents/` | `GET/POST /api/agents`, `PUT/DELETE /api/agents/{slug}` |
| `flows.go` | User-authored flow catalog CRUD backed by `~/.wallfacer/flows/` | `GET/POST /api/flows`, `PUT/DELETE /api/flows/{slug}` |
| `routines.go` | Routine card CRUD (list, create, update schedule, trigger) | `GET/POST /api/routines`, `PATCH /api/routines/{id}/schedule`, `POST /api/routines/{id}/trigger` |
| `routines_engine.go` | Scheduler loop that fires routine tasks on their configured cadence | — (internal) |
| `orgs.go` | Organization listing and switching for cloud-mode principals | `GET /api/auth/me`, `GET /api/auth/orgs`, `POST /api/auth/switch-org` |
| `login.go` | Cloud sign-in flow handler | `POST /api/auth/login`, `POST /api/auth/logout` |
| `force_login.go` | Force-login gate used when a session must be re-authenticated | — (internal) |
| `commitsbehind_cache.go` | LRU cache for per-workspace commits-behind-default counts | — (internal) |
| `planning_system_prompt.go` | Per-turn selection of the planning-agent system prompt and archived-spec guard | — (internal) |
| `watcher_wake.go` | Wake-up helpers that nudge background watchers when state changes | — (internal) |
| `event_actor.go` | Resolves the actor identity recorded on task events | — (internal) |
| `tasks.go` | Task CRUD, batch create, status transitions (cancel, resume, restore, archive, sync, test, done, feedback) | `POST /api/tasks`, `PATCH /api/tasks/{id}`, `POST /api/tasks/{id}/cancel`, etc. |
| `tasks_events.go` | Task event timeline, per-turn output serving, turn usage | `GET /api/tasks/{id}/events`, `GET /api/tasks/{id}/outputs/{filename}`, `GET /api/tasks/{id}/turn-usage` |
| `tasks_autopilot.go` | Automation watchers: auto-promoter, auto-retrier, auto-tester, auto-submitter, auto-refiner, waiting-sync | `StartAutoPromoter()`, `StartAutoRetrier()`, etc. |
| `stream.go` | SSE streaming for live task updates and container logs | `GET /api/tasks/stream`, `GET /api/tasks/{id}/logs` |
| `config.go` | Server configuration (autopilot flags, sandbox list, watcher health) | `GET /api/config`, `PUT /api/config` |
| `env.go` | Environment configuration (API tokens, model settings, sandbox routing) | `GET /api/env`, `PUT /api/env`, `POST /api/env/test` |
| `workspace.go` | Workspace browsing and switching | `GET /api/workspaces/browse`, `PUT /api/workspaces` |
| `instructions.go` | Workspace AGENTS.md read/write/reinit | `GET /api/instructions`, `PUT /api/instructions`, `POST /api/instructions/reinit` |
| `prompts.go` | System prompt template listing, override, and deletion | `GET /api/system-prompts`, `PUT /api/system-prompts/{name}`, `DELETE /api/system-prompts/{name}` |
| `templates.go` | Reusable prompt templates | `GET /api/templates`, `POST /api/templates`, `DELETE /api/templates/{id}` |
| `git.go` | Git workspace operations (status, push, sync, rebase, branches, checkout) | `GET /api/git/status`, `POST /api/git/push`, `POST /api/git/sync`, etc. |
| `execute.go` | Task execution trigger (delegates to runner) | — (internal, called by task status transitions) |
| `refine.go` | Prompt refinement agent lifecycle | `POST /api/tasks/{id}/refine`, `DELETE /api/tasks/{id}/refine`, `POST /api/tasks/{id}/refine/apply` |
| `ideate.go` | Brainstorm/ideation agent lifecycle | `GET /api/ideate`, `POST /api/ideate`, `DELETE /api/ideate` |
| `oversight.go` | Task oversight summary retrieval | `GET /api/tasks/{id}/oversight`, `GET /api/tasks/{id}/oversight/test` |
| `usage.go` | Aggregated token and cost usage statistics | `GET /api/usage` |
| `stats.go` | Task status and workspace cost statistics | `GET /api/stats` |
| `spans.go` | Span timing statistics (per-task and aggregate) | `GET /api/debug/spans`, `GET /api/tasks/{id}/spans` |
| `containers.go` | Running container listing | `GET /api/containers` |
| `files.go` | File listing for `@` mention autocomplete | `GET /api/files` |
| `admin.go` | Administrative operations | `POST /api/admin/rebuild-index` |
| `debug.go` | Health check and board manifest | `GET /api/debug/health`, `GET /api/debug/board`, `GET /api/tasks/{id}/board` |
| `runtime.go` | Live server internals (goroutines, memory, task states, containers) | `GET /api/debug/runtime` |
| `sandbox_gate.go` | Sandbox usability checks (auth validation before task launch) | — (internal helpers) |
| `watcher.go` | Shared two-phase watcher helper used by the autopilot loops in `tasks_autopilot.go` | `TwoPhaseWatcherConfig`, `runTwoPhase()` |
| `diffcache.go` | LRU diff cache for task diffs | — (internal) |
| `file_index.go` | Background file indexing for `@` mention | — (internal) |
| `event_helpers.go` | Shared helpers for inserting task events | — (internal) |
| `auth.go` | OAuth authentication flow (start, poll status, cancel) | `POST /api/auth/{provider}/start`, `GET /api/auth/{provider}/status`, `POST /api/auth/{provider}/cancel` |
| `explorer.go` | File explorer: directory listing, file read/write, binary detection | `GET /api/explorer/tree`, `GET /api/explorer/file`, `PUT /api/explorer/file`, `GET /api/explorer/stream` |
| `images.go` | Sandbox image management: status check, pull, delete | `GET /api/images`, `POST /api/images/pull`, `DELETE /api/images`, `GET /api/images/pull/stream` |
| `planning.go` | Planning chat agent: messages, streaming, interrupt, commands | `GET/POST/DELETE /api/planning/messages`, `GET /api/planning/messages/stream`, `POST /api/planning/messages/interrupt`, `GET /api/planning/commands` |
| `planning_directive.go` | `/spec-new` directive scanner: line-oriented, fence-aware parser that extracts scaffold requests from assistant output and calls `spec.Scaffold` | — (internal; called from planning chat commit path) |
| `planning_git.go` | Staging/committing planning rounds on the workspace branch with `Plan-Thread` and `Plan-Round` trailers so undo can target the caller's thread | — (internal) |
| `planning_system_prompt.go` | Per-turn selection of the planning-agent system prompt (empty vs non-empty workspace variants) and archived-spec guard | — (internal) |
| `planning_threads.go` | Planning chat thread CRUD (list, create, rename, archive, unarchive, activate) | `GET/POST /api/planning/threads`, `PATCH /api/planning/threads/{id}`, `POST /api/planning/threads/{id}/archive|unarchive|activate` |
| `planning_undo.go` | Undo the caller thread's most recent planning round via `git revert`; cancels board tasks whose `dispatched_task_id` was added in the reverted commit | `POST /api/planning/undo` |
| `specs.go` | Spec tree with metadata, progress, and archive/unarchive transitions | `GET /api/specs/tree`, `GET /api/specs/stream`, `POST /api/specs/archive`, `POST /api/specs/unarchive` |
| `specs_dispatch.go` | Atomic dispatch/undispatch pipeline that creates board tasks from validated leaf specs and writes `dispatched_task_id` back into the spec frontmatter (rollback on partial failure) | `POST /api/specs/dispatch`, `POST /api/specs/undispatch` |
| `terminal.go` | WebSocket terminal relay for host shell and container exec | `GET /api/terminal/ws` |

## Structured Logging

The `internal/logger` package provides named loggers built on `log/slog`:

| Logger | Component tag | Used by |
|---|---|---|
| `logger.Main` | `main` | CLI startup, server lifecycle, shutdown |
| `logger.Runner` | `runner` | Container orchestration, turn loop, commit pipeline |
| `logger.Store` | `store` | Task persistence, state transitions |
| `logger.Git` | `git` | Worktree and git operations |
| `logger.Handler` | `handler` | HTTP request handling, automation watchers |
| `logger.Recovery` | `recovery` | Orphaned task recovery on startup |
| `logger.Prompts` | `prompts` | System prompt template management |

`logger.Init(format)` configures all loggers. Two formats are supported:
- **`"text"`** (default) — Human-friendly output with ANSI colors (when stdout is a terminal), aligned columns: timestamp, 3-char level badge, 8-char component, source file:line, bold message, dim key=value pairs. Respects `NO_COLOR` and `TERM=dumb`.
- **`"json"`** — Structured JSON via `slog.NewJSONHandler`, suitable for log aggregation.

`logger.Fatal(msg, args...)` prints a user-friendly error to stderr and exits with code 1 (used for startup errors, not for runtime failures).

## Cross-Cutting Concerns

**Concurrency** — `Store.mu` for task map integrity; `Runner.worktreeMu` for filesystem ops; per-repo mutex for rebase serialization; per-task mutex for oversight generation. See [Data & Storage](data-and-storage.md) for the concurrency model.

**Recovery** — On startup, `RecoverOrphanedTasks` inspects `in_progress` and `committing` tasks against actual container and worktree state, recovering or failing them as appropriate.

**Security** — API key auth, SSRF-hardened gateway URLs, path traversal guards, CSRF protection, request body size limits.

**Circuit breakers** — Per-watcher exponential backoff suppresses individual automation loops on failure; container-level circuit breaker blocks launches when the runtime is unavailable. See [Circuit Breakers](../guide/circuit-breakers.md).

**Observability** — SSE event streams, append-only trace timeline per task, span timing, Prometheus-compatible metrics. See [API & Transport](api-and-transport.md) for the metrics reference.

**Middleware** — See [API & Transport](api-and-transport.md) for the middleware chain.

**Sandbox routing** — See [Workspaces & Configuration](workspaces-and-config.md) for sandbox routing.

**Graceful shutdown** — See [API & Transport](api-and-transport.md) for the shutdown sequence.

## See Also

- [Development Setup](development.md) — building from source, tests, make targets, releases
- [Data & Storage](data-and-storage.md) — persistence, models, migrations, search index
- [Task Lifecycle](task-lifecycle.md) — states, turn loop, dependencies, board context
- [Git Operations](git-worktrees.md) — worktrees, commit pipeline, branch management
- [Workspaces & Configuration](workspaces-and-config.md) — workspace manager, AGENTS.md, sandboxes, templates
- [API & Transport](api-and-transport.md) — HTTP routes, SSE, metrics, middleware
- [Automation](automation.md) — watchers, auto-retry, circuit breakers
