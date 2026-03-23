# Pluggable Sandbox Backends

**Date:** 2026-03-23

## Problem

The wallfacer runner executes all sandbox containers via `os/exec` calling a local `podman`/`docker` binary (`internal/runner/executor.go`). The `ContainerExecutor` interface passes raw CLI `args []string`, which leaks the podman/docker abstraction. This has two consequences:

1. **No lifecycle management.** A container is launched, blocks until exit, and is cleaned up — there is no intermediate state tracking. The runner cannot observe whether a container is starting, streaming output, stopping, or has crashed without shelling out to `podman ps`.

2. **Hard-coupled to local runtime.** For cloud deployment, the server needs to dispatch sandbox containers to remote execution backends (Kubernetes Jobs, cloud VM pools, remote Docker hosts) without changing the task lifecycle or runner logic. A cloud-native backend needs structured input (`ContainerSpec`), not CLI args.

Both problems are solved by a single abstraction: a `SandboxBackend` interface that accepts a `ContainerSpec`, returns a stateful `SandboxHandle`, and works identically for local and remote backends.

## Current Architecture

```
Runner.Run()                  (internal/runner/execute.go)
  → buildContainerArgsForSandbox()  → constructs ContainerSpec, calls Build() for CLI args
  → executor.RunArgs(ctx, name, args) → os/exec: runs `podman run`, blocks, returns stdout/stderr
  → executor.Kill(name)               → os/exec: runs `podman kill` + `podman rm`
```

Key types involved:

- **`ContainerExecutor`** (`internal/runner/executor.go:11-17`) — Current abstraction. Two methods: `RunArgs(ctx, name, args) → (stdout, stderr, err)` and `Kill(name)`. Passes raw CLI args.
- **`osContainerExecutor`** — Production implementation; removes leftover containers, calls `cmdexec.Capture()`.
- **`ContainerSpec`** (`internal/runner/container_spec.go`) — Structured container description (Image, Name, Volumes, Env, Labels, Network, CPUs, Memory, Cmd, WorkDir, ExtraFlags). Its `Build()` method returns `[]string` CLI args.
- **`VolumeMount`** (`internal/runner/container_spec.go:10-15`) — Single bind mount or named volume descriptor (Host, Container, Options, Named).
- **`containerRegistry`** (`internal/runner/registry.go`) — `syncmap.Map[uuid.UUID, string]` tracking `taskID → containerName` for running containers.
- **`ContainerInfo`** (`internal/runner/runner.go:35-45`) — Runtime container metadata returned by `ListContainers()`, which shells out to `podman ps --format json`.
- **Log streaming** — Container stdout captured by `cmdexec.Capture()` as a blocking call; live log streaming uses a separate `logpipe.Pipe` mechanism.
- **Circuit breaker** (`internal/pkg/circuitbreaker`) — Three-state breaker (closed/open/half-open) gating `runContainer()` calls; tracks consecutive failures via atomic CAS.

## Design: Pluggable Sandbox Backends

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

The `SandboxHandle` exposes `State() SandboxState` so the runner and handler can query current state without shelling out.

### New Interfaces

```go
// SandboxState represents the lifecycle state of a sandbox container.
type SandboxState int

const (
    SandboxCreating  SandboxState = iota
    SandboxRunning
    SandboxStreaming
    SandboxStopping
    SandboxStopped
    SandboxFailed
)

// SandboxBackend abstracts where and how sandbox containers run.
type SandboxBackend interface {
    // Launch starts a container from the given spec and returns a handle.
    // The handle streams output and allows cancellation.
    // The container is in Creating state during this call and transitions
    // to Running before Launch returns (or to Failed on error).
    Launch(ctx context.Context, spec ContainerSpec) (SandboxHandle, error)

    // List returns currently running sandboxes managed by this backend.
    // Replaces the current ListContainers() shell-out.
    List(ctx context.Context) ([]ContainerInfo, error)
}

// SandboxHandle represents a running sandbox container with lifecycle tracking.
type SandboxHandle interface {
    // State returns the current lifecycle state of the container.
    State() SandboxState

    // Stdout returns a reader for the container's stdout/stderr.
    // Reading from this transitions the state to Streaming.
    Stdout() io.ReadCloser

    // Wait blocks until the container exits and returns its exit code.
    // Transitions state to Stopped on return.
    Wait() (exitCode int, err error)

    // Kill requests the container to stop. Transitions state to Stopping,
    // then Stopped once the container has exited.
    Kill() error

    // Name returns the container's unique identifier
    // (container name, pod name, VM ID, etc.).
    Name() string
}
```

### Backend Implementations

#### 1. Local Backend

The primary backend. Wraps the existing `osContainerExecutor` but returns a `SandboxHandle` with proper lifecycle state tracking instead of blocking on `cmdexec.Capture()`.

```go
type LocalBackend struct {
    command string // "podman" or "docker"
}
```

**Key changes from current `osContainerExecutor`:**

