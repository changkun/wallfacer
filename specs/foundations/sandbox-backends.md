---
title: Pluggable Sandbox Backends
status: complete
depends_on: []
affects:
  - internal/sandbox/
effort: large
created: 2026-03-23
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Pluggable Sandbox Backends

## Problem

The runner executed all sandbox containers through a `ContainerExecutor` interface that passed raw CLI `args []string` to `os/exec`. This had two consequences:

1. **No lifecycle management.** Containers were launched as blocking calls — the runner could not observe intermediate states (starting, streaming, stopping, crashed) without shelling out to `podman ps`.

2. **Hard-coupled to local runtime.** Cloud deployment requires dispatching to K8s Jobs, cloud VMs, or remote Docker hosts. These backends need structured input (`ContainerSpec`), not CLI arg slices.

## Strategy

Replace `ContainerExecutor` with a single `Backend` interface that accepts a `ContainerSpec` and returns a stateful `Handle`. The interface is intentionally minimal — two methods on `Backend`, six on `Handle` — so that adding a new backend (K8s, remote Docker, native sandbox) requires implementing only these methods. All orchestration logic (worktrees, output parsing, circuit breaker, kill routing, log streaming) stays in the runner.

The extraction was done incrementally: interfaces first, then `LocalBackend`, then runner wiring, then handle-based streaming in each caller, then registry upgrade, then executor retirement, then full package extraction. Each step was independently testable and deployable.

## Design

### Lifecycle States

```
Creating → Running → Streaming → Stopping → Stopped
                 ↘                           ↗
                   ────────→ Failed ←────────
```

| State | Meaning | Set by |
|-------|---------|--------|
| `Creating` | Provisioning (image pull, pod scheduling, process fork) | `Backend.Launch()` |
| `Running` | Process alive, no output yet | Backend, after runtime confirms start |
| `Streaming` | Alive, output being read | Runner, after first read |
| `Stopping` | `Kill()` called, awaiting exit | Backend, inside `Kill()` |
| `Stopped` | Exited (any code). Terminal. | Backend, after `Wait()` returns |
| `Failed` | Could not create or crashed before output. Terminal. | Backend, on launch/runtime error |

### Interfaces

```go
// package sandbox (internal/sandbox/)

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

### Key Decisions

- **Stdout and Stderr are separate streams.** Merging via `io.MultiReader` was tried but causes back-pressure deadlocks when SSE clients aren't connected. The runner reads them sequentially; SSE log streaming uses `podman logs -f` independently.
- **No Suspend/Resume.** The agent CLI is one-shot. Container reuse ([container-reuse.md](container-reuse.md)) can add `podman pause`/`unpause` as an internal `LocalBackend` optimization.
- **Timeout is task-level.** The runner enforces `task.Timeout` and calls `handle.Kill()`. The backend does not have its own timeout.
- **`ContainerSpec` is declarative, `Build()` is local-only.** `ContainerSpec` describes what to run. `Build()` converts it to podman/docker CLI args — a concern of `LocalBackend`, not the interface. Future K8s backends map `ContainerSpec` fields to Job specs directly.
- **Log streaming stays decoupled.** SSE endpoints spawn `podman logs -f` via `logpipe` rather than tee-ing `handle.Stdout()`. This avoids back-pressure, supports late-joining clients with `--tail`, and keeps output parsing independent from streaming.
- **Backend selection via env var.** `WALLFACER_SANDBOX_BACKEND` (default: `local`) parsed in `envconfig`, selected in `NewRunner()`, reported by `wallfacer doctor`. Future backends add values to this switch.

## Outcome

### Package Structure

```
internal/sandbox/
  sandbox.go      — Type enum (Claude, Codex) — pre-existing
  backend.go      — Backend, Handle, BackendState, ContainerInfo
  spec.go         — ContainerSpec, VolumeMount, Build()
  local.go        — LocalBackend, localHandle (podman/docker via os/exec)
  parse.go        — ParseContainerList, IsUUID (JSON format handling)
```

### Runner Integration

All container callers in `internal/runner/` use `r.backend.Launch()`:
- `runContainer()` — task implementation and test execution
- `RunIdeation()` — brainstorm agent
- `GenerateTitle()` — title generation
- `runRefinementContainer()` — prompt refinement
- `generateCommitMessage()` — commit message generation

Kill methods (`KillContainer`, `KillRefineContainer`, `KillIdeateContainer`) route through `handle.Kill()` via the container registry. The registry stores `containerEntry{name, handle, logReader}` — the handle is set after successful `Launch()`.

### What Was Removed

- `ContainerExecutor` interface and `osContainerExecutor` implementation (`executor.go` — deleted)
- `Runner.executor` field
- `MockContainerExecutor` — replaced by `MockSandboxBackend`
- All direct `cmdexec.Capture()` calls for container execution
- All `cmdexec.New(r.command, "kill", name).Run()` fallbacks in kill methods

## Design Evolution

The spec originated on 2026-03-23 as **"Cloud Sandbox Executor"** — part of a cloud deployment epic alongside multi-tenant and cloud data storage specs. The initial design proposed three backends (`LocalBackend`, `K8sBackend`, `RemoteDockerBackend`), a three-phase implementation plan, and extensive cloud worktree management via shared PVC/NFS. Several aspects changed during implementation:

1. **Scope narrowed.** Only Phase 1 (interface extraction + `LocalBackend`) shipped. K8s and remote Docker backends were deferred to cloud deployment.

2. **Separate stdout/stderr.** The initial design specified a single `Stdout() io.ReadCloser` returning combined output. Implementation split into `Stdout()` and `Stderr()` after `io.MultiReader` caused back-pressure deadlocks when SSE clients weren't connected.

3. **Dedicated package.** The initial design placed all types in `internal/runner/`. Implementation extracted them to `internal/sandbox/` for cleaner separation from orchestration logic.

4. **Shorter names.** `SandboxBackend` → `Backend`, `SandboxHandle` → `Handle`, `SandboxState` → `BackendState`. The `sandbox` package name already provides context.

5. **`Build()` made explicitly local-only.** The spec noted this conceptually; the implementation codified it as a key decision — `ContainerSpec` is declarative, `Build()` is a `LocalBackend` concern only.

6. **Log streaming kept decoupled.** The initial design implied unifying output parsing and live streaming via the handle. Implementation kept SSE endpoints using `podman logs -f` via `logpipe` independently, avoiding back-pressure and supporting late-joining clients.

7. **Cloud worktree management dropped.** Described in the original as "the biggest architectural challenge," it was deferred entirely to cloud deployment.

## Future Work

Remote backend implementations (K8s, remote Docker) and cloud worktree management are scoped under [Cloud Backends](../cloud/cloud-backends.md).
