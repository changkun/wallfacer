---
title: Add Pi harness
status: complete
depends_on:
  - specs/shared/harness-abstraction/claude-and-codex-migration.md
affects:
  - internal/harness/pi.go
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

# Add Pi harness

## Goal

Add the `pi` CLI (Armin Ronacher's Pi Coding Agent, earendil-works/pi, **not** Inflection's Pi chatbot) as a Tier-A harness.

## Reference docs

- [pi.dev/docs/latest](https://pi.dev/docs/latest)
- [github.com/earendil-works/pi](https://github.com/earendil-works/pi)

## Translation

| Canonical (`harness.Request`) | Pi argv |
|---|---|
| `Prompt` | positional message arg after flags |
| `Cwd` | process cwd (no flag; sessions are cwd-scoped) |
| `Model` | `--provider <name> --model <pattern>` (provider/model split, not slash-separated like OpenCode) |
| `SessionID` (non-empty) | `--session <id>` |
| `Permission = ReadOnly` | `--tools Read` |
| `Permission = Edit` | `--tools Read,Write,Edit` |
| `Permission = Full` | (no `--tools` flag; all 4 default tools enabled) |
| `SystemPrompt` | prepended into the prompt |
| Always | `-p --mode json` |

## Event mapping

Pi's `--mode json` emits JSONL of all internal events. Known events:

- session-start with `{session_id, provider, model}` to `KindSystemInit`
- assistant text to `KindAssistantText`
- tool start / end (Read, Write, Edit, Bash) to `KindToolCallStart` / `KindToolCallEnd`
- final result with `{usage, stop_reason}` to `KindResult`

`--mode rpc` (LF-delimited JSONL on stdin/stdout for embedding hosts) is documented as Pi's preferred embedding protocol. v1 uses `--mode json`; an upgrade to `--mode rpc` can come later if Pi's protocol stabilizes. `--mode json` is the lower-risk start.

## Auth

- `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / etc. read from env per Pi's provider list.
- `AuthConfig.PiAPIKey` reserved for a future Pi-specific subscription provider; not used in v1.
- Doctor runs `pi --version` and reports the installed providers via `pi providers list`.

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

1. **`internal/harness/pi.go`**: implement `Harness`. Registers as `harness.Pi`.
2. **`internal/harness/testdata/pi/`**: fixtures from a real `pi -p --mode json "list files"` run. Include one Bash-tool invocation to exercise the most complex event shape.
3. **`internal/envconfig/envconfig.go`**: no new env vars unless Pi-specific subscription auth lands.
4. **`internal/handler/config.go`**: surface `harness.Pi` in `/api/config.sandboxes`. The harness list here is NOT auto-derived from `harness.All()`. `availableSandboxes` hardcodes `add(harness.Claude); add(harness.Codex)`, and the `GetConfig` handler builds `sandboxes`/`sandbox_usable` from the same hardcoded `[]harness.ID{harness.Claude, harness.Codex}`. Extend these to enumerate the registered harnesses (drive the built-in list from `harness.All()`, or add `harness.Pi` to each hardcoded slice/map) so a newly registered harness actually reaches the UI.
5. **`internal/handler/env.go`**: settings-tab `/test` probe, `pi --version`.
6. **`frontend/src/components/TaskComposer.vue`**: add the Pi option to the per-task harness `<select>` and widen the `sandbox` ref union (currently `ref<'' | 'claude' | 'codex'>`) to include `'pi'`. Mirrors the existing Claude/Codex `<option>` rows.
7. **`frontend/src/components/settings/SettingsTabSandbox.vue`**: add the Pi block alongside Claude and Codex. Clarify in the UI that this is Pi, the earendil-works coding agent (not Inflection Pi). This disambiguation matters; users will get confused otherwise. This tab renders the per-harness settings driven by the `config.sandboxes` list.
8. **`docs/guide/configuration.md`**: document install, model selection (provider plus model two-flag form is unusual), and the disambiguation.

## Tests

- `pi_test.go`: argv shape with and without `--tools`, fixture parsing for all 4 default-tool event types, model/provider flag composition.
- `internal/handler/config_test.go`: assert `harness.Pi` appears in `availableSandboxes` and in the `GetConfig` `sandboxes`/`sandbox_usable` response once registered.
- Integration: skip if `pi` not on PATH.

## Acceptance criteria

- `make test` green.
- A task with `harness: pi` runs end-to-end against a real Pi install.
- `harness: pi` is selectable in the TaskComposer harness `<select>` and is returned by `/api/config`.
- The provider plus model two-flag form survives the round-trip from UI to task config to argv.

## Notes

- Pi sessions auto-save to `~/.pi/agent/sessions/` keyed by cwd. Wallfacer's per-task worktree means each task gets its own session storage automatically; no cleanup required.
- Pi's 4-tool minimal core (Read, Write, Edit, Bash) is intentional simplicity. `Permission = ReadOnly` mapping to `--tools Read` is the simplest correct interpretation.
- Pi supports `--mode rpc` for embedded hosts (LF-JSONL stdin/stdout). If Pi's stability story matures, a follow-up spec switches to it; it gives the orchestrator finer control over turn boundaries.

## Outcome

Shipped. `harness.Pi` (`internal/harness/pi.go`) owns argv, event parsing,
auth, and capabilities; wired into the host backend (`launchPi` in
`internal/executor/host_pi.go`), the config API, the TaskComposer selector,
the sandbox settings tab, and the docs.

Design evolution vs the draft, after checking the real installed `pi`
(v0.79.3) and its shipped docs:

- **Event schema corrected.** The draft's event names (`session-start`,
  a `result` event with `usage`/`stop_reason`) were guesses. Pi's real
  `--mode json` stream is `session` / `agent_start` / `turn_start` /
  `message_*` / `tool_execution_*` / `turn_end` / `agent_end`. Usage and
  `stopReason` live on each assistant message, so the terminal result is
  synthesized from the last assistant message in `agent_end`. `ParseEvent`
  maps the real schema; message `content` is decoded lazily because user
  content can be a bare string.
- **`-p` is boolean, prompt is positional.** Unlike Claude/Cursor's
  `-p <prompt>`, pi's prompt is the trailing positional arg.
- **Model two-flag split** is `--provider`/`--model`, cut on the first `/`
  of `Request.Model`; a bare value emits `--model` alone.
- **Executor wiring was required and added.** The end-to-end criterion is
  unreachable from the draft's `affects` list (the host backend hardcodes
  per-harness launch). `launchPi` mirrors `launchCursor`: native JSON
  stream (no last-message wrapping), Permission forced to Full so all four
  tools are enabled, instructions file contents prepended.
- **No new env vars.** Per the spec, `pi` is resolved from `$PATH`
  (`HostBackendConfig.PiBinary` exists for an override but is not fed from
  a `WALLFACER_HOST_PI_BINARY` key); provider keys are inherited from the
  process env, so `AuthEnv` returns nothing and `AuthConfig.PiAPIKey`
  stays reserved.
- **Config/env surfacing was free.** A sibling change made `/api/config`
  enumerate `harness.All()` and the `/test` probe validate via
  `IsValid()`, so a registered harness reaches the UI automatically.
- **Live run not yet verified.** A build-tag-gated e2e test ships
  (`pi_integration_test.go`, `-tags pi_integration`): it runs a real pi
  one-shot and asserts the adapter parses the stream into a terminal
  result, skipping when pi is absent or unauthenticated. It could not be
  run to completion locally (no provider credentials), so the unit
  fixture is built from pi's documented json / session-format schema
  rather than a live capture. Run the e2e test with provider keys set to
  confirm end-to-end.
