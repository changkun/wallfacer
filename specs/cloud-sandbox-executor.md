# Cloud Sandbox Executor

**Date:** 2026-03-23

## Problem

The wallfacer runner executes all sandbox containers via `os/exec` calling a local `podman`/`docker` binary (`internal/runner/executor.go`). This hard-couples the server to a host with a container runtime installed. For cloud deployment, the server needs to dispatch sandbox containers to remote execution backends — Kubernetes Jobs, cloud VM pools, or remote Docker hosts — without changing the task lifecycle or runner logic.

## Current Architecture

```
Runner.Run()
  → buildContainerArgsForSandbox()  → constructs CLI args for `podman run`
  → executor.RunArgs()              → os/exec: runs `podman run`, captures stdout/stderr
  → executor.Kill()                 → os/exec: runs `podman kill` + `podman rm`
```

Key interfaces and types involved:

- **`ContainerExecutor`** (`internal/runner/executor.go:11-17`) — The abstraction point. Two methods: `RunArgs(ctx, name, args) → (stdout, stderr, err)` and `Kill(name)`.
- **`osContainerExecutor`** — Production implementation; calls the container binary via `cmdexec`.
- **`ContainerSpec`** (`internal/runner/spec.go`) — Structured representation of a container invocation (image, volumes, env, labels, network, cmd). Its `Build()` method returns `[]string` CLI args.
- **`containerRegistry`** (`internal/runner/registry.go`) — `sync.Map` tracking `taskID → containerName` for running containers.
- **Log streaming** — Container stdout is piped through `cmdexec.Capture()` and parsed line-by-line for agent output JSON.

## Design: Pluggable Sandbox Backends

### New Interface

The current `ContainerExecutor` interface passes raw CLI `args []string`, which leaks the `podman run` abstraction. A cloud-native backend needs structured input, not CLI args. Replace it with a higher-level interface:

```go
// SandboxBackend abstracts where and how sandbox containers run.
type SandboxBackend interface {
    // Launch starts a container from the given spec and returns a handle.
    // The handle streams output and allows cancellation.
    Launch(ctx context.Context, spec ContainerSpec) (SandboxHandle, error)

    // List returns currently running sandboxes managed by this backend.
    List(ctx context.Context) ([]ContainerInfo, error)
}

// SandboxHandle represents a running sandbox container.
type SandboxHandle interface {
    // Stdout returns a reader for the container's combined stdout/stderr.
    Stdout() io.ReadCloser

    // Wait blocks until the container exits and returns its exit code.
    Wait() (exitCode int, err error)

    // Kill forcibly stops the container.
    Kill() error

    // Name returns the container's unique identifier (container name, pod name, etc.).
    Name() string
}
```

### Backend Implementations

#### 1. Local Backend (wraps existing `osContainerExecutor`)

Preserves current behavior. `ContainerSpec.Build()` produces CLI args, `os/exec` runs them.

```go
type LocalBackend struct {
    command string // "podman" or "docker"
}
```

No behavior change from today. This is the default.

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

**Log streaming:** `k8s.io/client-go` pod log stream (replaces `os/exec` pipe).

**Workspace mounting:**
- Option A: PersistentVolumeClaim per user, pre-populated with cloned repos
- Option B: Init container that clones repos on Job start (slower but simpler)
- Option C: NFS/EFS shared volume with per-user subdirectories

**Worktree challenge:** Git worktrees use absolute paths in their `.git` file, which reference the main repo's `.git/worktrees/<name>/` directory. In K8s:
- The wallfacer server creates worktrees on its own filesystem
- These paths don't exist inside the Job pod
- **Solution:** Create worktrees inside the shared volume (PVC), or use an init container that creates the worktree inside the pod before the agent starts

#### 3. Remote Docker Backend

SSH tunnel or Docker API over HTTPS to a remote Docker host.

```go
type RemoteDockerBackend struct {
    client *docker.Client // docker client SDK pointing at remote host
}
```

