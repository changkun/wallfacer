---
title: Google Antigravity as a Remote Executor
status: drafted
depends_on:
  - specs/shared/harness-abstraction.md
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
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Google Antigravity as a Remote Executor

## Problem

Google ships [Antigravity](https://antigravity.google), an agentic developer platform with a desktop IDE, CLI, and — most relevant here — an **Interactions API** on Gemini (`POST /v1beta/interactions` with `agent: "antigravity-preview-..."`). For users with a Google Cloud / Gemini API account, wallfacer should be able to dispatch a coding task to Antigravity instead of running the harness locally. This widens the set of execution backends beyond Anthropic / OpenAI / Latere and matches users who already have Gemini billing set up.

## Shape

Antigravity is a **self-contained executor**, same shape as [claude-managed-agents](claude-managed-agents.md): the harness is the Antigravity agent harness, the model is fixed (Gemini 3.5 Flash on the managed Interactions API), and tool capabilities are configurable but the agent loop itself is not user-replaceable.

This is the third executor category in the harness-abstraction layering:

| Category | Examples | Harness selectable | Implements |
|---|---|---|---|
| Harness-running | host (default), Topos | yes (Claude Code, Codex, Cursor, …) | `Executor` + composes a `Harness` |
| Self-contained, third-party | Claude Managed Agents, Antigravity | no (fixed) | `Executor`, ignores `Harness` |

## Selection

```sh
wallfacer run --executor antigravity
```

Plus env:

```
GEMINI_API_KEY=...
ANTIGRAVITY_AGENT_VERSION=antigravity-preview-05-2026   # default; overridable for newer previews
ANTIGRAVITY_ENVIRONMENT=remote                          # remote (fresh sandbox) or env_<id> (reuse)
```

## API translation

| Wallfacer concept | Antigravity API |
|---|---|
| Per-task run | `POST /v1beta/interactions` with `agent`, `input`, `environment` |
| Sandbox reuse across turns | Pass the same `environment: env_<id>` value returned by the first call |
| Send feedback / next turn | Follow-up `POST /v1beta/interactions` reusing `environment` |
| Tool restriction | `tools` array in request body (default: code execution + search + URL fetch) |
| System instructions | Mounted markdown skill files (see workspace transport below) |
| Stream output | SSE on the same endpoint via `Accept: text/event-stream` |
| Auth | `x-goog-api-key: $GEMINI_API_KEY`, revision header `x-goog-api-version: 2026-05-20` |

## Event mapping

Antigravity's SSE event taxonomy is **not fully documented in public sources** as of writing — the spec captures what's documented and degrades gracefully on the rest.

| Antigravity event | Canonical `EventKind` |
|---|---|
| `interaction.started` | `KindSystemInit` |
| `interaction.output_text` (deltas) | `KindAssistantText` |
| `interaction.tool_call.started` | `KindToolCallStart` |
| `interaction.tool_call.completed` | `KindToolCallEnd` |
| `interaction.completed` (with usage) | `KindResult` |
| anything else | `KindUnknown` with `Raw` populated |

The implementation must tolerate event-schema evolution: log unknown event types at info level, never panic, and document the matrix in `docs/cloud/antigravity.md` so users can self-diagnose drift.

## Workspace transport

Antigravity's Interactions API has **no first-class repo or worktree primitive**. The sandbox is an isolated Linux box; bootstrap is whatever the agent can do with its built-in tools (code execution, URL fetch). Two options:

| Approach | Pros | Cons |
|---|---|---|
| **A. Agent `git clone`s inside the sandbox** | No upload step; works for any public-clonable repo. | Requires the workspace to be a git repo with a remote the sandbox can reach; private repos need a token injection. |
| **B. Pre-step: wallfacer pushes worktree to a temp branch on the user's remote, agent clones that** | Works for any local worktree state. | Adds a push step; pollutes the remote with temp branches. |

**v1 picks A** — same trade as the [topos-remote-executor](latere-integration/topos-remote-executor.md). Workspace must be a git repo with a reachable remote. System instructions (workspace `AGENTS.md`) are sent as a mounted skill file via the Antigravity skill-files mechanism.

## Auth

- `GEMINI_API_KEY` from `~/.wallfacer/.env`.
- For private repos, a `GIT_HTTPS_TOKEN` env (already supported elsewhere in wallfacer) is injected into the sandbox via the request body's environment-variable pass-through (if Antigravity supports it; otherwise document as a v1 limitation).
- 401 → "Gemini API key missing or lacks Antigravity preview access" with docs link.

## Capabilities

```go
Capabilities{
    SupportsResume:       true,    // via environment reuse
    SupportsMCP:          false,   // not documented; no MCP equivalent surfaced via the API
    SupportsSystemPrompt: true,    // via mounted skill files
    EmitsUsage:           true,
    EmitsCost:            false,   // Gemini token pricing surfaced via billing, not the event stream
    NeedsTTY:             false,
}
```

## Scope

### What this spec includes

- `internal/executor/antigravity.go` implementing the executor.
- SSE consumption with graceful unknown-event handling.
- Environment ID lifecycle (create on first turn, reuse for resume, no explicit delete since the API doesn't document one — environments are reclaimed by Google after inactivity).
- `--executor antigravity` CLI selection.
- Settings UI surface and `wallfacer doctor` checks.
- `docs/cloud/antigravity.md` user guide with the harness/model fixed-list disclaimer.

### What this spec excludes

- Antigravity SDK (self-hosted on EC2 / Vertex / on-prem) — significant scope; defer to a follow-up.
- Antigravity CLI integration — the CLI is an end-user IDE replacement, not an embedding target.
- Multi-folder Project support (desktop-app concept) — Interactions API doesn't expose Projects.
- Per-task model override — model is fixed by the managed API.
- Sampling parameter override (temperature, top_p, etc.) — explicitly unsupported by the managed API (returns 400).

## Risks

| Risk | Mitigation |
|---|---|
| Preview API breaks | `ANTIGRAVITY_AGENT_VERSION` env-overridable; pinned CI integration test; "supported preview versions" matrix in docs. |
| Lifecycle verbs (cancel, delete) undocumented | Surface long-running tasks as best-effort; expose a "session abandoned" UX when wallfacer can't actively cancel. Revisit when Google documents the verbs. |
| Event taxonomy drift | `KindUnknown` fallback; structured logging of unknown types so the schema can be filled in over time. |
| No MCP / no first-class skill injection | Workspace `AGENTS.md` is the only system-prompt vehicle; document the constraint vs Claude Managed Agents' richer tool/MCP surface. |
| Workspace must be a git repo with a reachable remote | Doctor check; clear error message; mirrors Topos v1 constraint. |

## Open Questions

- Does Antigravity's Interactions API support env-var pass-through into the sandbox for private-repo auth? If not, v1 supports only public-clonable repos and the docs are explicit. Resolve during implementation by reading the latest API reference.
- Does any Antigravity event surface token usage in a parseable shape, or is usage only via Gemini billing? If the latter, `EmitsUsage` becomes `false`. Resolve during implementation; default to optimistic `true` and fall back if needed.
- Is the SDK path (self-hosted Antigravity on user infra) interesting enough to justify a follow-up spec? Probably yes once one of the managed paths ships and a user asks.

## Why a separate spec from Claude Managed Agents

Both are self-contained executors, but the tenant infra, auth, harness, model, and capability matrix all differ. Bundling would mean a single spec with two parallel narratives — harder to review, harder to dispatch. Same rationale as splitting [topos-remote-executor](latere-integration/topos-remote-executor.md) from [claude-managed-agents](claude-managed-agents.md).