- `Launch()` calls `os/exec.Command().Start()` (non-blocking) instead of `Capture()` (blocking). Returns a `localHandle` that owns the `exec.Cmd` and its stdout pipe.
- `localHandle.Stdout()` returns the pipe reader. The runner reads and parses output while the container runs — same logic as today's `parseOutput()`, but now the handle tracks state transitions as reads occur.
- `localHandle.Wait()` calls `cmd.Wait()` and transitions to `Stopped`.
- `localHandle.Kill()` runs `podman kill` + `podman rm -f`, transitions through `Stopping` → `Stopped`.
- `List()` replaces `ListContainers()` shell-out; same `podman ps --format json` parsing but encapsulated inside the backend.

**Behavioral improvement:** Today `RunArgs()` blocks the entire goroutine until container exit, and live log streaming requires a separate `logpipe.Pipe` mechanism. With the handle-based approach, the runner reads from `Stdout()` directly, unifying output parsing and live streaming into a single path.

#### 2. Kubernetes Backend

Creates K8s Jobs via `client-go`. Each task becomes a Job with a single-container Pod.

```go
type K8sBackend struct {
    clientset kubernetes.Interface
    namespace string
    // PVC or CSI driver config for workspace volume mounting
}
```

**Mapping `ContainerSpec` → K8s Job:**

| ContainerSpec field | K8s equivalent |
|---------------------|----------------|
| `Image` | `pod.spec.containers[0].image` |
| `Volumes` | `pod.spec.volumes` + `volumeMounts` (PVC, hostPath, or NFS) |
| `Env` | `pod.spec.containers[0].env` (secrets via SecretKeyRef) |
| `Labels` | `job.metadata.labels` |
| `Network` | K8s NetworkPolicy or service mesh |
| `CPUs`, `Memory` | `resources.requests` / `resources.limits` |
| `Cmd` | `pod.spec.containers[0].args` |
| `WorkDir` | `pod.spec.containers[0].workingDir` |

**State mapping:** K8s pod phases map to sandbox states: `Pending` → `Creating`, `Running` → `Running`/`Streaming`, `Succeeded`/`Failed` → `Stopped`/`Failed`.

**Log streaming:** `k8s.io/client-go` pod log follow stream (replaces `os/exec` pipe).

**Workspace mounting:**
- Option A: PersistentVolumeClaim per user, pre-populated with cloned repos
- Option B: Init container that clones repos on Job start (slower but simpler)
- Option C: NFS/EFS shared volume with per-user subdirectories

**Worktree challenge:** Git worktrees use absolute paths in their `.git` file, referencing the main repo's `.git/worktrees/<name>/` directory. In K8s, the wallfacer server creates worktrees on its own filesystem — these paths don't exist inside the Job pod. **Solution:** Create worktrees inside the shared volume (PVC), or use an init container that creates the worktree inside the pod before the agent starts.

#### 3. Remote Docker Backend (optional)

SSH tunnel or Docker API over HTTPS to a remote Docker host.

```go
type RemoteDockerBackend struct {
    client *docker.Client // docker client SDK pointing at remote host
}
```

Useful for simple setups where a beefy VM runs all containers. Lower complexity than K8s but limited to single-host scaling. State tracking via Docker events API.

---

## Runner Changes

### Replace `executor` with `backend`

In `internal/runner/runner.go`, the `Runner` struct replaces `executor ContainerExecutor` with `backend SandboxBackend`:

```go
type Runner struct {
    // ...
    backend SandboxBackend // replaces executor ContainerExecutor
    // ...
}
```

### Refactor `runContainer` flow

Current flow in `internal/runner/container.go` calls `executor.RunArgs()` which blocks until the container exits. The new flow uses the handle for non-blocking launch, output streaming, and lifecycle tracking:

```go
func (r *Runner) runContainer(ctx context.Context, spec ContainerSpec) (*agentOutput, error) {
    handle, err := r.backend.Launch(ctx, spec)
    if err != nil {
        return nil, err
    }

    // Register for lookup by task ID (replaces containerRegistry.Set)
    r.taskContainers.SetHandle(taskID, handle)
    defer r.taskContainers.Delete(taskID)

    // Stream and parse output — same parseOutput logic, reads from handle.Stdout()
    // State transitions: Running → Streaming as output arrives
    output := r.parseAgentOutput(handle.Stdout())

    exitCode, err := handle.Wait()
    // handle.State() == SandboxStopped at this point
    // ... handle exit code, errors, circuit breaker recording, etc.
}
```

### Container registry upgrade

The `containerRegistry` currently maps `taskID → string` (container name). With handles, it could optionally map `taskID → SandboxHandle`, giving the runner direct access to `State()` and `Kill()` without needing to resolve names. This is an implementation choice — at minimum, `ContainerName()` delegates to `handle.Name()`.

### Container listing

`ListContainers()` currently shells out to `podman ps --format json` in `runner.go:139-187`. Replace with `backend.List()` — each backend implements listing natively:
- Local: same `podman ps` parsing, but encapsulated
- K8s: `client.BatchV1().Jobs().List()` with label selector
- Remote Docker: `client.ContainerList()`

### Circuit breaker

