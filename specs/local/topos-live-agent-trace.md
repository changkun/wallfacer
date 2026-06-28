---
title: Live Agent Traces for Topos Runs (Event Seam + Stream + UI)
status: drafted
depends_on:
  - topos-runtime-integration
affects:
  - internal/agentgraph/adapter.go
  - internal/agentgraph/agentgraph.go
  - internal/runner/execute.go
  - internal/handler/tasks_lineage.go
  - internal/apicontract/routes.go
  - internal/store/models.go
  - frontend/src/components/AgentLineage.vue
  - frontend/src/api/types.ts
effort: xlarge
created: 2026-06-28
updated: 2026-06-28
author: changkun
dispatched_task_id: null
---

# Live Agent Traces for Topos Runs

## Problem

Topos agentic runs are headless from wallfacer's view. `topos.Runner.Run` is a
blocking batch call that returns the final text plus a `Lineage` graph only when
the whole run finishes (`topos/topos.go:191`); `internal/agentgraph` exposes no
progress hook. So during a multi-agent run the user sees nothing live — the
lineage graph (and `AgentLineage.vue`) materializes only after completion.

Single-agent task runs stream live (harness stdout → Activity tab) and agon
streams live (per-round session-dir files → transcript endpoint). The new,
multi-agent runtime is the one surface with no live trace.

The data already exists inside topos. The agent loop builds a `Transcript
[]models.Message` and `FinalText`, and already consumes `models.KindTextDelta`
(`topos/runtime/loop/loop.go:79-215`). The `hooks.Bus` emits a rich structured
stream — `SessionStart`, `UserPromptSubmit`, `PostToolUse`, `SubagentStart/Stop`
(delegation), `Stop`, `SessionEnd` — and keeps an append-only `EventLog()`
(`topos/harness/hooks/bus.go`). None of it is reachable by an embedder: the bus
is internal (`Runner.bus`), `Options` has no event field, and assistant text is
never dispatched as an event (only returned in `loop.Result`).

## Decision

Expose topos's existing event stream to embedders, add an assistant-text event so
the trace is a readable transcription (not just lifecycle/tool events), and in
wallfacer forward it to a live per-task trace that an SSE endpoint streams and the
task UI renders — with the lineage nodes lighting up live from the same events.

This is also the convergence the [[topos-runtime-integration]] / agon discussion
pointed at: agon already solved live multi-agent tracing via session-dir files;
topos should grow the same capability as the shared runtime, through a clean event
seam rather than file tailing.

## Design

### A. topos SDK — expose an event observer (`latere.ai/x/topos`)

The runtime already dispatches everything to `Runner.bus`; the gap is a public
seam. The import guard restricts wallfacer to the root `topos` package, so the
event types must be reachable from root.

- Add to `topos.Options` an observer sink, e.g.
  `Observer func(Event)` (a single callback is allocation-light and thread-safe
  to reason about; the runner registers it as a bus `Consumer` that always
  returns `VerdictAllow`, so observation never alters control flow).
- Define a root-level, subpackage-free `topos.Event`:
  ```go
  type Event struct {
      Name      string    // re-exported EventName ("SessionStart", "PostToolUse", …)
      AgentID   string    // which agent/peer emitted it (lineage node id)
      SessionID string
      At        time.Time
      // Payload carries the typed hooks payload as already-marshalled JSON so
      // the root package need not re-export every subpackage payload type.
      PayloadJSON json.RawMessage
  }
  ```
  Adapting `hooks.LogEntry` → `topos.Event` in one place keeps the subpackage
  types internal while giving embedders the full, audit-grade payload.
- The observer is called synchronously in dispatch order; document that a slow
  observer backpressures the run (wallfacer's consumer must be non-blocking —
  it pushes to a buffered channel and returns).

### B. topos SDK — emit assistant text (the "transcription")

Lifecycle + tool events alone omit the agent's own words. Add an assistant-turn
event so the trace reads as a transcript:

- New `EventAssistantMessage` dispatched in the loop after a turn's assistant text
  is assembled (`loop.go` around the `KindTextDelta` accumulation), payload
  `{SessionID, AgentID, Text, Turn}`.
