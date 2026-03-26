# Pluggable Sandbox Backends

**Date:** 2026-03-23
**Updated:** 2026-03-26

## Already Implemented

All local-backend infrastructure is complete. The `internal/sandbox` package defines the pluggable backend abstraction and its local implementation:

- **`internal/sandbox/`** — Single package containing: `Backend` and `Handle` interfaces, `BackendState` enum (6 states), `ContainerSpec`/`VolumeMount` (declarative container description), `ContainerInfo` (runtime metadata), `LocalBackend`/`localHandle` (podman/docker via os/exec), `ParseContainerList`/`IsUUID` (JSON parsing helpers). Tests in `parse_test.go`.
- **Runner wiring** (`internal/runner/`): `Runner.backend sandbox.Backend` field. All container callers (`runContainer`, `RunIdeation`, `GenerateTitle`, `runRefinementContainer`, `generateCommitMessage`) use `r.backend.Launch()` + handle. Kill methods use `handle.Kill()` exclusively. `MockSandboxBackend` in `executor_mock_test.go`.
- **Handle-aware registry** (`internal/runner/registry.go`): `containerEntry{name, handle, logReader}`. `SetHandle()`/`GetHandle()` for handle storage; kill methods route through handle.
- **Log streaming**: SSE uses `podman logs -f` via `logpipe.Start()` — intentionally kept (avoids back-pressure, supports late-joining clients). `logpipe.StartReader()` available for future use.
- **Backend selection**: `WALLFACER_SANDBOX_BACKEND` env var (values: `local`, default: `local`). Parsed in `envconfig`, selected in `NewRunner()`, reported by `wallfacer doctor`.

## Problem

The wallfacer runner originally executed all sandbox containers via a `ContainerExecutor` interface that passed raw CLI `args []string`, leaking the podman/docker abstraction. This had two consequences:

1. **No lifecycle management.** A container was launched, blocked until exit, and was cleaned up — no intermediate state tracking.

2. **Hard-coupled to local runtime.** Cloud deployment requires dispatching to remote backends (Kubernetes Jobs, cloud VM pools, remote Docker hosts) with structured input, not CLI args.

Both problems are now solved by the `SandboxBackend` interface that accepts a `ContainerSpec`, returns a stateful `SandboxHandle`, and works identically for local and remote backends. The remaining work is adding remote backend implementations (K8s, remote Docker).

## Current Architecture

```
Runner.Run()                  (internal/runner/execute.go)
  → buildContainerSpecForSandbox()   → constructs ContainerSpec
  → backend.Launch(ctx, spec)        → returns SandboxHandle (non-blocking)
  → handle.Stdout() / Stderr()       → io.ReadCloser streams
  → handle.Wait()                    → exit code
  → handle.Kill()                    → on context cancel
```

Key types involved:

- **`sandbox.Backend`** (`internal/sandbox/backend.go`) — Core interface. `Launch(ctx, spec) → (Handle, error)` and `List(ctx) → ([]ContainerInfo, error)`.
- **`sandbox.Handle`** (`internal/sandbox/backend.go`) — Stateful handle: `State()`, `Stdout()`, `Stderr()`, `Wait()`, `Kill()`, `Name()`.
- **`sandbox.LocalBackend`** (`internal/sandbox/local.go`) — Production implementation; `Launch()` starts non-blocking via `cmd.Start()`, `List()` shells out to `ps --format json`.
- **`sandbox.ContainerSpec`** (`internal/sandbox/spec.go`) — Structured container description. `Build()` returns `[]string` CLI args.
- **`sandbox.ContainerInfo`** (`internal/sandbox/backend.go`) — Runtime container metadata.
- **`containerRegistry`** (`internal/runner/registry.go`) — `syncmap.Map[uuid.UUID, containerEntry]` storing name + optional `sandbox.Handle`. Kill methods route through handle.
- **Circuit breaker** (`internal/pkg/circuitbreaker`) — Three-state breaker gating `runContainer()` calls; uses exit code 125 detection from handle.

---

## Design

### Sandbox Lifecycle States

Every sandbox container progresses through a defined set of states. Backends report these states via the `SandboxHandle`, and the runner uses them for logging, circuit-breaker decisions, and status reporting.

```
Creating → Running → Streaming → Stopping → Stopped
                 ↘                           ↗
                   ────────→ Failed ←────────
```

| State | Meaning | Who sets it |
|-------|---------|-------------|
| `Creating` | Backend is provisioning the container (image pull, pod scheduling, `podman run` fork). | `Backend.Launch()` internally, before returning the handle. |
| `Running` | Container process is alive but has not yet produced output. | Backend, once the container runtime confirms the process started. |
| `Streaming` | Container is alive and output is being read from `Stdout()`. | Runner, after first successful read from the handle. |
| `Stopping` | `Kill()` has been called; waiting for the container to exit. | Backend, inside `Kill()`. |
| `Stopped` | Container exited (success or non-zero). Terminal state. | Backend, after `Wait()` returns. |
| `Failed` | Container could not be created or crashed before producing output. Terminal state. | Backend, on launch failure or runtime error. |

### Interfaces (as implemented in `internal/sandbox/`)

