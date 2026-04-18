---
title: Capture Planning Round Usage
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/progress-cost-tracking/planning-usage-store.md
affects:
  - internal/planner/usage.go
  - internal/planner/usage_test.go
  - internal/handler/planning.go
  - internal/handler/planning_test.go
effort: medium
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Capture Planning Round Usage

## Goal

After each planning round completes, parse its token/cost usage from
the agent exec result and persist one `TurnUsageRecord` via the
`AppendPlanningUsage` primitive. Failed rounds do not write a record.

## What to do

1. In `internal/planner/planner.go::Exec` (around L124–L149), extend
   the return so the handler can see per-round usage alongside the
   existing `sandbox.Handle`. Simplest shape: return a small
   `ExecResult` struct or a second typed value; do not mutate
   `sandbox.Handle`.
2. Parse the round's usage from the raw stdout JSON already being read
   at `internal/handler/planning.go:184`. Populate a `TurnUsageRecord`
   with `InputTokens`, `OutputTokens`, `CacheReadInputTokens`,
   `CacheCreationTokens`, `CostUSD`, `StopReason`, and `Sandbox` (the
   planner's configured sandbox type). Set
   `SubAgent = store.SandboxActivityPlanning`, `Timestamp = time.Now().UTC()`.
3. Derive the storage group-key from the planner's scoped workspaces
   — the planner already knows which workspace group it serves. Use
   `internal/workspace/groups.go::GroupKey`-style sorted paths piped
   through the group-key helper added in the store task
   (`InstructionsKey`-equivalent).
4. Compute `Turn` as the next round number for the group. Simplest
   correct approach: read the current file line count via
   `store.ReadPlanningUsage(root, groupKey, time.Time{})` and add 1.
   In-memory counters are fine too but must survive a planning sandbox
   restart — on cold start, rehydrate from the file.
5. In `internal/handler/planning.go` after line 184 (and before the
   response is streamed back), call `store.AppendPlanningUsage(...)`
   inside the round-completion path. If append returns an error, log
   and continue — a persistence failure must not fail the user-facing
   round.
6. Wire the data root through the handler via the existing
   `internal/store` root resolution; do not re-invent path discovery.

## Tests

Extend or create `internal/handler/planning_test.go`:

- `TestPlanningHandler_PersistsRoundUsage` — drive a fake planner
  `Exec` that returns a synthetic usage result; invoke the message
  handler; assert a `TurnUsageRecord` lands on disk with the right
  tokens/cost/sub-agent.
- `TestPlanningHandler_IncrementsTurn` — run two rounds in sequence;
  assert the second record has `Turn == previous + 1`.
- `TestPlanningHandler_FailedExecDoesNotPersist` — fake an error from
  `Exec`; assert no record is appended.
- `TestPlanningHandler_AppendErrorDoesNotFailRound` — inject a store
  failure; assert the round still returns a normal response to the
  caller.

## Boundaries

- Do not modify `sandbox.Handle` or any interface in
  `internal/sandbox/`.
- Do not add fields to `TurnUsageRecord`.
- Do not wire capture into any handler other than planning.
- Do not touch board task execution, commit pipeline, or their usage
  tracking.

## Implementation notes

- **Planner.Exec return unchanged.** The spec suggested extending
  `planner.Exec`'s return with a per-round usage value. In practice the
  handler already consumes stdout from the returned `sandbox.Handle`
  after `Wait()`, so usage can only be known downstream of `Exec`. The
  implementation instead adds a pure parser, `planner.ExtractUsage(raw)`,
  that the handler calls after reading `rawStdout`. `Exec`'s signature is
  untouched, which matched the spec's boundary "do not mutate
  `sandbox.Handle`" and avoided a speculative return shape.
- **New file `internal/planner/usage.go`** (not listed in the original
  `affects` — added during implementation) hosts the `RoundUsage` type
  and `ExtractUsage` function. The `affects` list was updated to reflect
  the actual files touched.
- **Sandbox type is hardcoded to `sandbox.Claude`** in the persisted
  record, matching `planner.Exec`'s existing hardcoded
  `sandbox.Claude` at the container spec site. When the planner learns
  to run other sandboxes, both sites will flip together.
- **Turn number derived from the existing log.** As suggested by the
  spec, `Turn = len(ReadPlanningUsage(...)) + 1`. This naturally survives
  process restarts and avoids an in-memory counter. Planning's `busy`
  lock serializes rounds, so the read-then-append has no race.
