# Sandbox Reuse

**Status:** Not started
**Depends on:** [M1: Pluggable Sandbox Backends](01-sandbox-backends.md) — complete

## Problem

Wallfacer creates and destroys an ephemeral sandbox for every agent invocation —
implementation turns, title generation, oversight summaries, commit messages, refinement,
and ideation. Every invocation pays startup overhead that varies by backend:

| Backend | Startup cost | What's wasted |
|---------|-------------|---------------|
| Local (podman/docker) | 0.5–2 s per container (namespace, cgroup, overlay) | Warm caches, compiled artifacts |
| K8s | 5–30 s per pod (scheduling, image pull, volume attach) | Everything — pods are fully ephemeral |
| VZ (macOS VM) | 3–10 s per VM (kernel boot, rootfs mount) | VM memory, warm runtimes |
| Sandbox-init (macOS) | ~100 ms per process fork | Dependency caches |

For a single task lifecycle that generates a title, runs 3 implementation turns, produces
an oversight summary, and generates a commit message, that is 6 sandbox create/destroy
cycles. With 5 concurrent tasks, the system performs ~30 lifecycle operations per batch.

The core idea is **keep a sandbox host alive and exec into it** rather than
create/destroy for each invocation. This pattern applies to every backend — each
implements it differently, but the strategy is the same.

## Scope

Sandbox reuse is an optimization **internal to each `Backend`** — the runner and handler
never see it. The `Backend` interface (`internal/sandbox/backend.go`) remains unchanged:

```
Runner → backend.Launch(spec) → Handle
                                  .State()   → Creating/Running/Streaming/Stopped/Failed
                                  .Stdout()  → io.ReadCloser
                                  .Stderr()  → io.ReadCloser
                                  .Wait()    → exitCode
                                  .Kill()
```

Each backend decides internally whether to create a fresh sandbox or exec into an
existing one. The returned `Handle` behaves identically either way. The runner's turn
loop, output parsing, circuit breaker, and lifecycle tracking are unaffected.

How reuse manifests per backend:

| Backend | Reuse mechanism |
|---------|----------------|
| Local (podman/docker) | Long-lived container + `podman exec` |
| K8s | Long-lived pod + `kubectl exec` |
| VZ (macOS VM) | Long-lived VM + exec via vsock/SSH |
| Sandbox-init (macOS) | Process pool or warm sandbox profiles |

This spec details the `LocalBackend` implementation first, as it is the only backend
currently implemented. The mount profile analysis and worker lifecycle pattern
apply to all backends.

## Agent Roles and Mount Profiles

### Roles

| Role | Mount Profile | Typical Duration | Frequency |
|------|--------------|------------------|-----------|
| Implementation | Full RW (worktrees, .git, board, instructions) | 1–60 min | Every turn |
| Testing | Full RW (same as implementation) | 1–60 min | On demand |
| Refinement | Workspace RO + instructions | 1–30 min | On demand |
| Ideation | Workspace RO + instructions | 1–30 min | On demand |
| Title generation | Minimal (claude-config only) | 5–10 s | Every task start |
| Oversight | Minimal (claude-config only) | 15–30 s | Task completion + periodic |
| Commit message | Minimal (claude-config only) | 10–20 s | Commit pipeline |

### Three Mount Profiles

**Profile A (Full RW)** — Implementation + Testing:
- Worktrees at `/workspace/<repo>` (read-write)
- Main repo `.git` directories (read-write, for git operations)
- Board context at `/workspace/.tasks/board.json` (read-only)
- Sibling worktrees at `/workspace/.tasks/worktrees/` (read-only)
- Instructions at `/workspace/AGENTS.md` (read-only)
- `claude-config` named volume

**Profile B (Workspace RO)** — Refinement + Ideation:
- Workspaces at `/workspace/<repo>` (read-only)
- Instructions at `/workspace/AGENTS.md` (read-only)
- `claude-config` named volume

**Profile C (Minimal)** — Title, Oversight, Commit message:
- `claude-config` named volume only
- No workspace or worktree mounts
- Prompt contains all context (diff stats, activity logs, task prompt)

