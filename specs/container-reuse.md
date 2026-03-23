# Container Reuse

**Date:** 2026-03-21
**Updated:** 2026-03-23
**Depends on:** [Pluggable Sandbox Backends](cloud-sandbox-executor.md) (Phase 1: Interface Extraction + Local Backend)

## Problem

Wallfacer creates and destroys an ephemeral container for every agent invocation —
implementation turns, title generation, oversight summaries, commit messages, refinement,
and ideation. Every invocation pays two kinds of overhead:

1. **Container runtime overhead** — process namespace creation, cgroup setup, image layer
   mount, volume attach: roughly 0.5–2 s per launch on Podman with cached images.

2. **Filesystem overhead** — each `podman run` creates a fresh read-write overlay layer.
   Any warm caches (npm, pip, compilation artifacts) from previous runs are lost. The
   storage driver performs repeated layer mount/unmount operations under load.

For a single task lifecycle that generates a title, runs 3 implementation turns, produces
an oversight summary, and generates a commit message, that is 6 container create/destroy
cycles. With 5 concurrent tasks, the host performs ~30 container lifecycle operations per
batch.

These two overhead sources are orthogonal and can be addressed independently:
- **Container reuse** (this spec) eliminates runtime lifecycle churn via long-lived workers
- **Filesystem reuse** (snapshots, warm overlays, persistent layers) eliminates I/O
  initialization overhead — discussed in the Filesystem Layer section below

## Prerequisite: Pluggable Sandbox Backends

This spec builds on the `SandboxBackend` / `SandboxHandle` abstraction defined in
[cloud-sandbox-executor.md](cloud-sandbox-executor.md). Container reuse is an optimization
**internal to `LocalBackend`** — the runner and handler never see it. The `SandboxBackend`
interface remains unchanged:

```
Runner → backend.Launch(spec) → SandboxHandle
                                  .State()   → SandboxCreating/Running/Streaming/Stopped/Failed
                                  .Stdout()  → io.ReadCloser
                                  .Wait()    → exitCode
                                  .Kill()
```

Container reuse changes how `LocalBackend.Launch()` provisions the underlying container
(ephemeral `podman run` vs `podman exec` on a long-lived worker), but the returned
`SandboxHandle` behaves identically. The runner's turn loop, output parsing, circuit
breaker, and lifecycle tracking are unaffected.

For K8s and remote backends, container reuse is not applicable — pod scheduling and
remote Docker have their own lifecycle models. This spec is scoped to `LocalBackend`.

## Current Architecture

### Container Roles

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

Every container invocation follows (inside `osContainerExecutor.RunArgs()`):
1. `podman rm -f <name>` — clean up any leftover container
2. `podman run --rm --name <name> ... <image> <cmd>` — ephemeral launch
3. Container runs Claude CLI, produces NDJSON on stdout, exits
4. Container auto-removed by `--rm`

Session state survives via the `claude-config` named volume and `--resume <sessionID>`.
Worktree changes persist on the host via bind mounts.

Key files: `internal/runner/container_spec.go` (spec builder), `internal/runner/executor.go`
(runtime abstraction), `internal/runner/container.go` (role-specific arg builders).

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

### Strategy D: Hybrid (Recommended)

Combine strategies based on mount profile:

| Profile | Strategy | Rationale |
|---------|----------|-----------|
| A (Implementation) | Long-lived per-task worker | Eliminates per-turn container churn; worktree mounts fixed per task |
| B (Refinement/Ideation) | Ephemeral (no change) | Infrequent; each run is independent |
| C (Title/Oversight/Commit) | Shared long-lived worker | Highest frequency, identical mounts, biggest startup savings |

---

## Filesystem Layer

Container reuse (long-lived workers) addresses **runtime overhead** but not **filesystem
overhead**. These are two orthogonal concerns within `LocalBackend`:

```
┌─────────────────────────────────────────────┐
│ LocalBackend                                │
│                                             │
│  ┌─────────────────┐  ┌──────────────────┐  │
│  │ Runtime Layer    │  │ Filesystem Layer │  │
│  │                  │  │                  │  │
│  │ • Ephemeral run  │  │ • Overlay FS     │  │
│  │ • Long-lived     │  │ • Snapshots      │  │
│  │   worker + exec  │  │ • Warm caches    │  │
│  │ • Lifecycle      │  │ • Persistent     │  │
│  │   state tracking │  │   layers         │  │
│  └─────────────────┘  └──────────────────┘  │
│                                             │
│  Both hidden behind SandboxBackend.Launch() │
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

## Recommended Approach: Hybrid Workers Inside LocalBackend

### Architecture

Container reuse lives entirely inside `LocalBackend`. The backend tracks two kinds of
workers alongside ephemeral containers:

```go
// internal/runner/backend_local.go

