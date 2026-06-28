---
title: Unified Transcript Rendering (Raw ↔ Rendered, all harnesses)
status: archived
depends_on:
  - harness-abstraction
  - agon-trajectory-streaming
affects:
  - internal/harness/harness.go
  - internal/harness/codex.go
  - internal/harness/opencode.go
  - internal/harness/pi.go
  - internal/handler/stream.go
  - internal/apicontract/routes.go
  - frontend/src/lib/prettyNdjson.ts
  - frontend/src/composables/useTaskActivity.ts
  - frontend/src/components/TaskDetail.vue
effort: large
created: 2026-06-27
updated: 2026-06-28
author: changkun
dispatched_task_id: null
---

# Unified Transcript Rendering (Raw ↔ Rendered, all harnesses)

## Problem

The task **Activity** tab dumps the raw NDJSON transcript as a `<pre>` block in
two common cases (see `TaskDetail.vue` activity section, the `v-else` fallback):

1. **Prose-only turns** — a session that produced no tool calls / thinking (e.g.
   the "Greeting Exchange" greeting) yields zero `ActivityRow`s, so the rendered
   path is skipped and the giant `system:init` frame + answer are shown as raw
   JSON. The assistant's answer prose is never rendered at all on this path.
2. **Non-Claude harnesses** — the frontend parser `prettyNdjson.ts` only
   understands Claude Code's `stream-json` dialect. Codex / OpenCode / Pi (and
   partially Cursor) emit different formats, so their transcripts fall through to
   the raw dump even when they did real work.

The user wants the transcript to **always offer two views** — (1) raw JSON
stream, (2) a rendered chat/trajectory view — with a **normalization layer**
between the harness-native stream and the renderer, so the rendered view works
for **all five harnesses** (claude, codex, cursor, opencode, pi). Default to
Rendered; allow toggling to Raw (mirrors the existing Spec/Result `Raw ↔
Rendered` button idiom in `TaskDetail.vue`).

## Findings (why a hybrid, not one path)

The backend **already** normalizes every harness into a canonical
`harness.Event` (`internal/harness/harness.go`): each `Harness.ParseEvent(raw)`
maps one native NDJSON line → `Event{Kind, Text, Tool{Name,Input,Output,Error},
Usage, …}`. But the fidelity is **uneven**, and that dictates the design:

