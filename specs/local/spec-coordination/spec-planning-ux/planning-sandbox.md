---
title: Planning Sandbox Lifecycle
status: complete
depends_on: []
affects:
  - internal/sandbox/
  - internal/runner/
  - internal/planner/
  - internal/handler/planning.go
  - internal/store/models.go
effort: large
created: 2026-03-30
updated: 2026-03-31
author: changkun
dispatched_task_id: null
---

# Planning Sandbox Lifecycle

## Design Problem

How does the planning sandbox container integrate with the existing sandbox backend infrastructure? The parent spec establishes that spec mode runs inside a long-lived sandbox with full read/write access to specs and read-only access to the full workspace. But the current sandbox system is designed around per-task ephemeral or worker containers — not a single persistent planning session that outlives individual tasks.

Key constraints:
- The planning container must survive across spec mode sessions (close/reopen the UI)
- The agent must read the full workspace but only write to `specs/`
- The sandbox operates directly on the workspace filesystem (no worktree isolation, per the concurrent edits decision)
- The container must integrate with existing `sandbox.Backend` and `sandbox.Handle` interfaces
- Container reuse model (worker containers) is the closest precedent, but planning has no "task" to key on

## Context

The existing sandbox architecture (`internal/sandbox/`) has two modes:
- **Ephemeral containers**: one per invocation, `--rm` cleanup
- **Worker containers**: long-lived per-task containers with `podman exec` for reuse

The runner (`internal/runner/`) manages container lifecycles keyed by task UUID. Planning has no task UUID — it's a workspace-scoped singleton. The runner also handles worktree setup, board context generation, and mount construction — most of which don't apply to planning.

The `ContainerSpec` in `internal/sandbox/spec.go` already supports arbitrary volume mounts, environment variables, and resource limits. The planning container would use a different mount configuration (full workspace read-only + specs read-write) than task containers (worktree read-write).

## Options

**Option A — New SandboxActivity + Runner extension.** Add `SandboxActivityPlanning` to the existing activity enum. The runner manages the planning container alongside task containers, using the workspace fingerprint as the key (instead of task UUID). Planning-specific mount logic lives in the runner.

- Pro: Reuses existing container lifecycle management, metrics, circuit breaker infrastructure. Usage attribution works via existing `UsageBreakdown` mechanism.
- Con: Bloats the runner with planning-specific logic. The runner's design assumes task-scoped lifecycles.

**Option B — Separate PlanningRunner.** A new `internal/planner/` package manages the planning sandbox independently. It uses the `sandbox.Backend` interface for container operations but has its own lifecycle (attach/detach model instead of start/stop per task).

- Pro: Clean separation. Planning lifecycle (long-lived, workspace-scoped) is fundamentally different from task lifecycle (turn-based, task-scoped). Each can evolve independently.
- Con: Duplicates some infrastructure (container spec building, mount logic, usage tracking). Two code paths for container management.

**Option C — Extend WorkerManager.** The existing `WorkerManager` interface manages long-lived per-task containers. Extend it with a planning-scoped variant that keys on workspace fingerprint. The planning "worker" stays alive across sessions.

- Pro: Minimal new code — piggybacks on the worker container infrastructure. The exec-based reuse model is exactly what planning needs.
- Con: Worker containers are designed for task isolation (per-task volumes). Planning needs different mount semantics. Overloading the worker concept may confuse the abstraction.

## Open Questions

1. Should the planning container use the same sandbox image as task containers (Claude Code sandbox), or a lighter-weight image optimized for spec editing (no build tools needed)?
2. How does the planning container interact with the circuit breaker? A planning container failure shouldn't count against the task execution circuit breaker.
3. When the workspace group changes, the planning container must be destroyed and recreated with new mounts. How does this interact with the global chat session that persists across sessions?
4. Should the planning container have resource limits different from task containers? Planning is mostly reading/writing markdown — it doesn't need build tool CPU/memory.
5. How are the read-only workspace mounts and read-write specs mount configured? Can the container runtime enforce write restriction to `specs/` only, or is this an agent-level convention?

