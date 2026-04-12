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

## Options

### Planning Usage Capture

**Option A — Synthetic planning task per session.** On first message in a
group, create (or reuse) a `Task` with `kind = "planning"` whose
`WorktreePaths` mirror the group's workspaces. Each round writes a
`TurnUsageRecord` to that task; `UsageBreakdown[planning]` accumulates.
The task is hidden from the kanban board (`GET /api/tasks` filters
`kind = "planning"`) and from task-centric analytics. The only visible
surface is a new planning section in the stats dashboard.

- Pro: Reuses `TurnUsageRecord` plumbing, per-turn storage, retention, and
  index rebuild paths unchanged. The `planning` activity slot already exists.
- Con: A task-shaped record for something that isn't a task. Needs filters
  in task list / stats-by-activity endpoints to avoid polluting the
  already-solved execution views.

**Option B — Dedicated per-group planning usage store.** New file
`~/.wallfacer/planning/<group-key>/usage.jsonl`, one
`TurnUsageRecord`-shaped entry per round. A small loader in
`stats.go` reads these and attaches a `Planning map[groupKey]UsageStat`
section to the response. Completely separate from `store.Task` and from
the existing execution aggregation code path.

- Pro: Zero risk of leaking into the solved execution analytics. Keyed
  natively by `GroupKey`. Storage and retention are self-contained.
- Con: New file format, new retention policy, new rebuild-index story for
  something close to (but not the same as) an existing type.

### Where Planning Cost Surfaces in the UI

**Option X — New "Planning" block in `modal-stats.js`.** A sibling to
`ByWorkspace` / `ByActivity`, listing one row per workspace group with
tokens, cost, and round count. `ByWorkspace` (paths) and `ByActivity` are
untouched. `usage-stats.js` gets a matching tile in its period-picker view.

- Pro: Additive only; zero change to the existing execution rollup or its
  UI. Easy to A/B hide behind a flag.
- Con: Two places with different grouping semantics (paths vs. groups) —
  users need to understand the distinction from the label.

**Option Y — Weave planning into the existing `BySubAgent` row.** The
`planning` activity bucket already exists; just make sure planning usage
from Option A/B flows into `BySubAgent["planning"]` in
`usage.go::GetUsageStats`. No new UI block.

- Pro: Minimal UI surface; piggybacks on existing rendering.
- Con: No workspace-group granularity — the feedback explicitly asks for
  per-group aggregation, so this alone is insufficient. Viable only as a
  supplement to Option X.

## Open Questions

1. Option A or B for capture? Leaning A — per-turn storage plumbing is
   already there and the `planning` activity slot is already reserved; the
   only real work is filtering synthetic tasks out of the
   already-solved execution views.
2. Whether to also populate `BySubAgent["planning"]` in `/api/usage`
   (Option Y on top of Option X). Cheap with Option A, essentially free.
3. Timeline vs. totals: do we show per-round sparklines in the focused
   planning view, or only cumulative tokens/cost per group in the stats
   modal? Timeline is easy if Option A is chosen, since `TurnUsageRecord`
   already carries `Timestamp`.
4. Retention: if Option A, planning rides task-tombstone retention; if
   Option B, we need an explicit retention policy for
   `~/.wallfacer/planning/<group-key>/usage.jsonl`.

## Affects

- `internal/planner/` — capture per-round usage from the agent exec result.
- `internal/handler/planning.go` — wire usage capture into the message
  round handler.
- `internal/store/` — Option A: write `TurnUsageRecord`s under a synthetic
  planning task and filter `kind = "planning"` from task-list reads.
  Option B: new `planning` sub-package with a JSONL usage store.
- `internal/handler/stats.go` — additive only: append a `Planning` section
  (keyed by workspace group) to `StatsResponse`. No changes to
  `ByWorkspace` / `ByActivity` / `ByStatus` buckets.
- `internal/handler/usage.go` — optional: surface `planning` in
  `BySubAgent` alongside existing activities.
- `ui/js/modal-stats.js` — render the new `Planning` block beside the
  existing execution breakdowns; the execution blocks stay unchanged.
- `ui/js/usage-stats.js` — optional: add a planning tile to the
  period-picker view if `BySubAgent` is populated.
