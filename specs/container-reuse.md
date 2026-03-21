# Container Reuse

**Date:** 2026-03-21

## Problem

Wallfacer creates and destroys an ephemeral container for every agent invocation —
implementation turns, title generation, oversight summaries, commit messages, refinement,
and ideation. Every invocation pays container startup overhead (image layer mount, process
init, volume attach): roughly 0.5–2 s per launch on Podman with cached images.

For a single task lifecycle that generates a title, runs 3 implementation turns, produces
an oversight summary, and generates a commit message, that is 6 container create/destroy
cycles. With 5 concurrent tasks, the host performs ~30 container lifecycle operations per
batch. The churn also stresses the container runtime's storage driver with repeated layer
mount/unmount operations.

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

Every container invocation follows:
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

**Concurrency:** Profile C pool works well — N pre-created containers serve N concurrent
auxiliary agents. Profile A cannot pool at all.

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

## Recommended Approach: Hybrid Workers

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

Title/Oversight/Commit invocation:
  → podman exec wallfacer-aux-claude \
      /usr/local/bin/entrypoint.sh \
      -p "<prompt>" --verbose --output-format stream-json

Server Shutdown:
  → podman rm -f wallfacer-aux-claude
```

**Concurrency:** Multiple `podman exec` processes can run concurrently in the same
container. Claude CLI's session state in `claude-config` uses file-level locking. If
contention is observed, serialize access with a `sync.Mutex` in the `AuxWorker` — since
auxiliary agents are fast (5–30 s), FIFO queuing is acceptable.

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

Each turn:
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

**No change to worktree lifecycle.** Worktrees are still created by `setupWorktrees()`
before the first turn and cleaned up by `cleanupWorktrees()` after the commit pipeline.
The only difference is that the container holding the bind mount lives across turns
instead of being recreated.

**Sync operation:** When a waiting/failed task is synced (rebase onto latest default
branch), the rebase happens on the host filesystem. The worktree path does not change,
so the bind mount remains valid. If the implementation worker is alive during sync, it
should be stopped first to avoid concurrent git operations:

1. `podman rm -f wallfacer-impl-<uuid8>`
2. Perform rebase on host
3. Recreate worker with same mounts on next turn

**Worktree health watcher:** The existing 2-minute health check (`StartWorktreeHealthWatcher`)
continues to monitor worktree integrity. If a worktree is restored, the implementation
worker's bind mount still points to the same path, so no worker restart is needed.

---

## Implementation Design

### New Types

```go
// internal/runner/aux_worker.go

// AuxWorker manages a long-lived container that serves auxiliary agent
// invocations (title, oversight, commit message) via podman exec.
type AuxWorker struct {
    mu            sync.Mutex
    executor      ContainerExecutor
    containerName string
    spec          ContainerSpec   // create-mode spec (no --rm, sleep entrypoint)
    alive         bool
}

func (w *AuxWorker) EnsureRunning(ctx context.Context) error
func (w *AuxWorker) Exec(ctx context.Context, cmd []string) (stdout, stderr []byte, err error)
func (w *AuxWorker) Stop()
```

### Extended ContainerExecutor Interface

```go
type ContainerExecutor interface {
    // Existing
    RunArgs(ctx context.Context, name string, args []string) (stdout, stderr []byte, err error)
    Kill(name string)

    // New — for long-lived workers
    Create(name string, args []string) error
    Start(name string) error
    ExecInContainer(ctx context.Context, name string, cmd []string) (stdout, stderr []byte, err error)
    IsRunning(name string) bool
    Remove(name string)
}
```

### Runner Changes

```go
// internal/runner/runner.go — add to Runner struct
type Runner struct {
    // ... existing fields ...
    auxWorkers map[sandbox.Type]*AuxWorker  // shared auxiliary workers
}
```

- `NewRunner()`: Initialize `auxWorkers` for configured sandbox types
- `Shutdown()`: Stop all auxiliary workers
- Turn loop in `execute.go`: Create per-task worker on first turn, reuse on subsequent
  turns, destroy on task completion/cancellation

### Modified Call Sites

| File | Current | Proposed |
|------|---------|----------|
| `title.go` | `exec.CommandContext(ctx, r.command, spec.Build()...)` | `r.auxWorker(sb).Exec(ctx, entrypointCmd)` |
| `oversight.go` | `exec.CommandContext(ctx, r.command, spec.Build()...)` | `r.auxWorker(sb).Exec(ctx, entrypointCmd)` |
| `commit.go` | `exec.CommandContext(ctx, r.command, spec.Build()...)` | `r.auxWorker(sb).Exec(ctx, entrypointCmd)` |
| `execute.go` | `runContainer()` each turn | Create worker on turn 1, `exec` on subsequent turns |

### Health Checks and Recovery

`AuxWorker.EnsureRunning()`:
1. Check `executor.IsRunning(containerName)` — if true, return nil
2. `executor.Remove(containerName)` — clean up dead container
3. `executor.Create(containerName, spec.BuildCreate()...)` + `executor.Start(containerName)`
4. If any step fails, return error; caller falls back to ephemeral `podman run --rm`

Graceful degradation ensures the system never breaks — it just loses the performance
optimization and falls back to the current behavior.

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_AUX_WORKERS` | `true` | Enable shared auxiliary worker containers |
| `WALLFACER_IMPL_WORKERS` | `true` | Enable per-task implementation workers |

When disabled, all containers use the current ephemeral `run --rm` behavior.

---

## Migration Path

### Phase 1: Auxiliary Workers (Profile C)

1. Extend `ContainerExecutor` with `Create`, `Start`, `ExecInContainer`, `IsRunning`, `Remove`
2. Implement `AuxWorker` with `EnsureRunning`, `Exec`, `Stop`, health check
3. Wire up title generation to use `AuxWorker`
4. Add integration test: launch worker, exec title generation, verify output
5. Feature-flagged behind `WALLFACER_AUX_WORKERS`

### Phase 2: Auxiliary Workers — Full Rollout

1. Migrate oversight and commit message to `AuxWorker`
2. Verify concurrent exec behavior under load (multiple tasks completing simultaneously)

### Phase 3: Implementation Workers (Profile A)

1. Refactor turn loop in `execute.go` to create worker on first turn
2. Handle worker recreation on sync operations
3. Handle worker cleanup on task completion/cancellation/failure
4. Wire up container registry tracking for the new worker containers
5. Feature-flagged behind `WALLFACER_IMPL_WORKERS`

### Phase 4: Measurement

1. Instrument container lifecycle timing (create, start, exec, remove)
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
| Worker container dies mid-task | Aux/impl invocation fails | `EnsureRunning` detects and recreates; fallback to ephemeral |
| Filesystem state accumulation | Stale caches, temp files affect behavior | Profile C has no workspace mounts; Profile A workers are per-task and short-lived |
| `claude-config` contention | Concurrent exec corrupts session state | Claude CLI uses file locking; add `sync.Mutex` if needed |
| `podman exec` not available | Some container runtimes may not support exec | Feature flag disables workers; fallback to ephemeral |
| Stale sibling mounts (Profile A) | New sibling tasks not visible in container | Board.json still lists them; accept limitation or recreate worker |