### Current Lifecycle

Every container invocation follows (inside `LocalBackend.Launch()`):
1. `podman rm -f <name>` — clean up any leftover container
2. `podman run --rm --name <name> ... <image> <cmd>` — ephemeral launch
3. Container runs Claude CLI, produces NDJSON on stdout, exits
4. Container auto-removed by `--rm`

Session state survives via the `claude-config` named volume and `--resume <sessionID>`.
Worktree changes persist on the host via bind mounts.

Key files: `internal/sandbox/spec.go` (`ContainerSpec`, `Build()`), `internal/sandbox/local.go`
(`LocalBackend`, `localHandle`), `internal/runner/container.go` (role-specific spec builders).

---

## Strategy Analysis

### Strategy A: Container Pool Per Role

Pre-create a pool of stopped containers per mount profile. When a container is needed,
pick one from the pool and `start` it.

**Problem:** Bind mounts are fixed at `create` time. Profile A containers require
task-specific worktree paths, so they cannot be pooled — each task has unique mounts at
`~/.wallfacer/worktrees/<taskID>/<repo>`. Profile C containers are identical (only
`claude-config`), so they can be pooled. Profile B containers are identical per workspace
set but change when workspaces are switched.

**Verdict:** Only viable for Profile C. Limited benefit since Profile C containers are
already lightweight (no workspace mounts). The pooling infrastructure adds complexity for
marginal gain.

### Strategy B: Long-Lived Worker Containers

Start a persistent container with `sleep infinity` as its process, then use `podman exec`
to run commands inside it.

**How it works:** A worker container is created once with the appropriate mounts and kept
alive. Each agent invocation becomes a `podman exec <worker> /usr/local/bin/entrypoint.sh
-p "..." --output-format stream-json` call. Multiple `exec` commands can run sequentially
(or concurrently) in the same container.

**Worktree challenge:** Profile A workers would need task-specific worktree mounts. Since
mounts are immutable after creation, each implementation task still needs its own worker
container. However, the worker survives across turns within the same task, eliminating
per-turn container churn for auto-continue loops.

**Profile C benefit:** A single long-lived worker per sandbox type (claude, codex) serves
all title/oversight/commit invocations. `podman exec` overhead is ~50–100 ms vs ~0.5–2 s
for `podman run`.

**Concerns:**
- Entrypoint logic (`entrypoint.sh`) must be invoked explicitly in `exec` commands
- Accumulated filesystem state (temp files, caches) may affect subsequent runs
- Requires detecting and recovering from dead worker containers

**Verdict:** Strong approach for Profile C. Interesting for Profile A per-task workers
(eliminates per-turn churn). Higher implementation complexity.

### Strategy C: Container Pause/Unpause

Use `podman pause` between turns to freeze the container process via cgroups, then
`podman unpause` to resume.

**Fatal flaw:** Claude CLI is a one-shot process (`claude -p "..." [--resume <sid>]`).
It exits when done. Once the process exits, the container exits. There is no running
process to pause. This strategy requires a persistent daemon inside the container that
accepts prompts via stdin/socket — which Claude CLI does not support.

**Verdict:** Fundamentally incompatible with the current architecture.

### Strategy D: Per-Task Worker (Recommended)

Each task gets a single long-lived container that serves **all** its agent
invocations — implementation turns, title generation, oversight summaries,
and commit messages. The container is created at the task's first turn and
destroyed when the task completes or is cancelled.

| Profile | Strategy | Rationale |
|---------|----------|-----------|
| A (Implementation) | Per-task worker | Eliminates per-turn container churn; worktree mounts fixed per task |
| B (Refinement/Ideation) | Ephemeral (no change) | Infrequent; each run is independent; different workspace mount mode |
| C (Title/Oversight/Commit) | **Per-task worker** (same container as A) | Clean context isolation between tasks; no cross-task session leakage |

**Why not a shared aux worker for Profile C?** A shared container serving
title/oversight/commit for multiple tasks risks session state leakage
(Claude CLI writes to `~/.claude/` inside the container), accumulated temp
files, and stale caches affecting unrelated tasks. Per-task workers provide
complete isolation with the same startup savings (the container already exists
for implementation turns).

