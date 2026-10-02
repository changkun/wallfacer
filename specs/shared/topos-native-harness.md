---
title: Topos as Native Harness
status: stale
depends_on:
  - specs/local/topos-runtime-integration.md
affects:
  - internal/harness/
  - internal/agentgraph/
  - internal/runner/
  - internal/handler/
  - internal/store/
  - frontend/src/components/AgentTrace.vue
  - frontend/src/views/AgentGraphPage.vue
  - frontend/src/styles/app/buttons-hero.css
effort: xlarge
created: 2026-06-30
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Topos as Native Harness

## Overview

Make Topos (`latere.ai/x/topos`) wallfacer's first-class **native agent harness**
and the default execution path going forward, so a fresh wallfacer run is powered by
the latere.ai-native runtime rather than the Claude Code CLI. Claude Code and every
other harness (Codex, Cursor, OpenCode, Pi) remain registered and selectable; only the
hardcoded *default* changes. The native harness runs in-process and offline for local
use, and offers signed-in users the option to run the same task as a hosted agent
session on the Latere platform.

This is the harness-layer counterpart to the already-shipped
[[topos-runtime-integration]] (which embedded Topos as a *separate* multi-agent runtime
path) and [[topos-live-agent-events]] (live tracing). Those made Topos *runnable*; this
spec makes it the *native default* and reconciles it with the harness abstraction.

## Current State

Two seams exist side by side and do not yet meet:

1. **The harness abstraction** (`internal/harness/`). Each harness is a CLI adapter:
   the `Harness` interface (`harness.go`) is built around a subprocess — `BuildArgv(req)
   → argv + stdin`, `ParseEvent(raw []byte) → Event`, `AuthEnv(cfg) → env`. Registered
   harnesses (claude/codex/cursor/opencode/pi/fake) self-register via `init()` into a
   package registry (`registry.go`). `Default() ID { return Claude }`
   (`registry.go:48`) hardcodes the default, and the doc comment already anticipates
   "a follow-up may make this configurable."

2. **The Topos runtime path** (`internal/agentgraph/`). Topos is embedded **in-process**
   and runs agentic flows via Lux (`flow.Flow.Agentic`); it has **zero** references to
   `internal/harness`. A run produces a trace graph, not a harness `Event` stream. The
   single importer of the root `topos` package is `internal/agentgraph`, guarded by an
   import-boundary test.

**Hardcoded Claude-Code couplings** beyond the registry default:

- `internal/runner/agent.go:189` — `primary := harness.Claude`.
- `internal/runner/agent.go:256,271` — a Claude-specific token-limit error fallback.
- `internal/runner/commit.go:487` — `initial := harness.Claude`.
- `internal/runner/container.go:252,266` — return `harness.Claude`.

A task pins its harness through the `Sandbox` field (a legacy name carried from the old
`sandbox.Type`; see `internal/runner/agent_test.go:172` using `Sandbox: harness.Claude`).

## The central design decision: in-process harness vs CLI subprocess

The `Harness` interface assumes a subprocess that emits NDJSON. Topos runs in-process.
Two ways to make Topos satisfy the harness contract:

- **(A) Generalize the harness seam to admit an in-process harness.** Add an execution
  strategy so a harness is either *subprocess* (today's `BuildArgv`/`ParseEvent`) or
  *in-process* (`Run(ctx, Request) → <-chan Event`). Topos reuses the existing
  `internal/agentgraph` embed (a single-agent run = a one-node region) and maps
  `topos.Event` → the canonical harness `Event`. No new process boundary, no `latere`
  binary dependency. Cost: the harness interface and its call sites in `internal/runner`
  must stop assuming argv.
- **(B) Run Topos as the `latere` CLI subprocess.** `../latere-cli` already streams a
  Topos run as NDJSON (`topos_stream.go`) and runs locally (`topos_local.go`), which
  fits `BuildArgv`/`ParseEvent` with near-zero interface change. Cost: a process
  boundary wallfacer already avoids for agentic runs, plus a hard dependency on an
  installed `latere` binary, duplicating an embed wallfacer already has.

**Decision: (A), ratified 2026-06-30.** wallfacer already embeds Topos in-process for
multi-agent runs ([[topos-runtime-integration]]); the native harness is the *same* engine
at single-agent scale, not a second copy reached through a subprocess. **CLI invocation of
`latere` is explicitly rejected** — no subprocess boundary for the native path.
`../latere-cli`'s `topos_*.go` commands are the **reference for the driver logic** (region
build, provider selection, sandbox wiring, streaming), not a binary to shell out to.