The circuit breaker (`containerCB`) wraps `backend.Launch()`. The handle's `State()` provides richer failure information — `SandboxFailed` at creation time is a circuit-breaker-relevant failure, while `SandboxStopped` with non-zero exit is an agent error (not a runtime failure).

---

## Worktree Management in Cloud

The biggest architectural challenge. Currently:

1. `Runner.ensureTaskWorktrees()` creates worktrees at `~/.wallfacer/worktrees/<task-uuid>/` (`internal/runner/worktree.go`)
2. `buildContainerArgs()` bind-mounts worktree paths into the container
3. Agent writes to `/workspace/<repo>` inside the container (= the worktree on the host)
4. After task completion, runner commits from the worktree and cleans it up

In a K8s/remote backend, the worktree filesystem must be accessible to both the wallfacer server (for git operations) and the sandbox pod (for agent writes). Options:

| Approach | How | Tradeoffs |
|----------|-----|-----------|
| **Shared volume (PVC/NFS)** | Both server and pods mount the same volume | Simple; requires ReadWriteMany PVC; potential contention |
| **Server-side worktree + rsync** | Server creates worktree, syncs to pod volume pre-launch, syncs back post-completion | No shared storage needed; adds latency; complex |
| **In-pod worktree creation** | Init container creates worktree; server reads results via K8s exec or shared volume | Decouples server from filesystem; git operations move to pod |
| **Git server sidecar** | Each pod has a git sidecar that handles worktree ops via API | Clean separation; most complex |

**Recommended:** Shared volume (PVC/NFS) for initial implementation. The wallfacer server and sandbox pods mount the same volume. Worktree creation and git operations happen from the server (as today). The pod sees the worktree as a regular directory.

---

## Implementation Plan

### Phase 1: Interface Extraction + Local Backend

1. Define `SandboxState`, `SandboxBackend`, and `SandboxHandle` in `internal/runner/backend.go`
2. Implement `LocalBackend` and `localHandle` wrapping `os/exec` with lifecycle state tracking
   - `Launch()` uses `cmd.Start()` (non-blocking) + stdout pipe → returns `localHandle`
   - `localHandle` tracks state transitions: Creating → Running → Streaming → Stopped/Failed
   - `localHandle.Kill()` runs `podman kill` + `podman rm -f`
   - `LocalBackend.List()` encapsulates the existing `podman ps --format json` parsing
3. Refactor `Runner` to use `SandboxBackend` instead of `ContainerExecutor`
4. Refactor `runContainer()` in `container.go` to use `SandboxHandle` for output streaming
5. Update `containerRegistry` to store handles (or at minimum delegate `Name()` and `Kill()`)
6. Retire `ContainerExecutor` interface and `osContainerExecutor`
7. All existing tests pass with `LocalBackend` — no behavior change

**Files touched:** `internal/runner/executor.go` (retire), `internal/runner/backend.go` (new), `internal/runner/backend_local.go` (new), `internal/runner/runner.go`, `internal/runner/container.go`, `internal/runner/execute.go`, `internal/runner/registry.go`

### Phase 2: Kubernetes Backend

1. Add `internal/runner/backend_k8s.go` implementing `SandboxBackend` via `client-go`
2. Map `ContainerSpec` → K8s Job spec
3. Implement `k8sHandle` with state tracking via pod watch
4. Implement log streaming via pod log follow API
5. Handle worktree mounting via shared PVC
6. Add `WALLFACER_SANDBOX_BACKEND` env var (`local` | `k8s`)
7. Integration tests with kind or minikube

**New dependency:** `k8s.io/client-go`

### Phase 3: Remote Docker Backend (optional)

1. Add `internal/runner/backend_remote.go` using Docker client SDK
2. SSH tunnel or TLS client cert for authentication
3. State tracking via Docker events API
4. Volume mounting via NFS or pre-provisioned volumes on the remote host

---

## Cross-Cutting Concerns

### Resource Limits

Currently set via `WALLFACER_CONTAINER_CPUS` and `WALLFACER_CONTAINER_MEMORY`, translated to `--cpus` and `--memory` CLI flags via `ContainerSpec.Build()`. For K8s, these map directly to `resources.limits`. No interface change needed — `ContainerSpec` already carries these fields; the backend interprets them.

### Sandbox Image Management

Currently checked via `podman images` / `docker images` in the handler. For K8s, images are pulled by the kubelet. The `GET /api/images` endpoint needs a backend-aware implementation:
- Local: check local image cache (as today)
- K8s: assume images are available (or check a registry)

### Network Policies

`WALLFACER_CONTAINER_NETWORK` sets `ContainerSpec.Network`. For local containers, this maps to `--network`. For K8s, this maps to NetworkPolicy resources. The field remains on `ContainerSpec`; its interpretation is backend-specific.

### Dependencies on Other Epics

- **Cloud Data Storage** (`cloud-data-storage.md`): If the store moves to a database, the sandbox executor doesn't need to share a filesystem for task metadata — only for worktrees. This simplifies shared volume requirements.
- **Multi-Tenant** (`cloud-multi-tenant.md`): The control plane decides which backend each user's instance uses. The sandbox executor just needs to support configuration injection at startup.
