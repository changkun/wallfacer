# M3a: Overlay Snapshots & CRIU Checkpoint/Restore

**Status:** Not started
**Depends on:** [M3: Container Reuse](03-container-reuse.md) — complete

## Problem

Per-task workers (M3) eliminated per-invocation container create/destroy overhead, reducing startup from 0.5–2 s to ~100 ms per exec. But two performance bottlenecks remain:

1. **Cold-start penalty on worker creation.** The first invocation for each task still pays full container startup (namespace, cgroup, overlay mount). For burst workloads — e.g., 20 tasks promoted simultaneously — this produces a thundering herd of `podman create` + `podman start` calls competing for kernel resources.

2. **Lost in-container state on worker recreation.** When a worker is stopped for sync/rebase or dies (OOM, crash), `ensureRunning()` recreates it from scratch. Any warm state — compiled artifacts, populated caches, resolved dependencies — is lost. Named volume caches (`WALLFACER_DEPENDENCY_CACHES`) partially address this for package managers, but build caches, editor state, and tool configurations inside the container filesystem are discarded.

3. **No pre-warming.** Workers are created lazily on first `Launch()`. There is no mechanism to pre-create containers before tasks are promoted, or to snapshot a "warm" container state for instant cloning.

### Cost of status quo

| Scenario | Current cost | With this spec |
|----------|-------------|----------------|
| 20-task burst promotion | 20 × 0.5–2 s serial creates | 20 × ~50 ms clone from snapshot |
| Worker recreation after sync | Full cold start, caches lost | Restore from checkpoint, state preserved |
| Repeated similar tasks (same repo, same deps) | Each builds from scratch | Clone from warm template snapshot |

## Strategy

Two complementary techniques, implementable independently:

### Overlay Snapshots (container filesystem)

Use the container runtime's snapshot/clone capabilities to create copy-on-write clones of a warm container filesystem. Instead of `podman create` from an image, clone from a previously committed container state that already has dependencies installed and caches populated.

**Mechanism:** `podman commit` a running worker after its first successful invocation → tagged snapshot image. Subsequent workers for similar tasks `podman create` from this snapshot instead of the base image. The overlay filesystem provides CoW efficiency — only divergent writes consume disk.

### CRIU Checkpoint/Restore (process state)

Use CRIU (Checkpoint/Restore in Userspace) via `podman container checkpoint`/`podman container restore` to serialize and restore the full process state of a worker container — including memory, file descriptors, and timer state.

**Mechanism:** Before `StopTaskWorker()` during sync, checkpoint the container instead of destroying it. On next `Launch()`, restore from checkpoint instead of cold-starting. For task completion, checkpoint can optionally be retained as a template for future similar tasks.

**CRIU is Linux-only** and requires kernel capabilities (`CAP_SYS_PTRACE`, `CAP_SYS_ADMIN` or rootful podman). This technique is positioned as an opt-in acceleration for Linux deployments, not a cross-platform requirement.

## Design

### Snapshot Manager

A new `SnapshotManager` coordinates snapshot lifecycle, sitting alongside the existing `WorkerManager`:

```go
// internal/sandbox/snapshot.go
type SnapshotManager interface {
    // CreateSnapshot commits a running worker's filesystem to a tagged image.
    // The tag encodes the workspace key and sandbox type for cache-hit routing.
    CreateSnapshot(ctx context.Context, containerName string, tag SnapshotTag) error

    // FindSnapshot returns the best matching snapshot for a container spec,
    // or empty if no usable snapshot exists.
    FindSnapshot(ctx context.Context, spec ContainerSpec) (SnapshotTag, bool)

    // PruneSnapshots removes snapshots older than maxAge or exceeding maxCount.
    PruneSnapshots(ctx context.Context, maxAge time.Duration, maxCount int) error

    // ListSnapshots returns all managed snapshots with metadata.
    ListSnapshots(ctx context.Context) ([]SnapshotInfo, error)
}

type SnapshotTag struct {
    Image        string // e.g., "wallfacer-snapshot:ws-abc123-claude-20260328T1504"
    WorkspaceKey string
    SandboxType  string
    CreatedAt    time.Time
}

type SnapshotInfo struct {
    Tag       SnapshotTag
    Size      int64  // bytes (overlay diff size, not virtual)
    BaseImage string // original image this was committed from
}
```

