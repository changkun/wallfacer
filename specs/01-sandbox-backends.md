# Pluggable Sandbox Backends

**Date:** 2026-03-23
**Updated:** 2026-03-26

## Already Implemented

- **Interfaces and types** (`internal/runner/backend.go`): `SandboxState` enum (6 states), `SandboxBackend` interface (`Launch`, `List`), `SandboxHandle` interface (`State`, `Stdout`, `Wait`, `Kill`, `Name`), `String()` method. Tests in `backend_test.go`.
- **`LocalBackend`** (`internal/runner/backend_local.go`): `Launch()` starts containers non-blocking via `cmd.Start()`, returns `localHandle` with atomic state tracking. `List()` shells out to `ps --format json` and handles both Podman JSON array and Docker NDJSON. Tests in `backend_local_test.go`.
- **Runner wiring** (`internal/runner/runner.go`): `Runner.backend` field initialized as `NewLocalBackend(r.command)` in `NewRunner()`. `ListContainers()` delegates to `r.backend.List()`. Old `executor` field retained temporarily for `container.go` and `ideate.go`.

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

- **`SandboxBackend`** (`internal/runner/backend.go`) — New abstraction. `Launch(ctx, spec) → (SandboxHandle, error)` and `List(ctx) → ([]ContainerInfo, error)`. `Runner.backend` field, initialized as `LocalBackend`.
- **`LocalBackend`** (`internal/runner/backend_local.go`) — Production implementation; `Launch()` starts non-blocking via `cmd.Start()`, `List()` shells out to `ps --format json`.
- **`ContainerExecutor`** (`internal/runner/executor.go`) — **Deprecated.** Still used by `container.go` and `ideate.go` for `RunArgs()` and `Kill()`. Will be removed in Task 7.
- **`osContainerExecutor`** — Legacy implementation; removes leftover containers, calls `cmdexec.Capture()`.
- **`ContainerSpec`** (`internal/runner/container_spec.go`) — Structured container description (Image, Name, Volumes, Env, Labels, Network, CPUs, Memory, Cmd, WorkDir, ExtraFlags). Its `Build()` method returns `[]string` CLI args.
- **`VolumeMount`** (`internal/runner/container_spec.go:10-15`) — Single bind mount or named volume descriptor (Host, Container, Options, Named).
- **`containerRegistry`** (`internal/runner/registry.go`) — `syncmap.Map[uuid.UUID, string]` tracking `taskID → containerName` for running containers.
- **`ContainerInfo`** (`internal/runner/runner.go`) — Runtime container metadata. `ListContainers()` now delegates to `r.backend.List()`.
- **Log streaming** — Container stdout captured by `cmdexec.Capture()` as a blocking call; live log streaming uses a separate `logpipe.Pipe` mechanism.
- **Circuit breaker** (`internal/pkg/circuitbreaker`) — Three-state breaker (closed/open/half-open) gating `runContainer()` calls; tracks consecutive failures via atomic CAS.

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

### Target Interfaces

