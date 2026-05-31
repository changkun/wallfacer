---
title: Add OpenCode harness
status: drafted
depends_on:
  - specs/shared/harness-abstraction/claude-and-codex-migration.md
affects:
  - internal/harness/opencode.go
  - internal/envconfig/envconfig.go
  - internal/handler/env.go
  - ui/partials/settings-tab-sandbox.html
  - docs/guide/configuration.md
effort: medium
created: 2026-06-01
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Add OpenCode harness

## Goal

Add the `opencode` CLI as a Tier-A harness behind the `Harness` interface. Independent from [cursor](cursor.md) — pick whichever lands first as the validation reference.

## Reference docs

- [OpenCode CLI docs](https://opencode.ai/docs/cli)

## Translation

| Canonical (`harness.Request`) | OpenCode argv |
|---|---|
| (subcommand) | `opencode run` (one-shot headless) |
| `Prompt` | positional arg after `run` |
| `Cwd` | `--dir <path>` |
| `Model` | `--model <provider/model>` |
| `SessionID` (non-empty) | `--session <id>` |
| `Permission = Full \| Edit` | `--mode build` |
| `Permission = ReadOnly` | `--mode plan` |
| `SystemPrompt` | prepended into the prompt — OpenCode has no `--append-system-prompt` |
| Always | `--format json` |

## Event mapping

OpenCode's JSON event schema is less standardized than Claude / Codex / Cursor. The adapter parses the events it knows and emits `KindUnknown` with `Raw` populated for the rest — wallfacer's event store already tolerates this. Known events:

- session-start with `{session_id, model}` → `KindSystemInit`
- assistant text chunks → `KindAssistantText`
- tool invocation start/end → `KindToolCallStart` / `KindToolCallEnd`
- final completion with usage → `KindResult`

Usage extraction: token counts when present; cost not surfaced reliably (`EmitsCost = false`).

## Auth

OpenCode handles auth via `opencode auth login` per provider (Anthropic, OpenAI, OpenRouter, etc.). Auth state lives in OpenCode's own config — wallfacer does not manage individual provider keys for OpenCode. Doctor checks that at least one provider is configured by running `opencode auth list --format json` and parsing the result.

Headless server mode (`opencode serve`) needs `OPENCODE_SERVER_PASSWORD` — kept on `AuthConfig.OpenCodeServerPassword` so a later spec can add the `opencode run --attach` warm-start path.

## Capabilities

```go
Capabilities{
    SupportsResume:       true,
    SupportsMCP:          true,
    SupportsSystemPrompt: false,
    EmitsUsage:           true,
    EmitsCost:            false,
    NeedsTTY:             false,
}
```

## What to do

1. **`internal/harness/opencode.go`** — implement `Harness`. Registers as `harness.OpenCode`.
2. **`internal/harness/testdata/opencode/`** — fixtures: one full `opencode run "..."` headless invocation with `--format json`. Capture both a successful and a tool-call-heavy run.
3. **`internal/envconfig/envconfig.go`** — read `OPENCODE_SERVER_PASSWORD`.
4. **`internal/handler/env.go`** — settings-tab `/test` probe: `opencode --version` + at least one provider in `opencode auth list`.
5. **`ui/partials/settings-tab-sandbox.html`** — add OpenCode row. Note in the UI: "OpenCode manages provider auth itself — no API key needed in `.env`."
6. **`docs/guide/configuration.md`** — document setup: install `opencode`, run `opencode auth login`, select a provider; wallfacer dispatches via `opencode run`.

## Tests

- `opencode_test.go`: argv shape per `Permission` mode, fixture-based event parsing, graceful `KindUnknown` for unrecognized events.
- Integration: skip if `opencode` not on PATH. When present, run `opencode run "echo hi"` and assert `KindResult` arrives.

## Acceptance criteria

- `make test` green.
- A task with `harness: opencode` runs end-to-end and commits, assuming the user has `opencode auth login` completed.
- The settings tab clearly distinguishes OpenCode's "auth handled by the harness" UX from Claude/Codex/Cursor's "API key in .env" UX.

## Out of scope

- `opencode serve` warm-start (`--attach`) — significant speedup for short tasks but adds a daemon lifecycle. Deferred to a follow-up spec.
- Per-task MCP config injection — defer until users ask; OpenCode reads project-level config from `AGENTS.md` and `~/.config/opencode/` which is good enough for v1.