### Snapshot Lifecycle

```
First task invocation for a workspace:
  → podman create from base image (existing path)
  → Agent runs, installs deps, warms caches
  → On successful completion: podman commit <worker> wallfacer-snapshot:<tag>

Subsequent task in same workspace:
  → FindSnapshot() returns matching tag
  → podman create from wallfacer-snapshot:<tag> (warm start)
  → Agent runs with pre-populated caches

Snapshot staleness:
  → Base image updated → invalidate snapshots derived from old base
  → Workspace config changed → invalidate snapshots for that workspace key
  → Max age exceeded → pruned by background goroutine
```

### Integration with Worker Lifecycle

The snapshot layer sits below the worker layer — it changes _how_ workers are created, not _how_ they are used. `taskWorker.ensureRunning()` gains a snapshot-aware creation path:

```go
func (w *taskWorker) ensureRunning(ctx context.Context) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    if w.alive {
        if w.healthCheck(ctx) {
            return nil
        }
        w.alive = false
    }

    // Try snapshot-accelerated creation
    if w.snapMgr != nil {
        if tag, ok := w.snapMgr.FindSnapshot(ctx, w.spec); ok {
            if err := w.createFromSnapshot(ctx, tag); err == nil {
                w.alive = true
                return nil
            }
            // Snapshot failed — fall through to cold create
        }
    }

    // Cold create (existing path)
    return w.coldCreate(ctx)
}
```

### CRIU Checkpoint/Restore

Checkpoint wraps `podman container checkpoint --export`:

```go
// internal/sandbox/checkpoint.go
type CheckpointManager interface {
    // Checkpoint exports the container state to a tar archive.
    Checkpoint(ctx context.Context, containerName string, dest string) error

    // Restore creates a new container from a checkpoint archive.
    Restore(ctx context.Context, archive string, newName string) (string, error)

    // Available reports whether CRIU is usable on this system.
    Available(ctx context.Context) bool
}
```

The checkpoint path is used in two places:

1. **Sync/rebase:** Instead of `podman rm -f` + recreate, checkpoint the worker, perform the rebase, then restore. File-level changes from the rebase are visible because worktrees are bind-mounted (outside the container filesystem).

