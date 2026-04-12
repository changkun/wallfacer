---
title: Planning Cost Tracking
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox.md
affects:
  - internal/planner/
  - internal/planningusage/
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
worktree, no commit lifecycle — so planning usage belongs in its own store,
keyed by workspace group, with no coupling to `store.Task`. The three real
decisions are below; "shoehorn into a synthetic task" was considered and
rejected as a category error that would force `kind = "planning"` filters
into every task-facing read path just to reuse an append-file helper.

### Decision 1 — Per-round record shape

**Option 1a — Reuse the `TurnUsageRecord` struct as the on-disk schema.**
Tokens, `CostUSD`, `Timestamp`, `Sandbox`, `StopReason` are all already
modeled; the only field without a natural meaning is `Turn` (reinterpret as
round number) and `SubAgent` (fix to `"planning"`).

- Pro: Zero new types. Existing JSON marshaling and cost math apply.
- Con: Type-level coupling to a kanban-shaped struct; if either side evolves
  (e.g., task-specific fields get added to `TurnUsageRecord`), planning
  drags them along or has to branch.

**Option 1b — New `PlanningRoundUsage` type.** Purpose-built:
`{Round, Timestamp, GroupKey, Model, Tokens, CacheTokens, CostUSD, StopReason}`.

- Pro: Decoupled evolution. The type says what it is.
- Con: A near-duplicate struct and a second JSON shape for something cost
  aggregators already know how to read.

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

### Decision 3 — Read path into analytics

**Option 3a — Loader owned by `internal/planner/`.** The planner owns write
AND read; `stats.go` imports a `planner.AggregateUsage(groupKey, since)`
helper and stitches the result into the response.

- Pro: Planning logic lives in one package. Thin coupling at the handler.
- Con: `stats.go` now depends on `internal/planner/`.

**Option 3b — New `internal/planningusage/` (or `internal/store/planning/`)
sub-package.** Owns the file format and aggregation. Both the planner (for
writes) and `stats.go` (for reads) depend on it; neither depends on the
other.

- Pro: Clean separation; reusable if planning usage is ever needed
  elsewhere (CLI, exports, alerts).
- Con: One more package for a small amount of code.

### UI surface (orthogonal to the above)

Regardless of how the store is built, planning cost lands in the UI as a
new "Planning" block in `modal-stats.js` — a sibling to the existing
`ByWorkspace` / `ByActivity` sections — listing one row per workspace group
with tokens, cost, and round count. The existing execution blocks are
untouched. Optionally, `/api/usage` also populates `BySubAgent["planning"]`
so the period-picker view in `usage-stats.js` reflects planning activity
alongside other sandbox activities; this is cheap and additive.

## Open Questions

1. 1a vs. 1b — reuse `TurnUsageRecord` or define `PlanningRoundUsage`?
   Leaning 1b: the decoupling cost is one struct, and it prevents a future
   task-shaped field from bleeding into planning storage.
2. 2a vs. 2b — per-group file or single file? Leaning 2a: lifecycle already
   aligns per group, and retention/deletion map naturally.
3. 3a vs. 3b — planner-owned loader or its own sub-package? Leaning 3b for
   separation of concerns; 3a is acceptable if the footprint stays small.
4. Timeline vs. totals: do we show per-round sparklines in the focused
   planning view, or only cumulative tokens/cost per group in the stats
   modal? Either works — each record has `Timestamp`.
5. Retention policy: planning JSONL has no task tombstone to ride; pick a
   retention window (mirror `WALLFACER_TOMBSTONE_RETENTION_DAYS`, or a
   separate knob) and compaction strategy.

## Affects

- `internal/planner/` — capture per-round usage from the agent exec result
  and persist it.
- `internal/handler/planning.go` — wire usage capture into the message
  round handler.
- `internal/planningusage/` (new, if 3b) — write/read/aggregate planning
  usage records; completely independent of `store.Task`.
- `internal/handler/stats.go` — additive only: append a `Planning` section
  (keyed by workspace group) to `StatsResponse`. No changes to
  `ByWorkspace` / `ByActivity` / `ByStatus` buckets.
- `internal/handler/usage.go` — optional: surface `planning` in
  `BySubAgent` alongside existing activities.
- `ui/js/modal-stats.js` — render the new `Planning` block beside the
  existing execution breakdowns; the execution blocks stay unchanged.
- `ui/js/usage-stats.js` — optional: add a planning tile to the
  period-picker view if `BySubAgent` is populated.
