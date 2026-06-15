---
title: Add Cursor harness
status: archived
depends_on:
  - specs/shared/harness-abstraction/claude-and-codex-migration.md
affects:
  - internal/harness/cursor.go
  - internal/harness/testdata/cursor/headless-run.ndjson
  - internal/envconfig/envconfig.go
  - internal/executor/host.go
  - internal/executor/host_cursor.go
  - internal/runner/runner.go
  - internal/cli/server.go
  - internal/cli/doctor.go
  - internal/handler/config.go
  - internal/handler/env.go
  - frontend/src/components/TaskComposer.vue
  - frontend/src/components/settings/SettingsTabSandbox.vue
  - frontend/src/api/types.ts
  - docs/guide/configuration.md
effort: medium
created: 2026-06-01
updated: 2026-06-15
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

## Outcome

Implemented directly (not dispatched) on 2026-06-15. Commits `dc90bdf9` → `e30044ea`.

**Validation result (the spec's Goal).** The `Harness` adapter itself fits in one file
(`cursor.go`, ~250 lines incl. argv, parsing, MCP, auth, capabilities). But the interface
was **not** sufficient on its own: registering a harness does not propagate, because the
launch dispatch, binary resolution, config surfaces, and doctor all hardcoded the
`{claude, codex}` pair. Adding Cursor therefore required edits beyond the spec's `affects`
list (`executor/host.go` + `host_cursor.go`, `runner.go`, `cli/server.go`, `cli/doctor.go`).
`config.go` was changed to drive the built-in list from `harness.All()`, so OpenCode and Pi
do not reopen that file. The interface shape (Request/Event/Capabilities/AuthEnv) needed no
revision; the gap was the un-abstracted call sites around it.

**Acceptance criteria.**
- `make test` — backend `go build ./...` and `golangci-lint run ./...` green; every touched
  package green in isolation. (An unrelated concurrent auth-by-default session left
  `internal/handler/TestStartOAuth_ReturnsAuthorizeURL` flaky under the full parallel suite;
  it passes in isolation and is not caused by this work.)
- TaskComposer `<select>` + `/api/config` — done; `config.go` and the composer ref union
  include `cursor`.
- End-to-end against real `cursor-agent` — verified at adapter + executor level: a live
  one-shot parses to a terminal `KindResult` with a session id (build-tagged integration
  test `internal/harness/cursor_integration_test.go`), and `launchCursor` is unit-tested for
  `--force` injection and instructions-contents prepend. A full server task-to-commit run was
  not exercised.
- "No code change outside [list]" — **not met, deliberately.** It contradicts the end-to-end
  criterion (the host launcher hardcodes the harness switch), so it was treated as a spec
  defect; see the validation result above.

**Design evolution / surprises.**
- `requestFromClaudeSpec` leaves `Permission` at its `ReadOnly` zero value and sets
  `SystemPrompt` to the instructions-file *path*. Cursor is the first harness to read
  `Permission`, and (like Codex) has no append-system-prompt flag. `launchCursor` therefore
  forces `Permission = Full` and swaps the path for the file contents.
- `Permission = ReadOnly` (`--mode plan`) does **not** clear cursor's workspace-trust gate on
  a fresh directory — a headless run there exits asking for `--trust`. v1 always launches with
  Full, which emits `--force --trust --approve-mcps`, so this is moot in production; noted for
  a future ReadOnly/plan path.
- The captured wire format differs from the translation table in this spec: assistant text is
  nested under `message.content[]` (not top-level `content`), tool calls use
  `type:"tool_call"` with `subtype`, and usage keys are camelCase
  (`inputTokens`/`cacheReadTokens`/`cacheWriteTokens`). The fixture is a real capture.
- Live-CLI integration tests are gated behind `//go:build cursor_integration`, not
  `testing.Short()`: `make test-backend` runs `go test ./...` without `-short`, so a Short
  guard would fire a paid cursor-agent call on every run.

**Beyond the spec (UI).** The user asked to make the keys configurable, so `CURSOR_API_KEY`
was wired through the env GET/PUT/test endpoints (`env.go`, `api/types.ts`) and a Cursor
credential block (key field + Test button) was added to `SettingsTabSandbox.vue`.
