---
title: Add Cursor harness
status: drafted
depends_on:
  - specs/shared/harness-abstraction/claude-and-codex-migration.md
affects:
  - internal/harness/cursor.go
  - internal/envconfig/envconfig.go
  - internal/handler/config.go
  - internal/handler/env.go
  - frontend/src/components/TaskComposer.vue
  - frontend/src/components/settings/SettingsTabSandbox.vue
  - docs/guide/configuration.md
effort: medium
created: 2026-06-01
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Add Cursor harness

## Goal

Add `cursor-agent` as the third Tier-A harness. This is the validation case for the abstraction in [interface](interface.md). If Cursor doesn't fit cleanly in a single new file, the interface needs revision before OpenCode and Pi are added.

## Reference docs

- [Cursor CLI headless mode](https://cursor.com/docs/cli/headless)
- [Cursor CLI parameters](https://cursor.com/docs/cli/reference/parameters)
- [Cursor CLI output-format](https://cursor.com/docs/cli/reference/output-format)

## Translation

| Canonical (`harness.Request`) | Cursor argv |
|---|---|
| `Prompt` | `-p "<prompt>"` |
| `Cwd` | `--workspace <path>` |
| `Model` | `--model <name>` |
| `SessionID` (non-empty) | `--resume <id>` |
| `Permission = Full` (headless write) | `--force --trust --approve-mcps` |
| `Permission = Edit` | `--mode agent` (default; write proposed but applied with `--force`. Without `--force` edits are only proposed, so document that headless requires `--force`) |
| `Permission = ReadOnly` | `--mode plan` |
| `SystemPrompt` | prepended into `-p` value (no native flag) |
| `MCPServers` | `--mcp-config <tmpfile>` (write a temp JSON) |
| Always | `--output-format stream-json` |
| Always | `--sandbox enabled` (matches Cursor's default OS-sandbox) |

## Event mapping

Cursor NDJSON events map to the canonical `Event`:

| Cursor event | Canonical `EventKind` |
|---|---|
| `{type:"system", subtype:"init", session_id, model, cwd, apiKeySource}` | `KindSystemInit` |
| `{type:"user", ...}` | `KindUserResult` |
| `{type:"assistant", content:[{type:"text", text}]}` | `KindAssistantText` |
| `{type:"<X>ToolCall", subtype:"started", ...}` | `KindToolCallStart` |
| `{type:"<X>ToolCall", subtype:"completed", ...}` | `KindToolCallEnd` |
| `{type:"result", subtype:"success"|"error_*", durations, session_id, request_id}` | `KindResult` |

Usage extraction: Cursor surfaces tokens on the terminal `result` event but not always with cache breakdown. Populate what's present, leave the rest zero.

## Auth

- `CURSOR_API_KEY` env var (added to `AuthConfig.CursorAPIKey`).
- `cursor-agent login` is interactive; we don't trigger it. Doctor instructs the user.

## Capabilities

```go
Capabilities{
    SupportsResume:       true,
    SupportsMCP:          true,
    SupportsSystemPrompt: false,  // no --append-system-prompt; we prepend
    EmitsUsage:           true,
    EmitsCost:            false,  // not surfaced in result event
    NeedsTTY:             false,
}
```

## What to do

1. **`internal/harness/cursor.go`**: implement `Harness`. Registers as `harness.Cursor` in `init()`.
2. **`internal/harness/testdata/cursor/`**: fixtures, one full headless run (system, assistant text deltas, tool call started/completed, result). Captured from a real `cursor-agent -p "list files" --output-format stream-json --force` run.
3. **`internal/envconfig/envconfig.go`**: read `CURSOR_API_KEY` into `AuthConfig.CursorAPIKey`.
4. **`internal/handler/config.go`**: surface `harness.Cursor` in `/api/config.sandboxes`. The harness list here is NOT auto-derived from `harness.All()`. `availableSandboxes` hardcodes `add(harness.Claude); add(harness.Codex)`, and the `GetConfig` handler builds `sandboxes`/`sandbox_usable` from the same hardcoded `[]harness.ID{harness.Claude, harness.Codex}`. Extend these to enumerate the registered harnesses (drive the built-in list from `harness.All()`, or add `harness.Cursor` to each hardcoded slice/map) so a newly registered harness actually reaches the UI.
5. **`internal/handler/env.go`**: wire the settings-tab `/test` probe, `cursor-agent --version` returning successfully plus auth env present.
6. **`frontend/src/components/TaskComposer.vue`**: add the Cursor option to the per-task harness `<select>` and widen the `sandbox` ref union (currently `ref<'' | 'claude' | 'codex'>`) to include `'cursor'`. Mirrors the existing Claude/Codex `<option>` rows.
7. **`frontend/src/components/settings/SettingsTabSandbox.vue`**: add the Cursor credential/test block (API key field, **Test** button) alongside the Claude and Codex blocks. This tab renders the per-harness settings driven by the `config.sandboxes` list.
8. **`docs/guide/configuration.md`**: document `CURSOR_API_KEY` and `cursor-agent` as a supported harness.
9. **`wallfacer doctor`**: already enumerates installed harnesses if the harness registry drives it; verify Cursor appears.

## Tests

- `cursor_test.go`: argv for every flag combination, fixture-based event parsing for at least 5 event subtypes, auth env wiring.
- `internal/handler/config_test.go`: assert `harness.Cursor` appears in `availableSandboxes` and in the `GetConfig` `sandboxes`/`sandbox_usable` response once registered.
- Integration: skip if `cursor-agent` not on PATH (`testing.Short()` or build tag). When present, run a one-shot `-p "echo hi"` and assert it yields a `KindResult` event with non-empty `SessionID`.

## Acceptance criteria

- `make test` green.
- A task created with `harness: cursor` runs end-to-end against a real `cursor-agent` install, completes with a commit.
- `harness: cursor` is selectable in the TaskComposer harness `<select>` and is returned by `/api/config`.
- No code change outside `internal/harness/cursor.go`, env config, `internal/handler/config.go`, the TaskComposer and SettingsTabSandbox harness UI, and docs.

## Notes

- `--force` is mandatory for headless edits; without it Cursor only *proposes* edits and exits without writing. This is documented at [cursor.com/docs/cli/headless](https://cursor.com/docs/cli/headless). The harness must inject it whenever `Permission` is `Edit` or `Full`.
- Cursor's `--worktree` flag is not used. Wallfacer's worktree-per-task model already handles isolation.
- Cursor supports `--stream-partial-output` for character-level deltas; out of scope for v1.
