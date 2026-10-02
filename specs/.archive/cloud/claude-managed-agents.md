---
title: Claude Managed Agents as a Remote Executor
status: archived
depends_on:
  - specs/.archive/shared/harness-abstraction.md
affects:
  - internal/executor/
  - internal/runner/
  - internal/handler/config.go
  - internal/handler/env.go
  - internal/envconfig/envconfig.go
  - internal/cli/server.go
  - docs/cloud/
effort: large
created: 2026-06-01
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Claude Managed Agents as a Remote Executor

> **Archived 2026-10-02. Retired as: outdated, and outside what wallfacer
> is becoming.** This spec dispatches a task to another vendor's hosted
> agents. The maintainer decided the same day that wallfacer runs one
> harness, on the Latere platform for a remote run
> ([platform-native](../../shared/platform-native.md)), and named this spec as
> no longer important. Remote execution is the
> [hosted executor](../../cloud/latere-integration/topos-remote-executor.md) alone.
>
> The text below is kept as written for the record. Its comparison with the
> Latere executor reflects that spec as it stood on 2026-10-02.

## Problem

Anthropic ships a managed agent platform ([platform.claude.com/docs/en/managed-agents](https://platform.claude.com/docs/en/managed-agents/overview)) where agents run on Anthropic-hosted (or self-hosted) sandboxes, with state, files, and event history persisted server-side. Wallfacer should be able to dispatch a task to Managed Agents so users with an Anthropic API account can offload long-running runs without operating their own infrastructure — and so wallfacer becomes a control plane that targets the most natural Anthropic-side execution surface.

## Shape

Managed Agents is a **self-contained executor**: the harness is the Managed Agents harness itself, not a CLI wallfacer spawns. The Latere [hosted executor](../../cloud/latere-integration/topos-remote-executor.md) has the same shape: a remote service runs its own agent loop, and wallfacer dispatches a request and follows its events. For Managed Agents, the harness is fixed; the model and tool catalog are selectable per agent version.

Implication for [harness-abstraction.md](../shared/harness-abstraction.md): the `Executor` interface must be high-level enough that some executors short-circuit `Harness.BuildArgv` and dispatch a `harness.Request` directly to a remote API instead of running argv. The host executor composes `Executor` with a `Harness`; the Managed Agents executor *is* both. The Latere hosted executor meets the same problem and does not extend `executor.Backend`: it supplies a session-shaped run to the runner's `driveToposRun` seam, which already carries typed events, a final result, and the task state machine. Evaluate that seam for this executor before adding a dispatch method to `Backend`.

## Selection

```sh
wallfacer run --executor claude-managed-agents
```

Plus env:

```
ANTHROPIC_API_KEY=sk-ant-...
ANTHROPIC_MANAGED_AGENTS_BETA=managed-agents-2026-04-01   # default; overridable for newer betas
CLAUDE_MANAGED_AGENTS_ENVIRONMENT=cloud                    # cloud | self-hosted
CLAUDE_MANAGED_AGENTS_SELF_HOSTED_URL=                     # if environment=self-hosted
```

## API translation

| Wallfacer concept | Managed Agents API |
|---|---|
| Per-task run | `POST /v1/sessions` (one session per task), bound to an agent + environment |
| Agent definition (system prompt, tools, MCP, model) | `POST /v1/agents` — created once per wallfacer install, versioned, cached by hash of (system prompt + tools + model) |
| Sandbox | `POST /v1/environments` — `cloud` (Anthropic-hosted) or `self-hosted` (user's worktree mount) |
| Send prompt / feedback | `POST /v1/sessions/{id}/events` (`user.message`) |
| Stream output | `GET /v1/sessions/{id}/stream` (SSE) |
| Cancel | `DELETE /v1/sessions/{id}` |
| Resume | Same session id — server retains state |

Auth headers on every call: `x-api-key`, `anthropic-version: 2023-06-01`, `anthropic-beta: managed-agents-2026-04-01`.

## Event mapping

SSE event types → canonical `harness.Event`:

| Managed Agents event | Canonical `EventKind` |
|---|---|
| session.created | `KindSystemInit` |
| agent.message (content blocks: text) | `KindAssistantText` |
| agent.tool_use start | `KindToolCallStart` |
| agent.tool_use result | `KindToolCallEnd` |
| user.message (echo) | `KindUserResult` |
| session.status_idle | `KindResult` (with stop_reason=idle) |
| error | `KindError` |

Usage extraction: token counts on `session.status_idle`; cost computed client-side from model + token counts (Managed Agents pricing is just Anthropic API token pricing + sandbox compute, which is surfaced separately by Anthropic billing — out of scope for in-task cost attribution v1).

## Workspace transport

Two paths matching Managed Agents' two sandbox modes:

| Mode | Approach | Use when |
|---|---|---|
| **Self-hosted sandbox** | Wallfacer runs the sandbox container on the user's host (or any reachable VM) with the worktree pre-mounted. The remote `POST /v1/environments` references the self-hosted endpoint. | Default — keeps worktree on the user's machine; behaves like a smarter local executor. |
| **Cloud sandbox** | Agent `git clone`s from inside the Anthropic-hosted sandbox; worktree never leaves Anthropic. | When the workspace is a public-clonable git repo and the user accepts that code runs on Anthropic infra. |

v1 ships **self-hosted sandbox only** — it's the path that matches existing wallfacer behavior (worktree on host) and the only path where in-progress edits stream back into the local explorer. Cloud-sandbox mode is a follow-up spec.

## Auth

- `ANTHROPIC_API_KEY` from `~/.wallfacer/.env` (already supported for Claude Code; reused here).
- Beta header value managed via env var (above) so users can opt into newer beta revisions without a wallfacer release.
- 401 → surface "API key expired or missing managed-agents access" with a docs link.

## Capabilities

The self-contained executor declares its `Capabilities` like a harness would:

```go
Capabilities{
    SupportsResume:       true,    // server-persisted sessions
    SupportsMCP:          true,    // first-class
    SupportsSystemPrompt: true,    // via agent definition
    EmitsUsage:           true,
    EmitsCost:            false,   // not in event stream; via billing API
    NeedsTTY:             false,
}
```

## Scope

### What this spec includes

- `internal/executor/claude_managed_agents.go` implementing the executor.
- Agent-definition caching (hash-keyed reuse of `/v1/agents` resources).
- SSE consumption with reconnect on transient failure.
- Self-hosted sandbox container lifecycle (this is the one place wallfacer keeps a "run a container" code path post-[host-default](../shared/host-default.md) — but it's the Managed Agents sandbox image from Anthropic, not wallfacer's own agent image).
- `--executor claude-managed-agents` CLI selection.
- Settings UI surface and `wallfacer doctor` checks.
- `docs/cloud/claude-managed-agents.md` user guide.

### What this spec excludes

- Cloud-sandbox mode (server-side git clone, no local worktree) — follow-up.
- Multimodal input (images, attachments) — uses Anthropic Files API; deferred.
- Per-task selection of executor — process-wide via `--executor` in v1. The Latere hosted executor selects per task; aligning the two is a follow-up.
- ZDR / HIPAA compatibility — Managed Agents is ineligible per Anthropic; document the constraint and refuse to run if a future `WALLFACER_ZDR=true` flag is set.

## Risks

| Risk | Mitigation |
|---|---|
| Beta API breaks | Beta header is env-overridable; ship a "supported beta versions" matrix in docs; CI integration test pinned to the supported version. |
| Self-hosted sandbox image drift | Pin the Anthropic-published sandbox image tag; surface mismatch in doctor. |
| Long sessions accumulate state cost | Surface `session.status_idle` events as a "task done" signal; auto-`DELETE` the session after wallfacer commits the result. |
| Rate limits (300/min create, 600/min read per org) | Self-throttle; surface 429 as a backoff with retry-after honored. |

## Open Questions

- Should wallfacer reuse a single long-lived "wallfacer agent" definition across tasks, or one per (system prompt + tools) hash? Lean toward hash-keyed reuse — agent definitions are cheap to create and reuse maximizes Anthropic-side caching.
- If a task's sub-agent needs to call Latere services, what does the Managed Agents sandbox present? The family answer is the dispatching user's own token, audienced to the service being called; auth mints no credential that stands for a user. Confirm the sandbox can receive one before this ships.
- Does this replace the [oauth-token-setup](../local/oauth-token-setup.md) Claude path for users who pick Managed Agents? No — Managed Agents needs a billing-capable API key, not an OAuth subscription token. Document the distinction clearly.

## Why a separate spec from the Latere hosted executor

Both are remote executors, but they have different shapes:

| Dimension | Latere hosted executor | Claude Managed Agents |
|---|---|---|
| Harness selectable | No (the platform's own loop; a mode of wallfacer's native harness) | No (fixed Managed Agents harness) |
| Tenant infra | Latere | Anthropic |
| Auth | Latere session | Anthropic API key |
| Workspace transport v1 | A task branch pushed to the platform's git host; the session's branch fetched back | Self-hosted sandbox mount |
| Model | Any model of the platform's catalog | Anthropic models |
| Composes with `harness.Harness` | No | No (replaces it) |

Bundling would conflate two unrelated integrations with different code paths, auth, and product positions.