type LocalBackend struct {
    command string // "podman" or "docker"

    // Worker management (container reuse optimization)
    auxWorkers   map[constants.SandboxType]*auxWorker // shared Profile C workers
    auxWorkersMu sync.Mutex

    implWorkers   map[uuid.UUID]*implWorker // per-task Profile A workers
    implWorkersMu sync.Mutex

    enableAuxWorkers  bool // WALLFACER_AUX_WORKERS (default true)
    enableImplWorkers bool // WALLFACER_IMPL_WORKERS (default true)
}
```

When `Launch()` is called, the backend decides the execution strategy based on the
container spec's mount profile and configuration:

```go
func (b *LocalBackend) Launch(ctx context.Context, spec ContainerSpec) (SandboxHandle, error) {
    profile := spec.MountProfile() // A, B, or C based on spec shape

    switch {
    case profile == ProfileC && b.enableAuxWorkers:
        return b.launchViaAuxWorker(ctx, spec)
    case profile == ProfileA && b.enableImplWorkers:
        return b.launchViaImplWorker(ctx, spec)
    default:
        return b.launchEphemeral(ctx, spec) // current behavior
    }
}
```

The returned `SandboxHandle` is identical regardless of execution strategy — the runner
never knows whether the container was ephemeral or a worker exec.

### Profile C: Shared Auxiliary Worker

A single long-lived container per sandbox type serves all title, oversight, and commit
message invocations.

```
Server Start
  → podman create --name wallfacer-aux-claude \
      --entrypoint '["sleep", "infinity"]' \
      -v claude-config:/home/claude/.claude \
      --env-file ~/.wallfacer/.env \
      --network host \
      wallfacer-claude:latest
  → podman start wallfacer-aux-claude

Title/Oversight/Commit invocation (via Launch → launchViaAuxWorker):
  → podman exec wallfacer-aux-claude \
      /usr/local/bin/entrypoint.sh \
      -p "<prompt>" --verbose --output-format stream-json

Server Shutdown:
  → podman rm -f wallfacer-aux-claude
```

**Concurrency:** Multiple `podman exec` processes can run concurrently in the same
container. Claude CLI's session state in `claude-config` uses file-level locking. If
contention is observed, serialize access with a `sync.Mutex` in the `auxWorker` — since
auxiliary agents are fast (5–30 s), FIFO queuing is acceptable.

**Handle state mapping:** The `SandboxHandle` returned by `launchViaAuxWorker` wraps the
`podman exec` process. State transitions work the same as ephemeral handles:
`Creating` (exec starting) → `Running` → `Streaming` → `Stopped`/`Failed`.

### Profile A: Per-Task Implementation Worker

Instead of creating a new container each turn, create a long-lived worker per task at
first turn and reuse it across the turn loop.

```
Task starts (first turn):
  → podman create --name wallfacer-impl-<uuid8> \
      --entrypoint '["sleep", "infinity"]' \
      -v claude-config:/home/claude/.claude \
      --mount type=bind,src=<worktree>,dst=/workspace/<repo> \
      --mount type=bind,src=<repo>/.git,dst=<repo>/.git \
      ... (board context, instructions, sibling mounts)
      wallfacer-claude:latest
  → podman start wallfacer-impl-<uuid8>

Each turn (via Launch → launchViaImplWorker):
  → (refresh board.json on host — visible via bind mount)
  → podman exec wallfacer-impl-<uuid8> \
      /usr/local/bin/entrypoint.sh \
      -p "<prompt>" --resume <sessionID> --verbose --output-format stream-json

Task completes / cancelled:
  → podman rm -f wallfacer-impl-<uuid8>
```

**Board context refresh:** Board context is mounted as a bind mount from a host directory.
Before each turn, the host writes an updated `board.json` to this directory. The container
sees the update immediately via the bind mount — no container restart needed.

**Sibling worktree mounts:** Sibling task worktrees are mounted read-only at container
creation time. New sibling tasks started after the container was created will not be
visible. This is acceptable — the board.json manifest still lists them, and the agent
can reference their prompts/status even without filesystem access. If full sibling
visibility is required, the worker must be recreated when the sibling set changes (rare).

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

## Implementation Design

### New Types Inside LocalBackend

```go
// internal/runner/backend_local_worker.go

// auxWorker manages a long-lived container that serves auxiliary agent
// invocations (title, oversight, commit message) via podman exec.
type auxWorker struct {
    mu            sync.Mutex
    command       string          // container runtime binary
    containerName string
    spec          ContainerSpec   // create-mode spec (no --rm, sleep entrypoint)
    alive         bool
}

func (w *auxWorker) ensureRunning(ctx context.Context) error
func (w *auxWorker) exec(ctx context.Context, cmd []string) (SandboxHandle, error)
func (w *auxWorker) stop()

// implWorker manages a long-lived per-task container that serves
// implementation turns via podman exec.
type implWorker struct {
    mu            sync.Mutex
    command       string
    containerName string
    spec          ContainerSpec
    alive         bool
    taskID        uuid.UUID
}