```go
type SandboxState int

const (
    SandboxCreating  SandboxState = iota
    SandboxRunning
    SandboxStreaming
    SandboxStopping
    SandboxStopped
    SandboxFailed
)

type SandboxBackend interface {
    Launch(ctx context.Context, spec ContainerSpec) (SandboxHandle, error)
    List(ctx context.Context) ([]ContainerInfo, error)
}

type SandboxHandle interface {
    State() SandboxState
    Stdout() io.ReadCloser
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

### Task 5: Refactor `runContainer()` to use handle-based streaming

**Goal:** Replace the blocking `executor.RunArgs()` call with the non-blocking `backend.Launch()` + handle pattern. The `Runner` already has a `backend SandboxBackend` field (from Task 4); this task switches `runContainer()` and `ideate.go` from `r.executor` to `r.backend`.

**Work:**
1. In `container.go`, refactor `runContainer()`:
   - Replace `r.executor.RunArgs(ctx, name, args)` (line ~497) with `r.backend.Launch(ctx, spec)`
   - Read output from `handle.Stdout()` instead of parsing a byte slice
   - Call `handle.Wait()` to get exit code
   - Use `handle.State()` for circuit-breaker decisions: `SandboxFailed` at creation = runtime failure, `SandboxStopped` with non-zero exit = agent error
2. Update `parseOutput()` / `parseAgentOutput()` to accept `io.Reader` instead of `[]byte` (or adapt the bridge)
3. Update container kill path: replace `r.executor.Kill(name)` with `handle.Kill()`
4. Update circuit breaker integration to use handle state

**Files:** `internal/runner/container.go`, `internal/runner/output.go` (if output parsing changes), `internal/runner/container_test.go`

**Acceptance:** `runContainer()` uses handle-based streaming. Output parsing works from reader. Circuit breaker uses handle state. All tests pass.

---

### Task 6: Upgrade container registry to store handles

**Goal:** Replace `containerRegistry`'s `syncmap.Map[uuid.UUID, string]` with handle storage for richer state queries.

**Work:**
1. Change `containerRegistry` to map `uuid.UUID → SandboxHandle` (or a wrapper struct with both handle and name)
2. Update `Set()`, `Get()`, `Delete()` methods
3. `ContainerName(id)` delegates to `handle.Name()`
4. Kill by task ID now calls `handle.Kill()` directly instead of shelling out
5. Update all call sites in handler (container kill endpoint, task cancel, etc.)

**Files:** `internal/runner/registry.go`, `internal/runner/registry_test.go`, `internal/handler/containers.go`

**Acceptance:** Registry stores handles. Kill works through handle. All tests pass.

---

### Task 7: Retire `ContainerExecutor` interface

**Goal:** Remove the old abstraction now that `SandboxBackend` is fully wired.

**Work:**
1. Delete `ContainerExecutor` interface from `executor.go`
2. Delete `osContainerExecutor` implementation
3. Delete or migrate `MockContainerExecutor` in test files — replace with mock `SandboxBackend`
4. Remove any remaining references to the old interface
5. Clean up imports

**Files:** `internal/runner/executor.go` (delete or empty), `internal/runner/executor_mock_test.go` (delete or replace)

**Acceptance:** No references to `ContainerExecutor` remain. All tests pass with `SandboxBackend` mocks.

---

### Task 8: Unify log streaming through the handle

**Goal:** Remove the separate `logpipe.Pipe` mechanism and stream logs directly from the handle's `Stdout()`.

**Work:**
1. Audit how `logpipe.Pipe` is used for live log streaming to the UI (SSE `/api/tasks/{id}/logs`)
2. Replace logpipe with a tee or multi-reader on `handle.Stdout()` — output parsing and live log streaming read from the same source
3. Ensure SSE log streaming still works correctly with the new reader path
4. Remove logpipe if no longer needed

**Files:** `internal/runner/logpipe/` (audit/remove), `internal/handler/stream.go`, `internal/runner/container.go`

**Acceptance:** Live log streaming works through the handle's stdout. No separate pipe mechanism needed. SSE logs work correctly.

---

### Task 9: Backend selection via env var

**Goal:** Add `WALLFACER_SANDBOX_BACKEND` env var so the server can select between backends at startup.

**Work:**
1. Add `WALLFACER_SANDBOX_BACKEND` to `internal/envconfig/` (values: `local`, default: `local`)
2. In server startup, create the appropriate backend based on config
3. Add to `wallfacer doctor` output
4. Update docs: `CLAUDE.md`, `docs/guide/configuration.md`

**Files:** `internal/envconfig/envconfig.go`, `internal/cli/server.go`, `internal/cli/doctor.go`, docs

**Acceptance:** `WALLFACER_SANDBOX_BACKEND=local` works (only option for now). Doctor reports backend. Docs updated.

---

### Task 10: Kubernetes backend (future)

**Goal:** Implement `K8sBackend` for dispatching sandbox containers as K8s Jobs.

**Depends on:** Tasks 5–9 complete.

**Work:**
1. Add `internal/runner/backend_k8s.go` implementing `SandboxBackend` via `client-go`
2. Map `ContainerSpec` → K8s Job spec (see design table above)
3. Implement `k8sHandle` with state tracking via pod watch
4. Implement log streaming via pod log follow API
5. Handle worktree mounting via shared PVC (see Worktree Management section)
6. Add `k8s` as a value for `WALLFACER_SANDBOX_BACKEND`
7. Integration tests with kind or minikube

**New dependency:** `k8s.io/client-go`

This task is deliberately left as a single unit — it should be broken down further when work begins.

---

### Task 11: Remote Docker backend (optional, future)

**Goal:** Implement `RemoteDockerBackend` for SSH/HTTPS dispatch to a remote Docker host.

**Depends on:** Tasks 5–9 complete.

**Work:**
1. Add `internal/runner/backend_remote.go` using Docker client SDK
2. SSH tunnel or TLS client cert for authentication
3. State tracking via Docker events API
4. Volume mounting via NFS or pre-provisioned volumes on the remote host

Lower priority than K8s. Useful for simple single-host remote setups.

---

## Task Dependency Graph

```
Task 5 (refactor runContainer)
  └→ Task 6 (upgrade registry)
       └→ Task 7 (retire ContainerExecutor)
            └→ Task 8 (unify log streaming)
                 └→ Task 9 (env var backend selection)
                      └→ Task 10 (K8s backend)
                      └→ Task 11 (Remote Docker backend)
```

Tasks 5–9 are sequential. Tasks 10 and 11 can run in parallel after Task 9.

---

## Worktree Management in Cloud

The biggest architectural challenge for remote backends. Currently:

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

**Recommended:** Shared volume (PVC/NFS) for initial implementation. Design details deferred to Task 10.

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

- **Cloud Data Storage** (`02-storage-backends.md`): If the store moves to a database, the sandbox executor doesn't need to share a filesystem for task metadata — only for worktrees.
- **Multi-Tenant** (`08-cloud-multi-tenant.md`): The control plane decides which backend each user's instance uses.