**Tradeoff:** Tasks that only run auxiliary agents (e.g. a title-only
re-generation on a completed task) still create an ephemeral container. This
is acceptable — these are rare, and the ephemeral fallback is always available.

---

## Filesystem Layer

Sandbox reuse (long-lived workers) addresses **runtime overhead** but not **filesystem
overhead**. These are two orthogonal concerns within each backend:

```
┌─────────────────────────────────────────────┐
│ Backend (any implementation)                │
│                                             │
│  ┌─────────────────┐  ┌──────────────────┐  │
│  │ Runtime Layer    │  │ Filesystem Layer │  │
│  │                  │  │                  │  │
│  │ • Ephemeral      │  │ • Overlay FS     │  │
│  │ • Long-lived     │  │ • Snapshots      │  │
│  │   worker + exec  │  │ • Warm caches    │  │
│  │ • Lifecycle      │  │ • Persistent     │  │
│  │   state tracking │  │   layers         │  │
│  └─────────────────┘  └──────────────────┘  │
│                                             │
│  Both hidden behind Backend.Launch()        │
└─────────────────────────────────────────────┘
```

### Filesystem Reuse Options

| Approach | Mechanism | Benefit | Complexity |
|----------|-----------|---------|------------|
| **Named volume caches** | Persist `~/.npm`, `~/.cache/pip`, etc. in named volumes mounted across invocations | Warm dependency caches; significant for tasks that install packages | Low — just add volume mounts to ContainerSpec |
| **Overlay snapshots** | Use `podman commit` to snapshot a warm container into a derived image, launch subsequent invocations from that image | Full filesystem reuse including compiled artifacts | Medium — need snapshot lifecycle management |
| **Filesystem checkpoints** | Use CRIU checkpoint/restore (Podman supports `podman container checkpoint/restore`) | Instant resume of entire container state including memory | High — CRIU support varies; security concerns with memory restore |
| **Bind-mount workspace caches** | Mount a host-side cache directory (e.g. `~/.wallfacer/cache/<workspace-hash>/`) into the container | Workspace-specific caches persist across tasks | Low — similar to current worktree bind mounts |

### Interaction with Container Reuse

Long-lived workers inherently provide filesystem reuse within their lifetime:
- **Profile C aux workers:** Warm claude-config, pre-loaded Python/Node runtimes persist
  across all title/oversight/commit invocations for the worker's lifetime
- **Profile A per-task workers:** Dependency caches built during turn 1 are available in
  subsequent turns without reinstallation

Named volume caches are complementary — they persist filesystem state *across* worker
lifetimes (e.g., when aux workers are recreated after server restart).

### Recommendation

Start with container reuse (this spec) for the biggest wins. Add named volume caches
for dependency directories as a low-cost follow-up. Defer overlay snapshots and CRIU
to later — they add significant complexity for diminishing returns.

---

## LocalBackend Implementation: Hybrid Workers

### Architecture

The first implementation targets `LocalBackend` (`internal/sandbox/local.go`). The
backend tracks per-task workers alongside ephemeral containers:

```go
// internal/sandbox/local.go (extend existing LocalBackend)

type LocalBackend struct {
    command string // "podman" or "docker" (existing field)

    // Per-task worker containers (reuse optimization)
    taskWorkers   map[uuid.UUID]*taskWorker
    taskWorkersMu sync.Mutex

    enableTaskWorkers bool // WALLFACER_TASK_WORKERS (default true)
}
```

When `Launch()` is called, the backend checks whether a worker exists for the
task. The task ID is available via the container spec's labels
(`wallfacer.task-id`):

```go
func (b *LocalBackend) Launch(ctx context.Context, spec sandbox.ContainerSpec) (sandbox.Handle, error) {
    taskID := spec.Labels["wallfacer.task-id"]

    if taskID != "" && b.enableTaskWorkers {
        return b.launchViaTaskWorker(ctx, spec, taskID)
    }
    return b.launchEphemeral(ctx, spec) // current behavior
}
```