Useful for simple setups where a beefy VM runs all containers. Lower complexity than K8s but limited to single-host scaling.

---

## Runner Changes

### Replace `executor` with `backend`

In `internal/runner/runner.go`:

```go
type Runner struct {
    // ...
    backend SandboxBackend // replaces executor ContainerExecutor
    // ...
}
```

### Refactor `runContainer` flow

Current flow calls `executor.RunArgs()` which blocks until the container exits. The new flow:

```go
func (r *Runner) runContainer(ctx context.Context, spec ContainerSpec) (*agentOutput, error) {
    handle, err := r.backend.Launch(ctx, spec)
    if err != nil {
        return nil, err
    }

    // Register for lookup by task ID
    r.taskContainers.Set(taskID, handle.Name())
    defer r.taskContainers.Delete(taskID)

    // Stream and parse output (same logic as today, but reads from handle.Stdout())
    output := r.parseAgentOutput(handle.Stdout())

    exitCode, err := handle.Wait()
    // ... handle exit code, errors, etc.
}
```

### Container listing

`ListContainers()` currently shells out to `podman ps --format json`. Replace with `backend.List()`.

### Circuit breaker

The circuit breaker (`containerCB`) wraps `backend.Launch()` — no change needed, just moves up one level.

---

## Worktree Management in Cloud

The biggest architectural challenge. Currently:

1. `Runner.EnsureTaskWorktrees()` creates worktrees at `~/.wallfacer/worktrees/<task-uuid>/`
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

### Phase 1: Interface Extraction

1. Define `SandboxBackend` and `SandboxHandle` interfaces in `internal/runner/`
2. Implement `LocalBackend` wrapping the existing `osContainerExecutor`
3. Refactor `Runner` to use `SandboxBackend` instead of `ContainerExecutor`
4. Refactor `runContainer` to use `SandboxHandle` for output streaming
5. All existing tests pass with `LocalBackend` — no behavior change

**Files touched:** `internal/runner/executor.go`, `internal/runner/runner.go`, `internal/runner/container.go`, `internal/runner/run.go`

### Phase 2: Kubernetes Backend

1. Add `internal/runner/backend_k8s.go` implementing `SandboxBackend` via `client-go`
2. Map `ContainerSpec` → K8s Job spec
3. Implement log streaming via pod log API
4. Handle worktree mounting via shared PVC
5. Add `WALLFACER_SANDBOX_BACKEND` env var (`local` | `k8s`)
6. Integration tests with kind or minikube

**New dependency:** `k8s.io/client-go`

### Phase 3: Remote Docker Backend (optional)

1. Add `internal/runner/backend_remote.go` using Docker client SDK
2. SSH tunnel or TLS client cert for authentication
3. Volume mounting via NFS or pre-provisioned volumes on the remote host

---

## Cross-Cutting Concerns

### Resource Limits

Currently set via `WALLFACER_CONTAINER_CPUS` and `WALLFACER_CONTAINER_MEMORY`, translated to `--cpus` and `--memory` CLI flags. For K8s, these map directly to `resources.limits`. No interface change needed — `ContainerSpec` already carries these fields.

### Sandbox Image Management

Currently checked via `podman images` / `docker images`. For K8s, images are pulled by the kubelet. The `GET /api/images` endpoint needs a backend-aware implementation:
- Local: check local image cache (as today)
- K8s: assume images are available (or check a registry)

### Network Policies

`WALLFACER_CONTAINER_NETWORK` sets `--network` for local containers. For K8s, this maps to NetworkPolicy resources. The `ContainerSpec.Network` field should remain, but its interpretation is backend-specific.

### Dependencies on Other Epics

- **Cloud Data Storage** (`cloud-data-storage.md`): If the store moves to a database, the sandbox executor doesn't need to share a filesystem for task metadata — only for worktrees. This simplifies the shared volume requirements.
- **Multi-Tenant** (`cloud-multi-tenant.md`): The control plane decides which backend each user's instance uses. The sandbox executor just needs to support configuration injection at startup.
