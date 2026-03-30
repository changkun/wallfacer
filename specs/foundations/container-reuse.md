---
title: Sandbox Reuse
status: complete
depends_on:
  - specs/foundations/sandbox-backends.md
affects:
  - internal/sandbox/
  - internal/runner/
effort: xlarge
created: 2026-03-27
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Sandbox Reuse

## Problem

Wallfacer created and destroyed an ephemeral sandbox for every agent invocation — implementation turns, title generation, oversight summaries, commit messages, refinement, and ideation. Every invocation paid startup overhead:

| Backend | Startup cost | What's wasted |
|---------|-------------|---------------|
| Local (podman/docker) | 0.5–2 s per container (namespace, cgroup, overlay) | Warm caches, compiled artifacts |
| K8s | 5–30 s per pod (scheduling, image pull, volume attach) | Everything — pods are fully ephemeral |

For a single task lifecycle that generates a title, runs 3 implementation turns, produces an oversight summary, and generates a commit message, that was 6 sandbox create/destroy cycles. With 5 concurrent tasks, the system performed ~30 lifecycle operations per batch.

## Strategy

Keep a sandbox alive and `exec` into it rather than create/destroy for each invocation. The key constraint: bind mounts are immutable after container creation, so per-task worktree paths force a **per-task worker** model rather than a shared pool.

The approach was per-task: each task gets a single long-lived container at its first invocation. All subsequent invocations for the same task (implementation turns, title, oversight, commit message) reuse the same container via `podman exec`. Invocations without a task scope (ideation, refinement) continue using ephemeral containers.

Three strategies were evaluated:

| Strategy | Verdict |
|----------|---------|
| Container pool per role | Only viable for Profile C (minimal mounts). Limited benefit — pooling infrastructure adds complexity for marginal gain. |
| Long-lived worker + exec | **Selected.** Per-task workers eliminate per-turn churn. All agent roles for a task share one container. |
| Container pause/unpause | Incompatible — Claude CLI is one-shot; no running process to pause after exit. |

A shared aux worker for Profile C (title/oversight/commit across tasks) was considered but rejected: session state leakage (`~/.claude/`), accumulated temp files, and stale caches affecting unrelated tasks. Per-task workers provide complete isolation.

## Design

### Execution Routing

`LocalBackend.Launch()` checks the `wallfacer.task.id` label on the container spec. If present and workers are enabled, the invocation routes through a per-task worker. Otherwise, the existing ephemeral path is used.

```go
func (b *LocalBackend) Launch(ctx context.Context, spec ContainerSpec) (Handle, error) {
    taskID := spec.Labels["wallfacer.task.id"]
    if taskID != "" && b.enableTaskWorkers {
        h, err := b.launchViaTaskWorker(ctx, spec, taskID)
        if err != nil {
            return b.launchEphemeral(ctx, spec) // graceful fallback
        }
        return h, nil
    }
    return b.launchEphemeral(ctx, spec)
}
```

The returned `Handle` is identical regardless of path — the runner never knows whether the container was ephemeral or a worker exec.

### Worker Lifecycle

```
First invocation (any agent role for this task):
  → podman create --name wallfacer-worker-<uuid8>
      --entrypoint '["sleep","infinity"]'
      <all mounts from ContainerSpec.BuildCreate()>
  → podman start wallfacer-worker-<uuid8>
  → podman exec wallfacer-worker-<uuid8> <agent command>

Subsequent invocations (same task):
  → podman exec wallfacer-worker-<uuid8> <agent command>

Task completes / cancelled / failed:
  → podman rm -f wallfacer-worker-<uuid8>
```

Workers are created lazily on first `Launch()` and cleaned up by the runner via `StopTaskWorker()` at task completion, cancellation, or failure. `Shutdown()` stops all active workers on server exit. Before sync operations (rebase), the worker is stopped and auto-recreated on the next launch.

### Key Types

```go
// internal/sandbox/worker.go
type taskWorker struct {
    mu            sync.Mutex
    command       string        // container runtime binary
    containerName string
    createArgs    []string      // podman create args (computed once from spec)
    alive         bool
}

// internal/sandbox/local.go
type LocalBackend struct {
    command           string
    taskWorkers       map[string]*taskWorker
    taskWorkersMu     sync.Mutex
    enableTaskWorkers bool
    reg               *metrics.Registry
}

// internal/sandbox/backend.go
type WorkerManager interface {
    StopTaskWorker(taskID string)
    ShutdownWorkers()
    WorkerStats() WorkerStatsInfo
}
```

### execHandle

`taskWorker.exec()` returns an `execHandle` that wraps `localHandle` but overrides `Kill()` to only kill the exec process — not the worker container. This lets the runner cancel an in-flight agent invocation without destroying the container that subsequent invocations need.

### Health Checks and Fallback

`ensureRunning()` verifies the worker container is alive via `podman inspect` before each exec. If the container has died (OOM, runtime crash), it is recreated transparently. If recreation fails, `Launch()` falls back to the ephemeral path so the system never breaks — it just loses the performance optimization.

### Key Decisions

- **Per-task, not shared.** Each task gets its own container. No cross-task session leakage, no `claude-config` contention, no stale filesystem state from unrelated tasks.
- **Graceful fallback.** Worker failure always falls back to ephemeral. The feature flag `WALLFACER_TASK_WORKERS` (default: `true`) can disable workers entirely.
- **`BuildCreate()` on `ContainerSpec`.** Produces `podman create` args with a sleep entrypoint (no `--rm`). Keeps all volume mounts, labels, env, and resource limits from the original spec.
- **Label-based routing.** The task ID label (`wallfacer.task.id`) already existed for container monitoring. Reusing it for worker routing avoids new fields on `ContainerSpec` or `Backend`.
- **`WorkerManager` as optional interface.** Not all backends need workers. The runner type-asserts to `WorkerManager` when available. The `Backend` interface stays unchanged.