2. **Task completion (optional):** Checkpoint before cleanup to retain as a template. Useful when the next task will work in the same repo with similar dependencies.

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_SNAPSHOTS` | `false` | Enable overlay snapshot acceleration |
| `WALLFACER_SNAPSHOT_MAX_AGE` | `24h` | Maximum age before snapshot is pruned |
| `WALLFACER_SNAPSHOT_MAX_COUNT` | `10` | Maximum snapshots per workspace key |
| `WALLFACER_CRIU` | `false` | Enable CRIU checkpoint/restore (Linux only) |
| `WALLFACER_CRIU_CHECKPOINT_DIR` | `~/.wallfacer/checkpoints/` | Directory for checkpoint archives |

### Snapshot Tagging and Matching

Snapshot tags encode enough context to determine cache validity:

```
wallfacer-snapshot:<workspace-key>-<sandbox-type>-<timestamp>
```

`FindSnapshot()` matches on workspace key + sandbox type, returning the most recent valid snapshot. A snapshot is invalid if:
- The base image digest has changed (image was rebuilt/pulled)
- The workspace key no longer exists
- The snapshot exceeds `WALLFACER_SNAPSHOT_MAX_AGE`

### Pruning

A background goroutine runs on a configurable interval (default: 1 hour) and removes snapshots that:
- Exceed `WALLFACER_SNAPSHOT_MAX_AGE`
- Exceed `WALLFACER_SNAPSHOT_MAX_COUNT` per workspace key (oldest first)
- Reference a base image that no longer exists locally

Pruning uses `podman rmi` for snapshot images and `rm` for checkpoint archives.

## Implementation Plan

### Phase 1: Overlay Snapshots

1. **`internal/sandbox/snapshot.go`** — `SnapshotManager` implementation using `podman commit` / `podman rmi`.
2. **Extend `taskWorker.ensureRunning()`** — snapshot-aware creation path with fallback.
3. **Snapshot creation hook** — after first successful exec in a new worker, asynchronously commit.
4. **Pruning goroutine** — background cleanup in `LocalBackend`.
5. **Configuration** — env vars parsed in `envconfig`, wired through `LocalBackendConfig`.
6. **Metrics** — `wallfacer_snapshot_creates_total`, `wallfacer_snapshot_hits_total`, `wallfacer_snapshot_misses_total`, `wallfacer_snapshot_prune_total`.

### Phase 2: CRIU Checkpoint/Restore

1. **`internal/sandbox/checkpoint.go`** — `CheckpointManager` implementation wrapping `podman container checkpoint/restore`.
2. **Capability detection** — `Available()` checks CRIU binary, kernel version, user namespace support.
3. **Sync integration** — `SyncWorktrees()` uses checkpoint instead of stop+recreate when CRIU is available.
4. **Checkpoint storage** — manage archive lifecycle in `WALLFACER_CRIU_CHECKPOINT_DIR`.
5. **Metrics** — `wallfacer_checkpoint_creates_total`, `wallfacer_checkpoint_restores_total`, `wallfacer_checkpoint_failures_total`.

### Phase 3: Pre-warming (stretch)

1. **Speculative worker creation** — when auto-promoter identifies pending backlog tasks, pre-create workers from snapshots before promotion.
2. **Warm pool** — maintain a small pool (1–2) of pre-created workers per active workspace, ready for instant assignment.

## Constraints and Risks

| Risk | Mitigation |
|------|------------|
| Snapshot staleness (stale deps, wrong config) | Base image digest tracking; max age; workspace key scoping |
| Disk usage from accumulated snapshots | Pruning goroutine with configurable limits; overlay CoW minimizes cost |
| CRIU kernel compatibility | Capability detection at startup; graceful fallback to cold create |
| CRIU only works with rootful podman | Document requirement; `Available()` checks rootful or user-ns support |
| Snapshot commit blocks the worker | Async commit after exec completes; worker remains usable during commit |
| Checkpoint archives can be large (100s of MB) | Configurable retention; compress archives; checkpoint is opt-in |

## Performance Expectations

| Scenario | Current (M3) | With snapshots | With CRIU |
|----------|-------------|----------------|-----------|
| Worker cold create | 0.5–2 s | 0.2–0.5 s (warm image, pre-populated layers) | N/A |
| Worker recreation after sync | 0.5–2 s (cold) | 0.2–0.5 s (from snapshot) | ~0.1–0.3 s (restore) |
| 20-task burst | 20 × 0.5–2 s | 20 × 0.2–0.5 s | 20 × 0.1–0.3 s |
| Disk cost per snapshot | N/A | 10–100 MB (overlay diff) | 50–500 MB (full checkpoint) |

## Non-Goals

- **Cross-host snapshot sharing.** Snapshots are local to the machine. Cloud backends (M6) would need a registry-based approach, which is out of scope.
- **Live migration.** CRIU supports live migration but this spec only uses checkpoint/restore for local acceleration.
- **Replacing named volume caches.** Snapshots and named volumes are complementary. Named volumes persist package manager caches across container restarts; snapshots preserve broader filesystem state (build artifacts, tool configs).
- **Windows/macOS CRIU.** CRIU is Linux-only. The snapshot path (overlay) works everywhere podman/docker runs. CRIU is a Linux-only optimization.