func (w *implWorker) ensureRunning(ctx context.Context) error
func (w *implWorker) exec(ctx context.Context, cmd []string) (SandboxHandle, error)
func (w *implWorker) stop()
```

Note: Unlike the previous design that extended `ContainerExecutor` with `Create`, `Start`,
`ExecInContainer`, `IsRunning`, `Remove`, these workers call `podman`/`docker` directly
via `cmdexec` — they are implementation details of `LocalBackend`, not interface methods.

### Health Checks and Recovery

`auxWorker.ensureRunning()`:
1. If `alive`, check `podman inspect --format '{{.State.Running}}' <name>` — if true, return nil
2. `podman rm -f <name>` — clean up dead container
3. `podman create <name> ...` + `podman start <name>`
4. If any step fails, return error; `LocalBackend.Launch()` falls back to ephemeral

Graceful degradation ensures the system never breaks — it just loses the performance
optimization and falls back to ephemeral `podman run --rm`.

### ContainerSpec Changes

`ContainerSpec` needs a method to identify its mount profile so `LocalBackend.Launch()`
can route to the right strategy:

```go
// MountProfile returns the mount profile for this spec based on its shape.
// Profile A: has worktree bind mounts (implementation/test)
// Profile B: has workspace read-only mounts (refinement/ideation)
// Profile C: minimal, claude-config only (title/oversight/commit)
func (s ContainerSpec) MountProfile() MountProfile
```

Alternatively, the caller can pass a hint via a label or a new field. The profile
detection should be simple — Profile A has RW worktree mounts, Profile C has no
workspace mounts at all.

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_AUX_WORKERS` | `true` | Enable shared auxiliary worker containers |
| `WALLFACER_IMPL_WORKERS` | `true` | Enable per-task implementation workers |

When disabled, `LocalBackend.Launch()` always uses ephemeral containers. These flags
only affect `LocalBackend` — K8s and remote backends ignore them.

---

## Implementation Plan

All phases assume [Pluggable Sandbox Backends](cloud-sandbox-executor.md) Phase 1
(Interface Extraction + Local Backend) is complete.

### Phase 1: Auxiliary Workers (Profile C)

1. Add `auxWorker` type in `internal/runner/backend_local_worker.go`
2. Implement `ensureRunning`, `exec` (returns `SandboxHandle`), `stop`, health check
3. Add `MountProfile()` to `ContainerSpec` for routing decisions
4. Wire `LocalBackend.Launch()` to route Profile C specs through aux workers
5. Add integration test: launch worker, exec title generation, verify output matches ephemeral
6. Feature-flagged behind `WALLFACER_AUX_WORKERS`

### Phase 2: Auxiliary Workers — Full Rollout

1. Verify oversight and commit message work through aux worker (they should — same Profile C)
2. Test concurrent exec behavior under load (multiple tasks completing simultaneously)
3. Add lifecycle timing metrics (worker create, exec, health check)

### Phase 3: Implementation Workers (Profile A)

1. Add `implWorker` type for per-task long-lived containers
2. Wire `LocalBackend.Launch()` to route Profile A specs through impl workers
3. Handle worker recreation on sync operations (kill before rebase, recreate on next turn)
4. Handle worker cleanup on task completion/cancellation/failure
5. Feature-flagged behind `WALLFACER_IMPL_WORKERS`

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

| Scenario | Current (ephemeral) | With workers |
|----------|-------------------|--------------|
| Single auxiliary invocation | 0.5–2 s startup + agent time | ~50–100 ms exec + agent time |
| 3-turn implementation task | 3 × (0.5–2 s) startup | 1 × (0.5–2 s) create + 3 × ~100 ms exec |
| 5 concurrent tasks, each with title + 3 turns + oversight + commit | ~30 container cycles | ~5 impl creates + ~15 exec + shared aux exec |

The biggest win is for auxiliary agents: title + oversight + commit currently cost
~1.5–6 s of pure container overhead per task. With a shared worker, this drops to
~150–300 ms total.

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Worker container dies mid-task | Aux/impl invocation fails | `ensureRunning` detects and recreates; fallback to ephemeral |
| Filesystem state accumulation | Stale caches, temp files affect behavior | Profile C has no workspace mounts; Profile A workers are per-task and short-lived |
| `claude-config` contention | Concurrent exec corrupts session state | Claude CLI uses file locking; add `sync.Mutex` if needed |
| `podman exec` not available | Some container runtimes may not support exec | Feature flag disables workers; fallback to ephemeral |
| Stale sibling mounts (Profile A) | New sibling tasks not visible in container | Board.json still lists them; accept limitation or recreate worker |
| Backend interface change | If `SandboxBackend` changes, workers need updating | Workers are internal to `LocalBackend`; no interface coupling |