| Harness  | Text broken out | Tool name+input+output+error | Thinking | FE `prettyNdjson` parses raw? |
|----------|:---:|:---:|:---:|:---:|
| Claude   | ✗ (in `Raw` only) | ✗ (in `Raw` only) | ✗ | **✓ richly today** |
| Cursor   | ✓ | ✓ | ✗ | ⚠ partial (misses cursor's separate `tool_call` events) |
| OpenCode | ✓ | ✓ | ⚠ folded to empty text | ✗ |
| Pi       | ✓ | ✓ | ✗ skipped | ✗ |
| Codex    | ✗ (`item.*` collapsed to `KindAssistantText`) | ✗ | ✗ | ✗ |

So: Claude is already rich **on the frontend**; cursor/opencode/pi are already
rich **on the backend** `Event`; codex is thin **everywhere**. Reusing each
parser where it already lives writes the least new code and keeps every parser in
exactly one place.

## Design (hybrid normalization, one shared renderer)

The renderer is shared over the existing `ActivityRow` model. Only the
**normalizer** differs by source:

### 1. Claude → keep `prettyNdjson` (unchanged)

Claude's frontend parser is rich, tested, and handles the narration/thinking
timeline (`parseTurn`). The canonical `Event` is strictly coarser for Claude, so
routing it through the backend would *regress* today's view and risk the runner's
last-text/usage extraction. **Do not touch Claude's path.** It is the
grandfathered frontend parser.

### 2. Cursor / OpenCode / Pi → backend canonical `Event` over `?format=normalized`

Extend the existing logs handler `StreamLogs` (`internal/handler/stream.go`,
served at `GET /api/tasks/{id}/logs`) with a `?format=normalized` query param.
When set, the handler streams the same raw turn lines through the **task's
harness** `ParseEvent`, emitting one normalized JSON object per recognised event
(NDJSON), instead of the raw bytes. This **inherits the existing streaming
infrastructure** (chunked, live tasks + stored turns) — no separate batch
endpoint, no live-streaming regression.

Wire shape (a stable DTO so `EventKind`'s int iota never leaks to the wire):

```json
{"kind":"tool_start","tool":{"name":"Read","input":{...}},"session_id":"…"}
{"kind":"tool_end","tool":{"name":"Read","output":"…","error":""}}
{"kind":"thinking","text":"…"}
{"kind":"assistant","text":"…"}
{"kind":"result","text":"…","usage":{"input_tokens":…,"output_tokens":…,"cost_usd":…},"stop_reason":"end_turn"}
{"kind":"error","subtype":"…","text":"…"}
```

`kind` values map 1:1 from `EventKind` via a string table
(`unknown|system_init|assistant|thinking|tool_start|tool_end|user_result|result|error`).
`KindUnknown` lines are **skipped** on the normalized path (they carry no
renderable content; the raw view still shows them).

The task's harness id comes from `task.Sandbox` (`store.Task`), already persisted
per task.

### 3. Codex → enrich `codex.go` once

Codex's `item.*` family currently collapses to `KindAssistantText` ("until a
richer mapping is justified" — `codex.go`). Enrich `codexHarness.ParseEvent` to
break out the `item.*` subtypes into `KindAssistantText` (message),
`KindToolCallStart/End` (command_execution / file_change / tool), and
`KindThinking` (reasoning), populating `Tool{Name,Input,Output,Error}`. Codex
renders thin everywhere today, so the regression surface is near-zero. **Get the
`item.*` schema from a real codex `--json` run (or codex source), not guesswork**
— there is currently **no codex testdata fixture** (only cursor/opencode/pi).
Capture one and commit it as `internal/harness/testdata/codex/headless-run.ndjson`.

### 4. Thinking fidelity → add `KindThinking` (additive)

`EventKind` has no thinking/reasoning kind, so reasoning is dropped or folded to
empty `AssistantText` (e.g. `opencode.go` `reasoning`). Add `KindThinking` and
emit it from opencode (`reasoning`), pi (thinking blocks), and codex (reasoning
items). This is **additive and safer** than the current "leave Text empty" hack,
and lights up thinking rows for those harnesses. Claude continues to surface
thinking via its own FE parser.

Constraint: `KindThinking` must be **inert** to `runner/harness_parse.go`'s
`parseHarnessOutput` accumulation (it keys on `KindAssistantText`/`KindResult` —
a new kind should be ignored). Verify with a test; do not let it pollute the
saved last-text answer.

### 5. Activity tab → Raw ↔ Rendered toggle (default Rendered)

In `TaskDetail.vue` activity section, **below the untouched Oversight Summary
box**:

- A `Raw ↔ Rendered` toggle button (reuse the Spec/Result idiom). Default
  **Rendered**.
- **Rendered** view renders `ActivityRow`s (trajectory) **and** the assistant
  answer prose (`renderMarkdown`) — fixing the prose-only case where the answer
  was previously thrown away. Source of rows + answer:
  - `harness === 'claude'` → `parseTurn(raw)` (existing prettyNdjson).
  - else → parse the `?format=normalized` event stream into `ActivityRow`s +
    trailing answer.
- **Raw** view: the original raw NDJSON (`ansiToHtml(rawOutput)`), as today.
- **Fallback**: if the rendered view yields nothing (no normalizer for the
  harness, or empty), auto-show Raw. Never show an empty pane.

`useTaskActivity` becomes harness-aware: it always exposes `raw` (for the Raw
view + Claude rendering) and, for non-claude harnesses, an `activity` + `answer`
derived from the normalized stream.

### 6. Agon transcript → raw ↔ rendered toggle only (no refactor)

Agon already renders well (critic/proposer fork/round accordion, markdown bodies;
`agon-trajectory-streaming`) and is a genuinely different shape (already
normalized server-side to `AgonTranscript` JSON). **Do not** refactor it into
`ActivityRow`. The only add for consistency: a `Raw ↔ Rendered` toggle in the
Verification tab where Raw shows the underlying `transcript.jsonl` (+ round
bodies) and Rendered is the existing accordion. (Optional / lowest priority; can
land last or be split out.)

## Outcome (2026-06-27)

Phases 1–4 implemented directly (not dispatched) and verified end-to-end in the
running app.

Backend: added `?format=normalized` to `StreamLogs` via a `normalizingWriter`
that rewrites the raw harness-native NDJSON into a stable `normalizedEvent` DTO
through the task's harness `ParseEvent` — installed before the live/stored/phase
branching so every serve path inherits it (no live-streaming regression). Added
an additive `KindThinking` (opencode `reasoning` and codex reasoning items emit
it) and guarded the runner's last-text fallback to `KindAssistantText` so
reasoning never becomes the answer. Enriched `codex.go`'s `item.*` to the real
exec-json schema (agent_message→text, reasoning→thinking, command_execution→tool
with command/output/error, file_change/mcp/web_search→generic tool) and added
the missing codex testdata fixture. The normalized tool DTO carries an `id` so
the renderer pairs `tool_start`/`tool_end` into one row.

Frontend: `createNormalizedParser` (events → `ActivityRow` + answer, generic v1
tool summaries) and `createTurnParser` (incremental Claude timeline that also
yields the answer prose). `useTaskActivity` is harness+mode aware — Claude parses
raw client-side; other harnesses render from `?format=normalized`; the raw view
shows the native stream. The Activity tab gained a `Raw ↔ Rendered` toggle
(default Rendered) that renders trajectory **and** answer prose and auto-falls
back to raw when nothing parses. The Oversight Summary box is untouched.

Verification: per-harness Go + TS unit tests over the real testdata fixtures,
plus a live-app pass (booted wallfacer + vite, seeded one done task per harness
with its fixture as the saved turn output): the Claude greeting renders prose
(was the raw-JSON dump), codex/cursor/opencode/pi render trajectories + answer,
and the Raw toggle shows the native stream — no console errors.

Phase 4: agon kept its existing rendered fork/round accordion and gained a
`Raw ↔ Rendered` toggle in the Verification tab; raw shows the assembled
transcript payload (fork/round records + bodies) pretty-printed — no agon
refactor, no backend change (the payload is already normalized server-side and
is more complete than the raw `transcript.jsonl`, which only carries pointers).

Deviations: (1) thinking fidelity is opencode + codex only — claude/pi pack
reasoning inside a message line and `ParseEvent` is one-line-one-event, so their
thinking stays embedded (Claude still shows it via its own parser). (2) tool
summaries are generic v1 (name + expandable raw input); per-harness rich
summarisers are a later pass.

## Non-Goals

- **Per-harness rich tool summaries.** `Tool.Input` is harness-shaped (cursor =
  whole object, opencode = `Part.State.Input`, pi = `Args`). v1 uses a **generic**
  tool row: tool `Name` as the label + the raw `Input` (pretty-printed) as the
  expandable `detail`; `Output`/`Error` into detail/error rows. Claude keeps its
  rich `summariseToolInput`/`toolPreview`. Richer per-harness summarizers are a
  later pass.
- Refactoring agon into the `ActivityRow` trajectory model.
- Token-by-token / sub-event streaming changes (the per-line event granularity is
  the unit).
- Reconstructing user-prompt bubbles from the agent log (the log is agent output;
  user turns aren't in it). The rendered view is a trajectory + answer, not a
  full two-sided chat.

## Phasing / Acceptance Criteria

**Phase 1 — backend `KindThinking` + normalized endpoint.** Add `KindThinking`;
emit it from opencode/pi. Add `?format=normalized` to `StreamLogs` that streams
canonical events (wire DTO) through the task's harness `ParseEvent`. Tests:
cursor/opencode/pi testdata fixtures, fed through the normalized path, produce the
expected `kind`/tool/text sequence; `KindThinking` is inert to
`parseHarnessOutput`; `?format=normalized` with the wrong/unknown harness degrades
gracefully (empty/raw, never 500). `go test ./...` green.

**Phase 2 — codex enrichment.** Capture a real codex `--json` fixture; enrich
`codex.go` `item.*` into text/tool/thinking events. Test: the codex fixture
yields a non-trivial trajectory (≥1 tool row, answer text) through the normalized
path.

**Phase 3 — frontend renderer + toggle.** Harness-aware `useTaskActivity`; a
normalized-event → `ActivityRow` + answer parser (generic tool rows); render
trajectory **and** answer prose in the Activity tab; `Raw ↔ Rendered` toggle
(default Rendered, auto-fallback to Raw). Keep the Oversight Summary box
untouched. Unit tests for the normalized→`ActivityRow` mapper (using fixtures
mirrored from the Go testdata); `vue-tsc` clean.

**Phase 4 — agon raw toggle (optional).** Raw ↔ Rendered toggle in the
Verification tab for the agon transcript.

**Acceptance (UI verification, required by the user).** Run the app and confirm
the Activity tab renders properly for each harness using the testdata fixtures
(not just unit tests): a Claude greeting shows rendered prose (not raw JSON); a
cursor/opencode/pi/codex transcript shows a rendered trajectory + answer; the
`Raw` toggle shows the original JSON; a harness with no events falls back to Raw
without an empty pane.
