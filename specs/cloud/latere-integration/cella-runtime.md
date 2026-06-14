---
title: "Runtime Integration: Cella Backend"
status: drafted
depends_on:
  - specs/foundations/sandbox-backends.md
  - specs/identity/authentication.md
  - specs/identity/agent-token-exchange.md
  - specs/shared/harness-abstraction.md
affects:
  - internal/executor/
  - internal/runner/
  - internal/envconfig/
effort: large
created: 2026-05-30
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Runtime Integration: Cella Backend

## Problem

Wallfacer runs agents through one abstraction (`executor.Backend`) with a
single local implementation wired in at startup:

| Implementation | Where the agent runs |
|----------------|----------------------|
| `HostBackend` (`internal/executor/host.go`) | `claude`/`codex` process directly on the host |

`HostBackend` runs **on the user's machine** (tasks execute in git worktrees, no
containers). The runner hard-wires it: `NewRunner`
(`internal/runner/runner.go`) constructs `executor.NewHostBackend(...)` directly
with no executor-selection switch.

To execute tasks in the cloud, wallfacer needs a second backend that dispatches
to **Cella** (`latere.ai/x/sandbox`, cella.latere.ai) - Latere's hardened K8s
sandbox runtime - without wallfacer taking on any K8s scheduling, warm-pool,
quota, or egress-policy logic. Cella owns all of that and exposes a `Runtime`
interface plus a `/v1/sandboxes` REST API (the `sandbox-backends` foundation
already names this as the consumer boundary).

This spec defines the **runtime integration interface**: a `CellaBackend` that
implements the existing `executor.Backend` so the runner is unchanged, selected
by config so local mode is untouched.

## Goal

Add a `CellaBackend` implementing `executor.Backend`/`Handle`, selected via
`--executor cella`, that runs an agent turn inside a Cella sandbox and streams
its output back - with **zero change** to runner orchestration (worktrees,
output parsing, circuit breaker, kill routing, log streaming) and **zero
change** to `HostBackend`.

## Why this is the lead integration example

The runtime seam is the cleanest demonstration of the "consume, don't absorb"
principle: the `executor.Backend` interface already exists and the host
implementation already satisfies it. Adding a cloud runtime is "implement the
interface, add a selection point" - not new architecture. Every other seam (FS,
metadata) follows the same shape.

## Design

### Interface mapping

`CellaBackend` maps the local launch lifecycle onto Cella's API:

| `executor.Backend` / `Handle` | Cella operation |
|-------------------------------|-----------------|
| `Launch(ctx, ContainerSpec)` | `POST /v1/sandboxes` (create from spec) → `POST /v1/sandboxes/{id}/exec` (start the agent command) |
| `Handle.State()` | derived from sandbox/exec status (`Creating`→`Running`→`Streaming`→`Stopped`/`Failed`) |
| `Handle.Stdout()` / `Stderr()` | streamed exec output via the WebSocket attach (`/v1/sandboxes/{id}/attach`) or exec stream, split into the two readers the runner expects |
| `Handle.Wait()` | block until exec completion; return its exit code |
| `Handle.Kill()` | stop/delete the sandbox (`DELETE /v1/sandboxes/{id}`) |
| `Handle.Name()` | the Cella sandbox id |
| `Backend.List()` | `GET /v1/sandboxes` filtered to wallfacer-launched sandboxes |

`ContainerSpec` is already declarative (its `Build()` helper renders a host
command line; see `internal/executor/spec.go`), so `CellaBackend` maps spec
fields to Cella API fields directly rather than to CLI args.

### ContainerSpec → Cella mapping

- **Image / policy** → Cella image + execution policy (`GET/POST /v1/policies`).
  Cella enforces egress and hardening; wallfacer selects, does not implement.
- **Secrets** (tokens, API keys) → **not** raw env. Use Cella's credential vault
  (`POST /v1/credentials`, envelope-encrypted) so secrets never transit
  wallfacer plaintext env into the cloud.