The returned `Handle` is identical regardless of execution strategy — the runner
never knows whether the container was ephemeral or a worker exec.

### Per-Task Worker

Each task gets a single long-lived container at its first invocation. All
subsequent invocations for the same task (implementation turns, title, oversight,
commit message) reuse the same container via `podman exec`.

```
Task starts (first invocation — implementation, title, etc.):
  → podman create --name wallfacer-task-<uuid8> \
      --entrypoint '["sleep", "infinity"]' \
      -v claude-config:/home/claude/.claude \
      --mount type=bind,src=<worktree>,dst=/workspace/<repo> \
      --mount type=bind,src=<repo>/.git,dst=<repo>/.git \
      ... (board context, instructions, sibling mounts)
      wallfacer-claude:latest
  → podman start wallfacer-task-<uuid8>

Each subsequent invocation (via Launch → launchViaTaskWorker):
  → (refresh board.json on host — visible via bind mount)
  → podman exec wallfacer-task-<uuid8> \
      /usr/local/bin/entrypoint.sh \
      -p "<prompt>" [--resume <sessionID>] --verbose --output-format stream-json

Task completes / cancelled:
  → podman rm -f wallfacer-task-<uuid8>
```

**Context isolation:** Since the container is scoped to a single task, there is
no cross-task session leakage, no stale caches from unrelated tasks, and no
concurrency contention on `claude-config`. Each task has a clean environment.

**Profile C invocations (title/oversight/commit):** These run in the same
per-task container. They don't need workspace mounts themselves, but having them
is harmless — the container already has them for implementation turns. The
benefit is avoiding a separate container lifecycle for these short-lived agents.

**Fallback:** Invocations without a task ID (e.g. ideation, refinement) use
ephemeral containers as before.

**Handle state mapping:** The `Handle` returned by `launchViaTaskWorker` wraps
the `podman exec` process. State transitions work the same as ephemeral handles:
`Creating` (exec starting) → `Running` → `Streaming` → `Stopped`/`Failed`.

**Board context refresh:** Board context is mounted as a bind mount from a host
directory. Before each turn, the host writes an updated `board.json` to this
directory. The container sees the update immediately — no restart needed.

**Sibling worktree mounts:** Sibling task worktrees are mounted read-only at
container creation time. New sibling tasks started after the container was
created will not be visible. This is acceptable — `board.json` still lists them,
and the agent can reference their prompts/status even without filesystem access.

### Worktree Impact

**No change to worktree lifecycle.** Worktrees are still created by `ensureTaskWorktrees()`
before the first turn and cleaned up by `CleanupWorktrees()` after the commit pipeline.
The only difference is that the container holding the bind mount lives across turns
instead of being recreated.

**Sync operation:** When a waiting/failed task is synced (rebase onto latest default
branch), the rebase happens on the host filesystem. The worktree path does not change,
so the bind mount remains valid. If the implementation worker is alive during sync, it
should be stopped first to avoid concurrent git operations:

1. `podman rm -f wallfacer-impl-<uuid8>`
2. Perform rebase on host
3. Recreate worker with same mounts on next turn

**Worktree health watcher:** The existing health check (`StartWorktreeHealthWatcher`)
continues to monitor worktree integrity. If a worktree is restored, the implementation
worker's bind mount still points to the same path, so no worker restart is needed.

---

## Implementation Design (LocalBackend)

### New Types

```go
// internal/sandbox/worker.go

// taskWorker manages a long-lived per-task container that serves all agent
// invocations for that task (implementation turns, title, oversight, commit
// message) via podman exec.
type taskWorker struct {
    mu            sync.Mutex
    command       string          // container runtime binary
    containerName string
    spec          ContainerSpec   // create-mode spec (no --rm, sleep entrypoint)
    alive         bool
    taskID        uuid.UUID
}

func (w *taskWorker) ensureRunning(ctx context.Context) error
func (w *taskWorker) exec(ctx context.Context, cmd []string) (Handle, error)
func (w *taskWorker) stop()
```

Workers call `podman`/`docker` directly via `cmdexec` — they are implementation
details of `LocalBackend`, not `Backend` interface methods.