## Affects

- `internal/sandbox/backend.go` — may need new methods or a separate manager interface for planning lifecycle
- `internal/sandbox/local.go` — planning container launch and attach logic
- `internal/sandbox/spec.go` — planning-specific `ContainerSpec` configuration (mount restrictions)
- `internal/runner/` — either extended with planning support or a new `internal/planner/` created alongside
- `internal/handler/` — new handler endpoints for entering/leaving spec mode, attaching to planning sandbox

## Design Decision

**Option B — Separate PlanningRunner** (`internal/planner/`). The planning lifecycle (long-lived, workspace-scoped, interactive) is fundamentally different from task lifecycle (turn-based, task-scoped, fire-and-forget). A separate package avoids bloating the runner with planning-specific logic while reusing the `sandbox.Backend` interface for container operations.

The planner uses the worker container pattern (long-lived container + `podman exec` per round) but keys on workspace fingerprint instead of task UUID. It follows the ideation singleton pattern for container registry management.

Mount configuration: full workspace read-only + `specs/` read-write override. Same sandbox image as task containers. Same resource limits initially (can be differentiated later via env config).

## Outcome

The planning sandbox lifecycle is fully implemented as `internal/planner/`, providing a workspace-scoped singleton container that delegates to the `sandbox.Backend` interface for cloud-portable container operations. Three API endpoints (`GET/POST/DELETE /api/planning`) control the sandbox, and the planner is integrated into server startup, shutdown, and workspace switching.

### What Shipped

- **`internal/planner/`** — 3 source files (`planner.go`, `spec.go`, `worker.go` removed in refactor) with container spec building, lifecycle management (Start/Stop/Exec/UpdateWorkspaces), and planning-specific mounts (workspace RO + specs/ RW override)
- **`internal/store/models.go`** — `SandboxActivityPlanning` and `TaskKindPlanning` constants
- **`internal/handler/planning.go`** — 3 HTTP endpoints: `GetPlanningStatus`, `StartPlanning`, `StopPlanning`
- **`internal/runner/`** — `SandboxBackend()` accessor on Runner interface and implementation
- **`internal/cli/server.go`** — planner creation, shutdown hook, workspace-switch wiring via `applySnapshot`
- **10 backend tests** across planner (9) and store (2) packages
- **6 handler tests** for the planning API endpoints

### Design Evolution

1. **Delegates to sandbox.Backend instead of direct cmdexec.** The original implementation managed containers directly via `os/exec` with custom `planningWorker`, `planningHandle`, and `planningExecHandle` types. This was refactored to delegate to `sandbox.Backend.Launch()` with a stable `wallfacer.task.id=planning-sandbox` label, so `LocalBackend` routes through its worker container mechanism automatically. This eliminates duplicated container lifecycle code and ensures K8s backends work without changes.

2. **SetPlanner method instead of constructor parameter.** The spec proposed passing the planner to `NewHandler`. Since the planner is created after the handler in the server initialization sequence, a `SetPlanner(*planner.Planner)` method was added instead, avoiding changes to the `NewHandler` signature and all its callers.

3. **Nil planner safety.** All handler endpoints gracefully handle `h.planner == nil` (status returns `running: false`, start returns 503, stop is a no-op). This means the planner is optional — the server works without it.

## Task Breakdown

| Child spec | Depends on | Effort | Status |
|------------|-----------|--------|--------|
| [Add SandboxActivityPlanning constant](planning-sandbox/planning-activity.md) | — | small | complete |
| [Create planner package with container lifecycle](planning-sandbox/planner-core.md) | planning-activity | large | complete |
| [Planning sandbox API endpoints](planning-sandbox/planning-api.md) | planner-core | medium | complete |
| [Wire planner into server lifecycle](planning-sandbox/planning-server-wiring.md) | planning-api | small | complete |

```mermaid
graph LR
  A[Planning activity constant] --> B[Planner core package]
  B --> C[Planning API endpoints]
  C --> D[Server lifecycle wiring]
```
