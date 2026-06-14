---
title: Add OpenCode harness
status: complete
depends_on:
  - specs/shared/harness-abstraction/claude-and-codex-migration.md
affects:
  - internal/harness/opencode.go
  - internal/envconfig/envconfig.go
  - internal/handler/config.go
  - internal/handler/env.go
  - frontend/src/components/TaskComposer.vue
  - frontend/src/components/settings/SettingsTabSandbox.vue
  - docs/guide/configuration.md
effort: medium
created: 2026-06-01
updated: 2026-06-15
author: changkun
dispatched_task_id: null
---

# Add OpenCode harness

## Goal

Add the `opencode` CLI as a Tier-A harness behind the `Harness` interface. Independent from [cursor](cursor.md); pick whichever lands first as the validation reference.

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
| `SystemPrompt` | prepended into the prompt; OpenCode has no `--append-system-prompt` |
| Always | `--format json` |

## Event mapping

OpenCode's JSON event schema is less standardized than Claude / Codex / Cursor. The adapter parses the events it knows and emits `KindUnknown` with `Raw` populated for the rest; wallfacer's event store already tolerates this. Known events:

- session-start with `{session_id, model}` to `KindSystemInit`
- assistant text chunks to `KindAssistantText`
- tool invocation start/end to `KindToolCallStart` / `KindToolCallEnd`
- final completion with usage to `KindResult`

Usage extraction: token counts when present; cost not surfaced reliably (`EmitsCost = false`).

## Auth

OpenCode handles auth via `opencode auth login` per provider (Anthropic, OpenAI, OpenRouter, etc.). Auth state lives in OpenCode's own config; wallfacer does not manage individual provider keys for OpenCode. Doctor checks that at least one provider is configured by running `opencode auth list --format json` and parsing the result.

Headless server mode (`opencode serve`) needs `OPENCODE_SERVER_PASSWORD`, kept on `AuthConfig.OpenCodeServerPassword` so a later spec can add the `opencode run --attach` warm-start path.

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

1. **`internal/harness/opencode.go`**: implement `Harness`. Registers as `harness.OpenCode`.
2. **`internal/harness/testdata/opencode/`**: fixtures, one full `opencode run "..."` headless invocation with `--format json`. Capture both a successful and a tool-call-heavy run.
3. **`internal/envconfig/envconfig.go`**: read `OPENCODE_SERVER_PASSWORD`.
4. **`internal/handler/config.go`**: surface `harness.OpenCode` in `/api/config.sandboxes`. The harness list here is NOT auto-derived from `harness.All()`. `availableSandboxes` hardcodes `add(harness.Claude); add(harness.Codex)`, and the `GetConfig` handler builds `sandboxes`/`sandbox_usable` from the same hardcoded `[]harness.ID{harness.Claude, harness.Codex}`. Extend these to enumerate the registered harnesses (drive the built-in list from `harness.All()`, or add `harness.OpenCode` to each hardcoded slice/map) so a newly registered harness actually reaches the UI.
5. **`internal/handler/env.go`**: settings-tab `/test` probe, `opencode --version` plus at least one provider in `opencode auth list`.
6. **`frontend/src/components/TaskComposer.vue`**: add the OpenCode option to the per-task harness `<select>` and widen the `sandbox` ref union (currently `ref<'' | 'claude' | 'codex'>`) to include `'opencode'`. Mirrors the existing Claude/Codex `<option>` rows.
7. **`frontend/src/components/settings/SettingsTabSandbox.vue`**: add the OpenCode block alongside Claude and Codex. Note in the UI that OpenCode manages provider auth itself, so no API key is needed in `.env`. This tab renders the per-harness settings driven by the `config.sandboxes` list.
8. **`docs/guide/configuration.md`**: document setup, install `opencode`, run `opencode auth login`, select a provider; wallfacer dispatches via `opencode run`.

## Tests

- `opencode_test.go`: argv shape per `Permission` mode, fixture-based event parsing, graceful `KindUnknown` for unrecognized events.
- `internal/handler/config_test.go`: assert `harness.OpenCode` appears in `availableSandboxes` and in the `GetConfig` `sandboxes`/`sandbox_usable` response once registered.
- Integration: skip if `opencode` not on PATH. When present, run `opencode run "echo hi"` and assert `KindResult` arrives.

## Acceptance criteria

- `make test` green.
- A task with `harness: opencode` runs end-to-end and commits, assuming the user has `opencode auth login` completed.
- `harness: opencode` is selectable in the TaskComposer harness `<select>` and is returned by `/api/config`.
- The settings tab clearly distinguishes OpenCode's "auth handled by the harness" UX from Claude/Codex/Cursor's "API key in .env" UX.

## Out of scope

- `opencode serve` warm-start (`--attach`). Significant speedup for short tasks but adds a daemon lifecycle. Deferred to a follow-up spec.
- Per-task MCP config injection. Defer until users ask; OpenCode reads project-level config from `AGENTS.md` and `~/.config/opencode/` which is good enough for v1.

## Outcome

Shipped (validated against the installed `opencode` v1.17.7 and a live one-shot e2e):

- **Harness** (`internal/harness/opencode.go`). `BuildArgv` emits `opencode run --format json [--dir] [--model] [--session]` plus the permission mapping; `ParseEvent` maps opencode's NDJSON `emit()` events (`text` / `reasoning` / `tool_use` / `step_start` / `error`) to canonical events and the launcher-synthesized `result` line to `KindResult`; `AuthEnv` surfaces only `OPENCODE_SERVER_PASSWORD`. Fixtures under `testdata/opencode/`.
- **Executor** (`internal/executor/host_opencode.go`). opencode emits **no terminal result event** (the run loop just breaks on idle), so `launchOpenCode` tees stdout, aggregates the final text + token usage from `text`/`step_finish`, and appends a synthesized `{"type":"result"}` line — mirroring the codex `--output-last-message` path. A schema-drift guard marks the result an error if bytes flowed but no recognized event parsed.
- **Wiring.** `WALLFACER_HOST_OPENCODE_BINARY` and `OPENCODE_SERVER_PASSWORD` read in envconfig; binary threaded through `RunnerConfig` and the server; host backend dispatch/binary cases added.
- **Surfacing.** `config.go` is registry-driven (landed by the cursor sibling), so registering the harness surfaced it in `/api/config` automatically; added `config_test.go` assertions. Frontend: OpenCode option in `TaskComposer.vue` and an OpenCode block in `SettingsTabSandbox.vue` ("auth handled by the CLI; no key in `.env`").
- **Docs** in `configuration.md`. Gated live e2e (`opencode_integration` tag) plus an `opencode` lane in `scripts/e2e-lifecycle.sh`.

Divergences from the spec as written:

- **Permission mapping.** The spec's `--mode build|plan` does not exist in opencode 1.17. Actual: `--agent plan` for `ReadOnly`, `--dangerously-skip-permissions` for `Edit`/`Full` (default build agent). The launcher forces `Full` so headless edits apply.
- **`config.go` change was unnecessary.** The cursor migration already made the sandbox list registry-driven, so item 4 was satisfied by registration alone.
- **`env.go` `/test` probe unchanged.** Rather than a separate `opencode --version` + `opencode auth list` probe, the existing smoke-task `/test` path is reused (consistent with claude/codex/cursor); opencode validates as a registered harness and launches through the executor.
- **Executor wiring added** (`host_opencode.go` + host backend), which the original `affects` list omitted but acceptance criterion 2 (end-to-end) required.
