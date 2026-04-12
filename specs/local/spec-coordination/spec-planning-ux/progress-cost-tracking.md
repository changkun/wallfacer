---
title: Planning Cost Tracking
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox.md
affects:
  - internal/planner/
  - internal/store/
  - internal/handler/planning.go
  - internal/handler/stats.go
  - internal/handler/usage.go
  - ui/js/modal-stats.js
  - ui/js/usage-stats.js
effort: medium
created: 2026-03-30
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Planning Cost Tracking

## Design Problem

How is planning sandbox cost recorded and surfaced alongside the existing
board execution cost analytics? Planning is a separate, orthogonal cost
dimension from kanban task execution:

- **Board execution cost is already tracked and solved.** Kanban tasks accrue
  per-turn `TurnUsageRecord`s and per-activity `UsageBreakdown` rows;
  `GET /api/stats` aggregates these by workspace, activity, status, and
  failure category; `modal-stats.js` renders it. This spec does not change any
  of that.
- **Planning cost is currently not captured at all.** The planning sandbox
  (`internal/planner/`, `internal/handler/planning.go`) runs an agent turn
  per user message round but discards usage after streaming the response.
  There is no persisted record, no aggregate, no dashboard row.

The only remaining question is how planning usage is captured and where it
appears in the existing analytics UI — without disturbing the solved
execution-cost path.

Decisions baked in by feedback:

- **Unit of attribution is the message round.** Each round produces one usage
  record regardless of how many specs the round modifies. Splitting a round's
  cost across multiple specs is explicitly out of scope.
- **Aggregation key is the workspace group.** A planning session is scoped to
  exactly one group (via `internal/workspace/groups.go::GroupKey`); its
  per-round usage rolls up to that group.
- **Planning cost appears in the existing usage analytics surface**
  (`modal-stats.js` / `usage-stats.js`) as a new, clearly-labelled planning
  section, distinct from — and additive to — the existing execution
  breakdowns.

## Already Implemented (out of scope)

These pieces are done; this spec does not touch them:

- Recursive spec progress: `internal/spec/progress.go` (`NodeProgress`,
  `TreeProgress`), exposed via `GET /api/specs/tree`, rendered by
  `ui/js/spec-mode.js`, live via `GET /api/specs/stream`.
- Board execution cost model: `TaskUsage`, `TurnUsageRecord`,
  `UsageBreakdown map[SandboxActivity]TaskUsage` in
  `internal/store/models.go`.
- Board execution analytics: `GET /api/stats`
  (`internal/handler/stats.go`) with `ByWorkspace`, `ByActivity`,
  `ByStatus`, `ByFailureCategory`, top-tasks, and daily rollup; `GET /api/usage`
  (`internal/handler/usage.go`) with `ByStatus` / `BySubAgent`.
- Sandbox activity enum: the `planning` `SandboxActivity` constant already
  exists and is routable, but nothing writes usage under it yet.

## Shape of the Solution

