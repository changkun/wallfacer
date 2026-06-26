---
title: Topos as a Remote Agent Executor
status: stale
depends_on:
  - specs/cloud/latere-integration.md
  - specs/cloud/latere-integration/cella-runtime.md
  - specs/identity/authentication.md
  - specs/shared/harness-abstraction.md
affects:
  - internal/executor/
  - internal/runner/
  - internal/handler/config.go
  - internal/handler/env.go
  - internal/cli/server.go
  - docs/cloud/
effort: large
created: 2026-06-01
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Topos as a Remote Agent Executor

## Problem

Wallfacer today executes every task locally — even in cloud mode, the harness runs on the user's machine. [Latere Topos](https://topos.latere.ai) (internal label: `agents`, located at `latere.ai/agents`) is Latere's managed agent-workspace product, exposing a `/v1/agents` control plane that runs coding agents remotely. Wallfacer should be able to dispatch a task to Topos instead of running it locally, so users can offload long-running agents to managed infrastructure without leaving the wallfacer board.

## Layering

The [harness-abstraction.md](../../shared/harness-abstraction.md) spec separates two concerns:

- **Harness** (`internal/harness/`) — which CLI to spawn and how to parse its events.
- **Executor** (existing `sandbox.Backend` interface, narrowed) — where the harness process runs.

Topos integration is an **Executor**, not a Harness. The remote side already runs whichever harness Topos provides (Claude / Codex / etc.); wallfacer's job is to dispatch the request, stream events back, and surface them in the board.

## Decision

Add `internal/executor/topos.go` implementing the executor interface against the Topos `/v1/agents` HTTP API. Selectable via `--executor topos` (replacing the now-removed `--backend` flag from [host-default.md](../../shared/host-default.md), in the executor dimension only).

Topos integration depends on Latere's platform pieces:

- **Cella** runs the underlying sandbox on the Topos side. Wallfacer does not talk to Cella directly when using Topos — Topos owns that coupling. The [cella-runtime](cella-runtime.md) spec handles the *direct* Cella path; this spec handles the *via-Topos* path. They are parallel runtime options.
- **Latere auth** (Identity) provides the principal that Topos authorizes. Wallfacer reuses the existing OIDC token from [authentication.md](../../identity/authentication.md); Topos validates it against the same Latere issuer.

## Shape

### CLI

```sh
wallfacer run --executor topos
```

Plus env config:

```
TOPOS_BASE_URL=https://topos.latere.ai
# Auth: reuses the same Latere session cookie / bearer token established by `wallfacer auth login`.
# No separate TOPOS_API_KEY in v1.
```

### Executor interface (defined in [harness-abstraction.md](../../shared/harness-abstraction.md))

```go
type Executor interface {
    Launch(ctx, argv []string, env map[string]string, cwd string, stdin io.Reader) (Handle, error)
}

type Handle interface {
    Stdout() <-chan []byte      // NDJSON lines from the remote harness
    Wait() error
    Cancel() error
}
```

`TopozExecutor.Launch` translates to:

1. `POST /v1/agents` — create a remote agent run. Body includes the canonical harness ID, the request (prompt, model, session id), and workspace files (initially: pushed via Latere FS once it exists; v1 may require the workspace to be a remote-clonable git repo).
2. `GET /v1/agent/{id}/events` (Server-Sent Events or streaming JSONL) — stream events from the remote harness. Each event is already in the canonical `harness.Event` shape (Topos normalizes server-side) so the runner's downstream consumers don't need to know remote vs local.
3. `DELETE /v1/agent/{id}` on `Handle.Cancel`.

### Auth flow

- User runs `wallfacer auth login` — establishes a Latere session (already implemented).
- `TopozExecutor` reads the saved token from `<UserConfigDir>/latere/token.json` (same store Identity owns).
- Every Topos API call attaches `Authorization: Bearer <token>`.
- On 401, surface a clear "please re-run `wallfacer auth login`" message rather than retrying silently.

### Workspace handling

Per-task workspace transport is the hardest piece. Options:

| Approach | Pros | Cons |
|---|---|---|
| **A. Git push to a remote** | Reuses existing git infra; Topos clones | Requires the workspace to be a git repo with a writable remote |
| **B. Latere FS upload** | Works for any cwd; aligns with [tenant-filesystem.md](../tenant-filesystem.md) | FS Workspace API not yet shipped |
| **C. Topos pulls from local via reverse tunnel** | No upload needed | New protocol; tunnel infra |

**v1 picks A** — requires the workspace to be a git repo with a remote wallfacer can push to (the user's GitHub fork, say). Topos clones, runs the harness, pushes results back to a branch. The user reviews the resulting PR / branch as usual. Option B is the longer-term path once FS Workspace API ships.

### Failure modes

- Remote disconnect during streaming → reconnect with `Last-Event-ID` header; if Topos can't resume, surface as a `KindError` and let the runner classify failure normally.
- Quota exhaustion (HTTP 429) → surface to UI as a non-retryable failure with a clear message.
- Token expiry mid-run → cancel run, surface auth-needed error.

## Scope

### What this spec includes

- `internal/executor/topos.go` implementing `Executor`.
- `TOPOS_BASE_URL` env var; wired into config UI.
- `--executor` CLI flag (replaces the local `--backend` removed by [host-default](../../shared/host-default.md)).
- Settings UI surface to enable Topos as the default executor for new tasks.
- E2E test against a Topos staging endpoint (or a recorded fake when staging is unavailable).
- `docs/cloud/topos.md` — user-facing guide.

### What this spec excludes

- The Topos server-side `/v1/agents` API contract — owned by the `latere.ai/agents` repo. This spec consumes whatever Topos exposes; if the API changes, this spec follows.
- Direct Cella dispatch — that's [cella-runtime](cella-runtime.md). They are parallel paths and can coexist.
- Per-task selection of executor — v1 is process-wide via `--executor`. Per-task selection can be a follow-up once the basic path works.
- Migrating existing local task state into Topos — out of scope; users opt in to Topos for new tasks.

## Tests

- Unit tests against a `httptest.Server` mocking the Topos endpoints; verify request bodies, header injection, SSE consumption, cancel semantics, 401 / 429 handling.
- Integration test gated on `TOPOS_BASE_URL` + auth being available; runs a real one-shot task end-to-end.
- Auth-token expiry test: simulate 401 mid-stream, verify clean failure with the right error message.

## Open Questions

- Does Topos already normalize harness events server-side, or does the client need to parse harness-native NDJSON? If the latter, the client reuses `harness.ParseEvent` — clean reuse of the same abstraction.
- Workspace transport: do we hard-block v1 on having git-repo workspaces, or fall back to a "best-effort prompt-only" mode for non-git cwds? Lean toward hard-block — silent feature degradation is worse than a clear error.
- Per-tenant rate limits — should wallfacer self-throttle, or let Topos return 429 and react? Lean toward reactive; self-throttling is premature optimization.

## Why not a "Topos Harness" instead

A `harness.Topos` would be wrong: Topos doesn't replace the harness, it replaces the *executor*. Topos itself runs `claude` or `codex` (or whatever harness wallfacer asked for) on the remote side. Modeling it as a harness would force every Topos call to round-trip through a fake `claude` shim, losing the per-harness event fidelity for no gain.