- v1 emits the **full turn text once per turn** (simple, readable). Token-level
  `KindTextDelta` streaming is a follow-up (OQ-1) — more plumbing, marginal value
  for a verification/observability trace.

### C. wallfacer — forward events through the agentgraph seam

- `agentgraph.RunFlowWithModel` sets `Options.Observer` to a function that maps
  each `topos.Event` → a **topos-free** `agentgraph.TraceEvent` (preserve the
  seam: only `internal/agentgraph` names topos types) and hands it to a per-run
  sink passed in by the runner.
- The sink is non-blocking: it appends to an in-memory ring buffer and fans out
  to live subscribers (mirror the agon in-flight pattern in `tasks_autopilot.go`,
  but event-driven, not polled).

### D. wallfacer — persist + serve

- Persist the trace so a completed run replays: append events to a per-task
  trace log (a sidecar file under the task's state dir, JSONL — cheaper than
  growing a `Task` field unbounded). The final `Lineage` persistence is unchanged.
- Endpoint `GET /api/tasks/{id}/agentgraph/trace`:
  - live run → **SSE** stream of events (do not reuse agon's 2.5s poll; that
    polling + unmemoized render was the CPU sink found in the resource-governance
    work — SSE avoids it).
  - completed run → replay the persisted JSONL then close.

### E. frontend — render the live trace

- Extend `AgentLineage.vue`: subscribe via `EventSource`; drive node status live
  (`SessionStart`→running, `Stop`/`SessionEnd`→done, `SubagentStart`→new node +
  delegate edge) so the graph animates as the run proceeds.
- A per-node transcript panel renders the agent's `AssistantMessage` text and a
  compact tool-call log. Markdown rendering must be **memoized by content** (the
  exact bug found in `AgentVerification.vue` — re-parsing every round on each tick
  pegged the browser); render each turn once.

## Phasing / Acceptance Criteria

**Phase 1 — topos event seam (SDK).** `Options.Observer`, root `topos.Event`,
root-re-exported event-name constants, and `EventAssistantMessage`. Tests (fake
model): an observer receives SessionStart → UserPromptSubmit → AssistantMessage →
Stop → SessionEnd in order for a single agent, and SubagentStart/Stop for a
delegated peer. Observation does not change run output (lineage/final text equal
with and without an observer).

**Phase 2 — wallfacer forward + persist + SSE.** agentgraph forwards events to a
per-task sink; events persist to JSONL; `GET …/agentgraph/trace` streams live
(SSE) and replays completed runs. Tests: a fake-model agentic run produces the
expected `TraceEvent` sequence on the sink; the endpoint streams then replays;
the seam stays topos-free (import-guard test still passes).

**Phase 3 — live UI.** `AgentLineage.vue` animates node status from the stream and
shows per-agent transcript with memoized markdown. Test: nodes transition on
mocked SSE events; markdown renders once per turn (no re-parse on subsequent
events).

## Non-Goals

- Token-level delta streaming (per-turn message text is enough for v1; OQ-1).
- Live trace for non-topos paths (single-agent already streams; agon already has
  its trajectory view).
- Rendering the trace inside the Map's `GraphCanvas` (coordinate with the
  in-progress mission-control map work; task-detail `AgentLineage.vue` first).

## Open Questions

- **OQ-1 — granularity.** Per-turn `AssistantMessage` (v1) vs token-level
  `TextDelta` streaming. Deltas give a typewriter effect but need backpressure
  handling and more event volume. Defer to a follow-up unless the per-turn
  latency feels too coarse.
- **OQ-2 — observer vs multi-consumer.** A single `Options.Observer` callback vs a
  general `Options.Hooks` registration (which would also let embedders influence
  decisions, e.g. PreToolUse gating). Start with the observer (pure observation);
  decision hooks are a separate capability.
- **OQ-3 — persistence shape.** Sidecar JSONL under the task state dir (proposed)
  vs a capped `Task` field. JSONL avoids unbounded growth in the task record and
  matches how agon persists its session artifacts.