## Architecture

```mermaid
flowchart TD
  task[Task run] --> resolve{harness pin / Default}
  resolve -->|claude/codex/.../pi| cli[Subprocess harness<br/>BuildArgv + ParseEvent]
  resolve -->|topos default| topos[In-process Topos harness]
  topos --> region[single-agent one-node Region<br/>scales up to agentgraph mesh]
  region --> mode{execution target}
  mode -->|local, the default| local[in-process runtime in the task worktree]
  mode -->|hosted, signed in| cloud[hosted agent session<br/>api.latere.ai/v1/agents]
  topos --> ev[topos.Event → canonical harness Event] --> stream[same Activity/timeline + trace trace]
```

The native harness unifies the two seams: it is a registered `Harness` (so the picker,
config, and runner treat it uniformly) whose implementation is the embedded Topos
runtime. The multi-agent agentgraph path and the single-agent native harness become the
same engine at different region sizes.

## Components

### 1. Harness registration + Result/event mapping

- New `harness.Topos ID = "topos"` constant and a `toposHarness` registered via `init()`
  (mirrors `claude.go`/`codex.go`). Lives in `internal/harness/topos.go`.
- Bridge `internal/agentgraph` into the harness contract: a single-agent run maps
  `topos.Event` (already exposed via `Options.Observer`, see [[topos-live-agent-events]])
  to the canonical `harness.Event` (`KindAssistantText`, `KindToolCall*`, `KindResult`
  with `Usage`/`StopReason`). The canonical types are already harness-agnostic, so the
  mapping is total.
- Resolve the in-process-vs-subprocess decision (OQ-1) by either extending the `Harness`
  interface with an in-process `Run` strategy (recommendation A) or adding a thin
  subprocess adapter (B).
- Keep the import boundary: only `internal/agentgraph` (and the new harness bridge it
  backs) names a `topos` type.

### 2. Default flip + decoupling audit

- `registry.go:48` `Default()` returns `Topos` (make it configurable per the existing
  doc-comment intent; env/config override falls back to `Topos`).
- Audit and unwind each hardcoded `harness.Claude`: `agent.go:189`, `commit.go:487`,
  `container.go:252,266`. Replace with `harness.Default()` / the resolved pin, preserving
  behavior for tasks explicitly pinned to Claude.
- The Claude-specific token-limit fallback (`agent.go:256,271`) is gated on
  `primary == harness.Claude`; generalize or scope it so a non-Claude default does not
  silently lose the fallback (the native harness needs its own degradation story).
- "Decouple from Claude Code" = stop defaulting to the Claude Code **CLI harness**. It
  does **not** remove Claude **models**: Topos can still call Claude models through Lux
  (`../latere-cli/.../topos_claude_auth.go` shows model-credential use is distinct from
  the harness).

### 3. Local and hosted execution

> **Revised 2026-10-02.** This component first injected a remote sandbox provider
> into the in-process runtime, so the loop ran on the user's machine and its tools
> ran in a remote workspace. Latere consolidated its services into one platform,
> and in cloud mode wallfacer calls the platform's Agents capability and nothing
> beneath it ([platform integration](../cloud/latere-integration.md)). The loop and
> its machine now move together or not at all.

A task on the native harness has an execution target. The swap is where the whole
run happens, not which sandbox the local loop reaches.

| Target | Available when | Where the loop runs | Model access | Where tools act |
|--------|----------------|---------------------|--------------|-----------------|
| `local` (default) | always | in-process, the embedded runtime | the configured gateway or provider key | the task's worktree |
| `hosted` | signed in | a hosted agent session at `api.latere.ai/v1/agents` | the session's, billed by the platform | the session's own workload |

