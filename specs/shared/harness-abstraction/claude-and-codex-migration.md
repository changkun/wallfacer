---
title: Migrate Claude and Codex onto the Harness interface
status: drafted
depends_on:
  - specs/shared/harness-abstraction/interface.md
affects:
  - internal/harness/claude.go
  - internal/harness/codex.go
  - internal/sandbox/host.go
  - internal/sandbox/host_codex.go
  - internal/runner/agent.go
  - internal/runner/container.go
  - internal/runner/execute.go
  - internal/envconfig/envconfig.go
  - internal/handler/config.go
  - internal/handler/env.go
effort: large
created: 2026-06-01
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Migrate Claude and Codex onto the Harness interface

## Goal

Move every Claude- and Codex-specific decision out of `internal/sandbox/host*.go` and the runner into `internal/harness/claude.go` and `internal/harness/codex.go`, behind the `Harness` interface from [interface](interface.md). Behavior-preserving — the existing runner suite is the regression gate.

## Current state to migrate

The following Claude / Codex specifics are scattered today:

| Concern | Today's location |
|---|---|
| Claude argv (`-p`, `--verbose --output-format stream-json`, `--resume`, `--model`, `--append-system-prompt`) | `internal/sandbox/host.go` + `internal/runner/agent.go` |
| Codex argv translation (`exec --full-auto --sandbox workspace-write --skip-git-repo-check --json --output-last-message ...`) | `internal/sandbox/host_codex.go` |
| Codex event-stream → Claude-result synthesis | `internal/sandbox/host_codex.go` |
| Claude NDJSON parsing (system / assistant / user / result records, usage extraction) | `internal/runner/parse.go` |
| Auth env wiring (`ANTHROPIC_API_KEY`, `CLAUDE_CODE_OAUTH_TOKEN`, codex auth file) | `internal/envconfig/envconfig.go` + handler config |
| Per-harness "is configured" probe (settings-tab test button) | `internal/handler/env.go` |

## What to do

### Step 1 — Implement `harness.Claude`

`internal/harness/claude.go`:
- `BuildArgv` emits `claude -p <prompt> --verbose --output-format stream-json [--model m] [--resume sid] [--append-system-prompt sys] [--permission-mode auto]`.
- `ParseEvent` wraps the existing `parse.go` logic; one NDJSON line → one canonical `Event`.
- `AuthEnv` returns `{ANTHROPIC_API_KEY, CLAUDE_CODE_OAUTH_TOKEN}` populated from `AuthConfig`.
- `Capabilities`: all five `Supports*` true, `EmitsCost` true, `NeedsTTY` false.

### Step 2 — Implement `harness.Codex`

`internal/harness/codex.go`:
- `BuildArgv` returns the Codex argv form (move from `host_codex.go`).
- `ParseEvent` parses native Codex JSON events directly into the canonical `Event` — no Claude-event synthesis intermediate. Tracks `session_id` / `turn.completed` / usage / cost from native events.
- `AuthEnv` returns `{OPENAI_API_KEY}` and ensures `~/.codex/auth.json` is honored.
- `Capabilities`: `SupportsResume = false` (matches existing host_codex behavior), `EmitsCost = false`, `EmitsUsage = true`, `SupportsSystemPrompt = false` (Codex has no `--append-system-prompt`; runner prepends to prompt).

### Step 3 — Rewire the runner

- `internal/runner/agent.go`'s `launchOne` calls `harness.Lookup(activity-harness).BuildArgv(req)` and `ParseEvent(line)` instead of `host.go` / `host_codex.go`.
- Delete `internal/sandbox/host_codex.go` (its logic now lives in `harness.Codex`).
- Delete the Claude-specific argv assembly in `internal/sandbox/host.go`; `HostExecutor` becomes a pure process launcher that takes argv + env + cwd.
- The Claude-result-synthesis adapter for Codex disappears — the runner consumes canonical `Event`s directly, no more "fake a Claude result line" trick.

### Step 4 — Rewire env config and handlers

- `internal/envconfig/envconfig.go` populates `harness.AuthConfig` instead of returning a `map[string]string` of mixed keys.
- `internal/handler/env.go` swaps `sandbox.Type` for `harness.ID` at call sites; settings-tab `/test` endpoint dispatches on `harness.Lookup(id)` and asks each harness for its own "verify auth" probe (added as an optional method or a separate `Probe(ctx, env)` function — decide during implementation).
- `internal/handler/config.go` exposes `harness.All()` in `/api/config` instead of the hardcoded `[]sandbox.Type{Claude, Codex}`.