### Health Checks and Recovery

`taskWorker.ensureRunning()`:
1. If `alive`, check `podman inspect --format '{{.State.Running}}' <name>` — if true, return nil
2. `podman rm -f <name>` — clean up dead container
3. `podman create <name> ...` + `podman start <name>`
4. If any step fails, return error; `LocalBackend.Launch()` falls back to ephemeral

Graceful degradation ensures the system never breaks — it just loses the performance
optimization and falls back to ephemeral `podman run --rm`.

### ContainerSpec Changes

No new methods needed on `ContainerSpec`. The task ID is already present as a
label (`wallfacer.task-id`) on every task-scoped container spec. `Launch()`
reads this label to decide whether to route through a per-task worker. Specs
without a task ID label (ideation, refinement) use ephemeral containers.

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_TASK_WORKERS` | `true` | Enable per-task worker containers |

When disabled, `LocalBackend.Launch()` always uses ephemeral containers. Future backends
will have their own reuse configuration as appropriate to their runtime model.

---

## Implementation Plan

M1 (Pluggable Sandbox Backends) is complete. `LocalBackend` is in `internal/sandbox/local.go`.
M2.5 (Multi-Workspace Groups) is complete — `activeGroups` and per-task store resolution are in place, so workers can be scoped per workspace group if needed.

### Phase 1: Per-Task Worker Foundation

1. Add `taskWorker` type in `internal/sandbox/worker.go`
2. Implement `ensureRunning`, `exec` (returns `Handle`), `stop`, health check
3. Wire `LocalBackend.Launch()` in `internal/sandbox/local.go` to route task-scoped specs through per-task workers
4. Add integration test: launch worker, exec implementation turn, verify output matches ephemeral
5. Feature-flagged behind `WALLFACER_TASK_WORKERS`

### Phase 2: Worker Lifecycle Management

1. Handle worker cleanup on task completion/cancellation/failure (registered via runner cleanup hooks)
2. Handle worker recreation on sync operations (kill before rebase, recreate on next turn)
3. Verify title, oversight, and commit message agents work through the per-task worker
4. Test concurrent tasks with separate workers under load

### Phase 3: Robustness

1. Add health check recovery (detect dead workers, recreate transparently)
2. Add lifecycle timing metrics (worker create, exec, health check)
3. Graceful degradation: if worker creation fails, fall back to ephemeral

### Phase 4: Filesystem Reuse (Optional Follow-Up)

1. Add named volume mounts for dependency caches (`~/.npm`, `~/.cache/pip`, etc.)
2. Mount a per-workspace cache directory for workspace-specific build artifacts
3. Measure cold vs warm task execution times

### Phase 5: Measurement

1. Instrument container lifecycle timing (create, start, exec, remove) via span events
2. Compare wall-clock time for task batches with and without workers
3. Monitor container runtime storage driver health under sustained load

---

## Performance Expectations

| Scenario | Current (ephemeral) | With per-task workers |
|----------|-------------------|--------------|
| 3-turn implementation task + title + oversight + commit | 6 × (0.5–2 s) startup | 1 × (0.5–2 s) create + 6 × ~100 ms exec |
| 5 concurrent tasks, each 6 invocations | ~30 container cycles | 5 creates + 30 exec calls |

Per task, container overhead drops from ~3–12 s (6 ephemeral launches) to
~0.5–2 s (1 create) + ~0.6 s (6 execs). The biggest win is eliminating
per-turn startup for auto-continue loops (3+ turns).

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Worker container dies mid-task | Agent invocation fails | `ensureRunning` detects and recreates; fallback to ephemeral |
| Filesystem state accumulation | Stale caches, temp files affect behavior | Workers are per-task and short-lived; no cross-task contamination |
| `podman exec` not available | Some container runtimes may not support exec | Feature flag disables workers; fallback to ephemeral |
| Stale sibling mounts | New sibling tasks not visible in container | Board.json still lists them; accept limitation or recreate worker |
| Backend interface change | If `Backend` interface changes, workers need updating | Workers are internal to each backend; no interface coupling |
