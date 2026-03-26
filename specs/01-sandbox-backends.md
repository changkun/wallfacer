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

## Status: Complete

All tasks in this milestone are done. The pluggable sandbox backend abstraction is fully implemented and all local container execution routes through it.

Remote backend implementations (K8s, remote Docker) and cloud worktree management are scoped under [M6: Cloud Backends](06-cloud-backends.md).