### Step 5 — Remove the sandbox compat shim

- Delete `sandbox.Type` aliases from `internal/sandbox/sandbox.go`.
- Search-and-replace `sandbox.Claude` → `harness.Claude`, `sandbox.Codex` → `harness.Codex` across the tree.
- Persisted task records keep their `sandbox` field name (or rename to `harness` with a migration — decide during implementation; cheap migration since values are unchanged strings).

## Tests

- All existing runner tests must pass unchanged. They are the regression gate.
- New `internal/harness/claude_test.go`: argv shape for every flag combination; parse fixtures for system / assistant / user / result records; auth env wiring.
- New `internal/harness/codex_test.go`: argv translation matches the previous `host_codex.go` golden output; parse fixtures for `thread.started`, `turn.completed`, `item.*` events; usage extraction.
- Parse fixtures live under `internal/harness/testdata/{claude,codex}/`. Reuse existing fixtures from `internal/runner/testdata/` where possible.
- Handler config test asserts `/api/config` exposes the same `sandboxes` list values as before (`["claude","codex"]`) — proves backward compatibility on the API surface.

## Acceptance criteria

- `make test` green.
- E2E lifecycle (Claude + Codex lanes) green.
- `grep -r 'sandbox.Claude\|sandbox.Codex\|case Claude:\|case Codex:' internal/ --include='*.go'` returns only files under `internal/harness/` (the new home).
- `internal/sandbox/host_codex.go` no longer exists.
- Codex token-usage attribution in the UI is unchanged (manual smoke check: run one Codex task, verify usage panel shows tokens).

## Risks

| Risk | Mitigation |
|---|---|
| Parse divergence between old and new Codex paths | Golden NDJSON fixtures captured from current `host_codex.go` output, asserted byte-equal post-migration. |
| Hidden Codex behavior in `codex-agent.sh` (already gone after host-default) | Re-read the script before deletion to capture any flag we still need (e.g., `--config model_reasoning_effort="low"` when `WALLFACER_SANDBOX_FAST != "false"`). |
| Persistence-layer rename causes task replay regressions | Keep the `sandbox` field name in `store.Task` for one release; document as a follow-up rename. |

## Outcome

Shipped:

- **Write path.** `harness.Claude` and `harness.Codex` own argv assembly
  (`BuildArgv`), auth env (`AuthEnv`), and capabilities.
  `HostBackend.launchClaude` / `launchCodex` call
  `harness.Lookup(...).BuildArgv(req)` via a `requestFromClaudeSpec` shim.
- **Read path.** The runner derives `agentOutput` from per-line
  `harness.ParseEvent` via `parseAgentStream` + the `parseHarnessOutput`
  accumulator. All four production parse sites (heavyweight/inspector
  `runAgent`, commit-message, ephemeral ideation, idea-backlog re-parse) go
  through it; `parseOutput` survives only as the fallback for an
  unregistered harness. claude keys its terminal on line shape (typeless /
  `type:"result"`); codex maps `turn.completed` and the normalized result
  envelope. `Subtype` and top-level `total_cost_usd` are carried through so
  token-limit and cost-budget accounting are unchanged.
- **No cross-harness synthesis.** `host_codex.go` recovers codex's final
  message from `--output-last-message` and emits it as a codex-native
  `turn.completed` event (not a Claude-shaped result line), parsed by
  `harness.Codex.ParseEvent`.
- The agent-type enum is unified: `harness.ID` is the only type;
  `sandbox.Type` (and its `Parse`/`Normalize`/`Default`/`All`/`IsValid`/
  `OrDefault`) is deleted. `store`, `envconfig`, `handler`, `planner`,
  `runner`, and `sandbox` all reference `harness.ID`.
- `WALLFACER_SANDBOX_FAST` is threaded through `harness.Request.FastMode`
  (resolved by the host backend from the per-task env), fixing a regression
  where the harness read it from the server process env (always empty).
- Dead `SupportsAppendSystemPrompt` probe and `extractPromptAndModelFromClaudeArgv`
  removed.

Remaining (cosmetic, separate phase):

- `internal/sandbox` is now purely the launch/executor layer but retains the
  `sandbox` name and container-era `ContainerSpec` fields; renaming to
  `internal/executor` and slimming the spec is deferred.