- The hosted run, its field on the task, the dispatch sequence, the event mapping and
  the data-boundary confirmation are specified in
  [topos-remote-executor](../cloud/latere-integration/topos-remote-executor.md). This
  spec owns the harness surface the choice is drawn on.
- `local` must work fully offline with no Latere dependency. It is the default path.
- `hosted` is an **opt-in** for signed-in users and must never become a silent
  requirement. Signing in does not change a task's target.

### 4. UI / branding surfacing

The runtime is invisible today (only 3 frontend mentions, all code-comment/help-text).
Surface the native-harness identity:

- Add a `.topos-brand` wordmark rule to `frontend/src/styles/app/buttons-hero.css`
  alongside the existing `.wallfacer-brand`/`.cella-brand` (italic serif,
  `linear-gradient(135deg,#55707a 0%,#6f8a56 58%,#a07045 100%)` clipped to text), and a
  small 4-node graph SVG icon component (ported from the agents-repo `SiteNav` mark).
- Brand `AgentTrace.vue`'s header ("Agent Graph" → node icon + Topos wordmark) and
  upgrade the plain-text "topos runtime" mention in `AgentGraphPage.vue` to the wordmark.
- Show the resolved harness (and local/cloud mode) in the harness picker so a user can
  see and switch the native default.

### 5. Migration / back-compat

- The change is a **default flip, not a removal**: tasks with an explicit `Sandbox`
  harness pin (including Claude) run unchanged.
- Existing serialized tasks with no pin must resolve deterministically; decide whether
  the flip is retroactive (unpinned old tasks become Topos) or only applies to new runs
  (OQ-3).
- No regression to existing subprocess-harness execution paths.

## API Surface

- Config / env: a default-harness override (the `Default()` doc-comment's "configurable"
  follow-up) defaulting to `topos`; a local-vs-cloud execution selector for logged-in
  users.
- No new HTTP routes required for local mode; a hosted run consumes the platform's
  Agents API, not a new wallfacer route.

## Error Handling

- The Claude-only token-limit fallback must not be silently lost when the default is not
  Claude; the native harness defines its own degradation (e.g., surface the Lux/cloud
  error, optional fallback to a configured subprocess harness).
- A hosted dispatch that cannot start (offline, not signed in, a platform refusal) fails
  with the platform's reason and leaves the task ready to run again under either target.
  It does not switch targets on its own: a silent move to `local` would run a task on a
  machine the user did not choose for it, and a silent move to `hosted` would push a
  repository the user did not choose to send.
- Topos observer/runtime panics are already `recover()`-guarded in the SDK
  ([[topos-live-agent-events]] Phase 1); the harness bridge must not reintroduce a crash
  path.

## Testing Strategy

- **Harness registration**: `Topos` registers; `Lookup`/`All` include it; `Default()`
  returns `Topos`; pinned Claude still resolves to Claude.
- **Event mapping**: a fake-model Topos single-agent run maps to the canonical `Event`
  stream (assistant text, tool calls, result with usage) — deterministic via topos
  `ModelFake` (no network), mirroring the agentgraph tests.
- **Decoupling audit**: tests asserting the runner uses the resolved/default harness, not
  a literal `harness.Claude`, at each former hardcoded site; the token-limit fallback
  still fires for a Claude-pinned task.
- **Local/hosted selection**: a `local` task runs in-process with no platform call; a
  `hosted` task is offered only when signed in. The hosted run's own tests are in
  [topos-remote-executor](../cloud/latere-integration/topos-remote-executor.md).
- **Back-compat**: existing pinned tasks serialize/run byte-identically; the import-guard
  test still passes (only the agentgraph bridge names topos).
- **Frontend**: `.topos-brand` renders; `AgentTrace.vue` shows the wordmark; non-topos
  tasks unaffected. `make build` (vue-tsc + SSG) and `golangci-lint` green.

## Open Questions

- **OQ-1 RESOLVED (2026-06-30).** Approach **A** — in-process harness: generalize the
  seam, reuse the agentgraph embed, map `topos.Event` → canonical `Event`. CLI subprocess
  over `latere` (B) is rejected; no subprocess boundary for the native path.