Planning is its own track. A round is not a task — no board state, no
worktree, no commit lifecycle — so planning usage is a new *record type*
keyed by workspace group, with no coupling to the `Task` record. It still
lives in `internal/store/` (that's the persistence package), just in its
own file alongside the existing per-task storage, reusing the atomic-write
and JSON helpers already there. "Shoehorn into a synthetic `Task`" was
considered and rejected as a category error that would force
`kind = "planning"` filters into every task-facing read path just to
inherit an append-file helper.

### Decision 1 — Per-round record shape

**Reuse `TurnUsageRecord`.** The fields are identical in meaning for a
planning round: `Turn` is the round number, `Timestamp` is when the round
happened, the token/cost fields map one-for-one, `StopReason` uses the
same set, `Sandbox` names the runtime, and `SubAgent` is fixed to
`"planning"`. No new type, no duplicated JSON shape, no duplicated cost
math — existing helpers that already sum `TurnUsageRecord`s work as-is.

A separate `PlanningRoundUsage` type was considered for "decoupled
evolution" but rejected: the fields genuinely match today, and a future
task-specific field on `TurnUsageRecord` would be a smell even for tasks
(the right move would be to split the record, not fork planning). The
coupling cost here is real; the decoupling benefit is speculative.

### Decision 2 — File layout

**Option 2a — One file per group**:
`~/.wallfacer/planning/<group-key>/usage.jsonl`. Parallels how planning
sessions are already scoped (the planning sandbox container is per group).

- Pro: Matches session lifecycle. Easy to delete/inspect per group. No
  group-key field needed inside each record.
- Con: `<group-key>` must be path-safe — either hash-derived or URL-encoded.

**Option 2b — Single file with `group_key` column**:
`~/.wallfacer/planning/usage.jsonl`, each line carries its own
`group_key`.

- Pro: One file, one append path, one retention sweep.
- Con: All groups share the same file — larger reads per stats call,
  coarser deletion semantics.

### Decision 3 — Where the read/write API lives inside `internal/store/`

**Option 3a — Flat in `internal/store/`.** A new `planning_usage.go`
alongside the existing task-storage files, exporting
`AppendPlanningUsage(groupKey, record)` and
`ReadPlanningUsage(groupKey, since) []record`. Both `internal/planner/`
(writes) and `internal/handler/stats.go` (reads) import from `store`, as
they already do for tasks.

- Pro: No new package. Sits next to peer persistence code and reuses its
  atomic-write / JSON helpers. Smallest diff.
- Con: `internal/store` continues to grow; acceptable if the addition is
  small (~one file + tests).

**Option 3b — `internal/store/planningusage/` sub-package.** Same code
behind a small namespace fence. The sub-package owns the file format and
aggregation; callers import `store/planningusage`.

- Pro: Explicit separation from task storage; easier to evolve
  independently if the format grows.
- Con: A new package for what is likely ~200 lines; premature fencing.

### UI surface (orthogonal to the above)

Regardless of how the store is built, planning cost lands in the UI as a
new "Planning" block in `modal-stats.js` — a sibling to the existing
`ByWorkspace` / `ByActivity` sections — listing one row per workspace group
with tokens, cost, and round count. The existing execution blocks are
untouched. Optionally, `/api/usage` also populates `BySubAgent["planning"]`
so the period-picker view in `usage-stats.js` reflects planning activity
alongside other sandbox activities; this is cheap and additive.

## Open Questions

1. 2a vs. 2b — per-group file or single file? Leaning 2a: lifecycle
   already aligns per group, and retention/deletion map naturally.
2. 3a vs. 3b — flat in `internal/store/` or a `store/planningusage/`
   sub-package? Leaning 3a: one file keeps the footprint minimal and the
   code stays next to the existing persistence helpers; promote to 3b only
   if the format grows.
3. Timeline vs. totals: do we show per-round sparklines in the focused
   planning view, or only cumulative tokens/cost per group in the stats
   modal? Either works — each record has `Timestamp`.
4. Retention policy: planning JSONL has no task tombstone to ride; pick a
   retention window (mirror `WALLFACER_TOMBSTONE_RETENTION_DAYS`, or a
   separate knob) and compaction strategy.

## Affects

- `internal/planner/` — capture per-round usage from the agent exec result
  and call into `internal/store/` to persist it.
- `internal/handler/planning.go` — wire usage capture into the message
  round handler.
- `internal/store/` — new `planning_usage.go` (or `planningusage/`
  sub-package, per Decision 3) with append/read/aggregate for planning
  usage records. Independent of the `Task` record but shares the
  package's existing atomic-write and JSON helpers.
- `internal/handler/stats.go` — additive only: append a `Planning` section
  (keyed by workspace group) to `StatsResponse`. No changes to
  `ByWorkspace` / `ByActivity` / `ByStatus` buckets.
- `internal/handler/usage.go` — optional: surface `planning` in
  `BySubAgent` alongside existing activities.
- `ui/js/modal-stats.js` — render the new `Planning` block beside the
  existing execution breakdowns; the execution blocks stay unchanged.
- `ui/js/usage-stats.js` — optional: add a planning tile to the
  period-picker view if `BySubAgent` is populated.