## Outcome

### Package Changes

```
internal/sandbox/
  worker.go     — taskWorker, execHandle (NEW)
  local.go      — LocalBackend extended with taskWorkers map, routing, WorkerManager impl
  backend.go    — WorkerManager interface, WorkerStatsInfo (NEW)
  spec.go       — BuildCreate(), BuildExec() (NEW)
```

### Runner Integration

- `RunBackground()` defers `StopTaskWorker()` after `Run()` returns
- `SyncWorktrees()` stops the worker before rebasing (auto-recreated on next launch)
- `CancelTask` handler calls `StopTaskWorker()` alongside `KillContainer()`
- `Shutdown()` calls `ShutdownWorkers()` before waiting for background goroutines
- Title, oversight, and commit message specs now include the `wallfacer.task.id` label so they route through the per-task worker

### Filesystem Caches

Named volume mounts for dependency caches (`~/.npm`, `~/.cache/pip`, `~/.cargo/registry`, `~/.cache/go-build`) are available via `WALLFACER_DEPENDENCY_CACHES` (default: `false`, opt-in). Volume names include the workspace key so different groups don't share caches.

### Metrics

Prometheus counters track worker lifecycle:
- `wallfacer_container_worker_creates_total`
- `wallfacer_container_worker_execs_total`
- `wallfacer_container_worker_fallbacks_total`

`GET /api/debug/runtime` includes `worker_stats` with enabled flag and active worker count. The Settings > About tab displays these stats alongside goroutine count, heap size, active containers, and circuit breaker state.

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_TASK_WORKERS` | `true` | Enable per-task worker containers |
| `WALLFACER_DEPENDENCY_CACHES` | `false` | Mount named volumes for dependency caches |

### Performance

| Scenario | Before (ephemeral) | After (per-task workers) |
|----------|-------------------|------------------------|
| 3-turn task + title + oversight + commit | 6 × (0.5–2 s) startup | 1 × (0.5–2 s) create + 6 × ~100 ms exec |
| 5 concurrent tasks, each 6 invocations | ~30 container cycles | 5 creates + 30 exec calls |

## Design Evolution

The spec originated as a hybrid design with two worker types: **shared aux workers** (one per sandbox type for title/oversight/commit) and **per-task impl workers** (one per task for implementation turns). During implementation, the shared aux worker was dropped in favor of a unified per-task model:

1. **Shared aux workers rejected.** Cross-task `claude-config` contention, session state leakage, and accumulated temp files made shared workers unreliable. Per-task workers provide clean isolation with the same startup savings.

2. **`LocalBackendConfig` struct.** The original `NewLocalBackend(command)` grew to accept a `LocalBackendConfig` with `EnableTaskWorkers` and `Reg` fields as the feature matured.

3. **`execHandle` added.** Not in the original design. Needed because `localHandle.Kill()` removes the container — which would destroy the worker on every agent cancellation. `execHandle` overrides Kill to only kill the exec process.

4. **Label is `wallfacer.task.id` (dots), not `wallfacer.task-id` (dashes).** The spec used dashes; the existing codebase used dots. Implementation followed the codebase.

5. **Dependency caches opt-in.** Originally unscoped. Made opt-in (`WALLFACER_DEPENDENCY_CACHES=false` default) because persistent caches can affect reproducibility.

## Bugs Found and Fixed

1. **`BuildCreate()` entrypoint format** — `--entrypoint '["sleep","infinity"]'` (JSON array) was not parsed correctly by podman when passed via Go's `exec.Command`. Fix: use `--entrypoint sleep` as a plain string with `infinity` as the CMD argument after the image.

2. **Container name mismatch** — `BuildCreate()` used the spec's original name (e.g., `wallfacer-title-xxx`) for `--name`, but `ensureRunning()` used the worker name (`wallfacer-worker-xxx`) for `podman start`. The container was created with one name and started with another → exit 125. Fix: override `spec.Name` with the worker name before calling `BuildCreate()`. Also clean up the container on start failure to avoid leaving "Created" containers.

3. **`podman exec` skips ENTRYPOINT** — `podman exec` does not invoke the image's ENTRYPOINT automatically (only `podman run` does). The agent command (`-p "..."`) was passed directly → "executable `-p` not found". Fix: `exec` now explicitly calls `/usr/local/bin/entrypoint.sh` before the command args. The entrypoint path is configurable per worker.

4. **Premature worker cleanup** — `StopTaskWorker` was deferred in `RunBackground`, so it ran when `Run()` returned. But title, oversight, and commit agents launch as separate background goroutines AFTER `Run()`. Each found no worker and created a new one, inflating the create count. Fix: moved cleanup to `CleanupWorktrees` (commit pipeline) so the worker lives until all agents finish.

5. **Worker leak on terminal state shortcuts** — Some paths to Done skip the commit pipeline (idea-agent completion, auto-submit without session). Workers leaked because `CleanupWorktrees` was never called. Fix: added `StopTaskWorker` to all `ForceUpdateTaskStatus → Done` paths and to archive handlers as a safety net.

## Future Work

- **Cleanup task (task-11):** After production verification (~2 weeks), remove `WALLFACER_TASK_WORKERS` flag and ephemeral fallback. Workers become the only strategy for task-scoped containers.
- **Overlay snapshots / CRIU:** Deferred — high complexity for diminishing returns vs. named volume caches.