- **OQ-2 RESOLVED (2026-10-02).** The execution target is a per-task field with a
  workspace-level default for new tasks, `local` unless the user changed it. What remains
  open is where the selector sits next to the harness picker.
- **OQ-3.** Retroactivity of the default flip for existing unpinned tasks (new-runs-only
  vs retroactive).
- **OQ-4.** Relationship to [[topos-runtime-integration]] M6 (unified Agents/Flows graph
  UI): does the native single-agent harness share the same editor/registry surface, and
  does this spec wait on the in-progress GraphCanvas/MapPage work or proceed in parallel
  on the harness layer only.

## Implementation Status (2026-07-01)

Built and tested (opt-in native harness — a task pinned to `topos` runs end to
end in-process):

- **Decoupling audit DONE** (`9059cb99`). The harness resolver's final fallback
  (`sandboxForTaskActivity`, `runAgent` tier-6, the plan-commit-message helper)
  routes through `harness.Default()` instead of a literal `harness.Claude`;
  behavior-identical today. The Claude→Codex token-limit fallback is correctly
  left keyed on the *resolved* primary (Claude-specific, not default-specific).
- **Harness registration DONE** (`26a8d02f`). `harness.Topos` + a `toposHarness`
  registry citizen; in-process (`BuildArgv` returns `ErrInProcess`); an
  `InProcess(id)` predicate; surfaced in `harness.All()` (UI selector). Pkg cov
  90.5%.
- **Single-agent seam DONE** (`e865d461`). `agentgraph.RunAgent` runs a one-node
  pinned region through the same engine/observer/model wiring as the multi-agent
  path. Pkg cov 90.1%.
- **Runner wiring DONE** (`519a1dd9`). `execute.go` routes an implement-path task
  whose resolved harness is in-process to `runNativeTopos` (zero container
  launches, single-node trace). Shared `driveToposRun` extracted from
  `runAgenticFlow`. Integration-tested.
- **Branding DONE** (`52d11106`). `.topos-brand` wordmark + the node-graph logo
  in `AgentTrace.vue` ("Agent Graph · powered by Topos").

**Worktree execution DONE (end-to-end, locally) — `wallfacer 72f3fa2f` + `topos 0e8c971`.**
`agentgraph.RunAgent` threads the task worktree into `topos.Options.Workdir` (a
root field — boundary-safe); `execute.go` dispatches the native run after worktree
setup and skips the oversight worker + turn loop. `TestRunAgent_WithWorktreeExecutesInRepo`
proves a native run's tool writes a file into the real worktree. Green under the
co-dev `go.work`; **standalone/CI is gated on the topos release** (push+tag
`0e8c971`, bump wallfacer `go.mod`), else `GOWORK=off` fails on the missing
`Options.Workdir`.

**Remaining (the `Default()` flip is gated on these — NOT yet done):**

0. **Release the topos seam** (user action): push + tag `latere.ai/x/topos`
   `0e8c971` (e.g. v0.0.6), bump wallfacer `go.mod`, so standalone/CI builds.
1. **Commit + verification parity.** After the native run edits the worktree,
   `runNativeTopos`/`driveToposRun` must make a durable git commit of the changes
   and run the verification/test step (wallfacer owns the worktree + git, so this
   is wallfacer-side, no topos change). Today the run edits the worktree but walks
   the state machine through `committing` without committing.
2. **Local and hosted targets.** Draw the execution-target selector for signed-in
   users (Component 3 above). The hosted run itself is
   [topos-remote-executor](../cloud/latere-integration/topos-remote-executor.md).
3. **`Default()` flip + `defaultSandbox` UI default** — flip `registry.go`
   `Default()` to `Topos` and the `config.go` `defaultSandbox` pre-selection,
   ONLY after 0–1 land, else real task runs stop committing code. Update
   `TestDefault`.

## Notes

- [[topos-runtime-integration]]'s frontmatter is **stale**: marked `drafted` though
  M1–M5 are DONE and shipped (only M6 remains, gated on in-progress map work). It should
  read `in_progress`. Flagged here; correct under that spec, not this one.
- This is an xlarge parent spec; break down via `/wf-spec-breakdown` after OQ-1 is
  ratified, since the interface decision reshapes the harness-registration milestone.