```go
type BackendState int

const (
    StateCreating  BackendState = iota
    StateRunning
    StateStreaming
    StateStopping
    StateStopped
    StateFailed
)

type Backend interface {
    Launch(ctx context.Context, spec ContainerSpec) (Handle, error)
    List(ctx context.Context) ([]ContainerInfo, error)
}

type Handle interface {
    State() BackendState
    Stdout() io.ReadCloser
    Stderr() io.ReadCloser
    Wait() (exitCode int, err error)
    Kill() error
    Name() string
}
```

### Design Notes

- **No Suspend/Resume at the backend level.** Claude CLI is a one-shot process — it exits when done. Long-lived worker containers (see [Container Reuse](03-container-reuse.md)) can be paused via `podman pause`/`unpause` between exec invocations, but that is an optimization internal to `LocalBackend`.
- **No Snapshotting state.** Filesystem snapshots are a `LocalBackend` optimization covered in [Container Reuse](03-container-reuse.md).
- **Timeout is task-level, not sandbox-level.** The runner enforces `task.Timeout` and kills via `handle.Kill()`.

---

## Tasks

### Task 11: Kubernetes backend (future)

**Goal:** Implement `K8sBackend` for dispatching sandbox containers as K8s Jobs.

**Work:**
1. Add `internal/sandbox/k8s.go` implementing `sandbox.Backend` via `client-go`
2. Map `ContainerSpec` → K8s Job spec (see design table above)
3. Implement `k8sHandle` with state tracking via pod watch
4. Implement log streaming via pod log follow API
5. Handle worktree mounting via shared PVC (see Worktree Management section)
6. Add `k8s` as a value for `WALLFACER_SANDBOX_BACKEND`
7. Integration tests with kind or minikube

**New dependency:** `k8s.io/client-go`

This task is deliberately left as a single unit — it should be broken down further when work begins.

---

### Task 12: Remote Docker backend (optional, future)

**Goal:** Implement `RemoteDockerBackend` for SSH/HTTPS dispatch to a remote Docker host.

**Work:**
1. Add `internal/sandbox/remote.go` implementing `sandbox.Backend` via Docker client SDK
2. SSH tunnel or TLS client cert for authentication
3. State tracking via Docker events API
4. Volume mounting via NFS or pre-provisioned volumes on the remote host

Lower priority than K8s. Useful for simple single-host remote setups.

---

## Task Dependency Graph

```
Task 11 (K8s backend)
Task 12 (Remote Docker backend)
```

Tasks 11 and 12 are independent and can run in parallel. All local-backend infrastructure is complete.

---

## Worktree Management in Cloud

The biggest architectural challenge for remote backends. Currently:

1. `Runner.ensureTaskWorktrees()` creates worktrees at `~/.wallfacer/worktrees/<task-uuid>/` (`internal/runner/worktree.go`)
2. `buildContainerSpecForSandbox()` bind-mounts worktree paths into the container
3. Agent writes to `/workspace/<repo>` inside the container (= the worktree on the host)
4. After task completion, runner commits from the worktree and cleans it up

In a K8s/remote backend, the worktree filesystem must be accessible to both the wallfacer server (for git operations) and the sandbox pod (for agent writes). Options:

| Approach | How | Tradeoffs |
|----------|-----|-----------|
| **Shared volume (PVC/NFS)** | Both server and pods mount the same volume | Simple; requires ReadWriteMany PVC; potential contention |
| **Server-side worktree + rsync** | Server creates worktree, syncs to pod volume pre-launch, syncs back post-completion | No shared storage needed; adds latency; complex |
| **In-pod worktree creation** | Init container creates worktree; server reads results via K8s exec or shared volume | Decouples server from filesystem; git operations move to pod |
| **Git server sidecar** | Each pod has a git sidecar that handles worktree ops via API | Clean separation; most complex |

**Recommended:** Shared volume (PVC/NFS) for initial implementation. Design details deferred to Task 11.

---

## Cross-Cutting Concerns

### Resource Limits

Currently set via `WALLFACER_CONTAINER_CPUS` and `WALLFACER_CONTAINER_MEMORY`, translated to `--cpus` and `--memory` CLI flags via `ContainerSpec.Build()`. For K8s, these map directly to `resources.limits`. No interface change needed — `ContainerSpec` already carries these fields; the backend interprets them.

### Sandbox Image Management

Currently checked via `podman images` / `docker images` in the handler. For K8s, images are pulled by the kubelet. The `GET /api/images` endpoint needs a backend-aware implementation:
- Local: check local image cache (as today)
- K8s: assume images are available (or check a registry)

### Network Control

`ContainerSpec.Network` is the abstraction point. Currently a single string (`"host"`, `"none"`, `"slirp4netns"`). Sufficient for local deployment. For egress filtering and DNS control, add optional fields to `ContainerSpec` when needed — this is primarily a multi-tenant concern designed in [08-cloud-multi-tenant.md](08-cloud-multi-tenant.md).

### Dependencies on Other Epics

- **Cloud Data Storage** (`02-storage-backends.md`): If the store moves to a database, the sandbox backend doesn't need to share a filesystem for task metadata — only for worktrees.
- **Multi-Tenant** (`08-cloud-multi-tenant.md`): The control plane decides which backend each user's instance uses.