- **Non-secret env** (model name, flags) → sandbox env.
- **Volume mounts (worktrees)** → the hard part (see below).

### The worktree problem (key open design question)

The host backend runs the agent directly against the worktree on disk. A Cella
sandbox has no access to the host filesystem, so the task's worktree must reach
the remote sandbox. Options, in preference order:

1. **FS Workspace API** - stage the worktree to fs.latere.ai hot tier, mount it
   into the Cella sandbox, write back on completion. Cleanest, but **blocked on
   FS Phase 5 (`/workspaces/*` not built yet)**. Owned by
   [tenant-filesystem.md](../tenant-filesystem.md).
2. **Git push/pull** - push the worktree branch to a remote the sandbox can
   clone; pull results back. Works today without FS, but heavier and assumes a
   remote.
3. **Cella durable workspace** - let Cella hold the workspace across exec calls.

This spec defines the backend contract; the worktree-sync mechanism is resolved
jointly with the FS integration and must not be reinvented here.

### Identity / per-task delegation

A Cella sandbox running on the user's behalf may need to call Latere services
(FS, telemetry) back. Mint a short-lived per-task token via RFC 8693 token
exchange (see [agent-token-exchange.md](../../identity/agent-token-exchange.md))
and hand it to the sandbox through the credential vault, not via the prompt or
plaintext env.

### Configuration & selection

- New env: `CELLA_URL` (base API URL); auth via the signed-in principal's token
  (cloud mode requires `WALLFACER_CLOUD=true` + Identity).
- **New work - there is no executor-selection switch today.** `NewRunner`
  (`internal/runner/runner.go`) hard-wires `executor.NewHostBackend(...)`; there
  is no `--backend`/`--executor` flag and no `case "host"`/`case "cella"`
  branch in the codebase. Selecting Cella therefore depends on the
  `Executor`/`--executor` interface defined in
  [shared/harness-abstraction.md](../../shared/harness-abstraction.md) (Layer 2,
  Executor), which introduces the executor-selection seam. Land that seam first;
  then this spec adds `cella` as a selectable executor (`--executor cella`),
  with `NewRunner` constructing `executor.NewCellaBackend(...)` instead of the
  host backend when selected.
- `internal/envconfig` learns `cella` as a valid executor value.
- `wallfacer doctor` reports Cella reachability + auth, mirroring how it reports
  host backend readiness.

### Data boundary

Cella execution **sends the worktree (source code) off the machine** - it
deliberately crosses the local-first trust boundary. This makes it a Phase 3+
capability, gated by demand and explicit opt-in, distinct from cloud metadata
coordination (which never sends code). Document this prominently in the UI
executor selector and in `docs/`.

## Acceptance criteria

- `CellaBackend` implements `executor.Backend` and `Handle`; the runner uses it
  through the same `r.backend.Launch()` path as the host backend, unmodified.
- `--executor cella` selects it; default (host) behavior is byte-identical to
  today. (Depends on the executor-selection seam from
  [harness-abstraction.md](../../shared/harness-abstraction.md) landing first.)
- A task runs end-to-end in a Cella sandbox (with the chosen worktree-sync
  mechanism), streams output live, commits results, and cleans up the sandbox on
  completion, cancel, and kill.
- `wallfacer doctor --executor cella` validates connectivity and auth.
- Unit tests with a mock Cella client cover launch, stream, wait, kill, and
  list; an opt-in E2E mirrors `e2e-lifecycle` against a real Cella endpoint.

## Boundaries

- Do **not** implement K8s scheduling, warm pools, quotas, egress policy, or
  sandbox hardening - Cella owns these.
- Do **not** modify `HostBackend` behavior.
- Do **not** reinvent worktree transport - consume the FS integration.
- Do **not** put secrets in env or prompts crossing the boundary - use Cella's
  credential vault.

## Open questions

- Which worktree-sync option ships first (FS vs git push/pull) given FS Phase 5
  timing?
- One exec per turn vs a long-lived sandbox across a task's turns?
