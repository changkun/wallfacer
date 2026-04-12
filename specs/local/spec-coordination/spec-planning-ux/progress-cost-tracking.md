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
  - internal/workspace/groups.go
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

How is planning sandbox cost recorded, aggregated, and surfaced in the existing
usage analytics? Planning happens in a long-lived sandbox that the user drives
conversationally — each user message triggers one agent turn ("round") that
may read many files and edit any subset of the spec tree. Unlike kanban tasks,
planning has no `Task` record to attach `TaskUsage` to, so per-turn token and
cost data needs a dedicated capture path.

Decisions baked in by feedback:

- **Unit of attribution is the message round**, not the spec file. Each round
  records one cost entry regardless of how many specs the round modifies.
  Splitting a round's cost across multiple specs is explicitly out of scope.
- **Aggregation key is the workspace group**, matching how planning sessions
  are scoped (`internal/workspace/groups.go::GroupKey`). A planning session
  belongs to exactly one group; per-round costs roll up to that group.
- **Reporting surface is the existing usage analytics** (`GET /api/stats`,
  `GET /api/usage`, and the modal-stats / usage-stats dashboards). Planning
  cost is a new row/column in those views, not a separate dashboard.

Recursive spec progress (`done/total` leaf counts) is already implemented and
exposed; see "Already Implemented" below. This spec is now scoped to planning
cost only.

## Already Implemented

These pieces are done and not part of this spec's remaining work:

- `internal/spec/progress.go` — `NodeProgress`, `TreeProgress` compute
  recursive `done/total` leaf counts across the spec tree.
- `GET /api/specs/tree` (`internal/handler/specs.go::GetSpecTree`) returns
  `{Nodes, Progress: map[string]Progress}` keyed by spec path; the server is
  the single source of truth for progress numbers.
- `ui/js/spec-mode.js` renders the recursive progress badges from this
  response; the SSE stream at `GET /api/specs/stream` keeps them live.
- The `planning` `SandboxActivity` constant already exists in
  `internal/store/models.go` and is routable via
  `WALLFACER_SANDBOX_IMPLEMENTATION`-style env vars, but nothing currently
  writes `TaskUsage` rows under it for the planning sandbox.

## Context

Planning sandbox (`internal/planner/`, `internal/handler/planning.go`) runs a
persistent container per workspace group. User messages arrive via
`POST /api/planning/messages`; each call triggers one agent exec turn. Token
usage for these turns is currently discarded — the planner emits SSE tokens
but does not persist a usage record.

The existing cost model in `internal/store/models.go`:

- `TaskUsage{InputTokens, OutputTokens, CacheReadInputTokens, CacheCreationTokens, CostUSD}`
- `TurnUsageRecord{Turn, Timestamp, tokens…, CostUSD, StopReason, Sandbox, SubAgent}`
- `Task.UsageBreakdown map[SandboxActivity]TaskUsage`

The existing analytics surface:

- `GET /api/stats` (`internal/handler/stats.go`) returns `StatsResponse` with
  `ByWorkspace map[string]UsageStat` keyed by individual workspace paths
  pulled from each task's `WorktreePaths`. It does not yet aggregate by
  workspace group key.
- `GET /api/usage` (`internal/handler/usage.go`) returns aggregate totals plus
  `ByStatus` and `BySubAgent` breakdowns.
- `ui/js/modal-stats.js` renders `ByWorkspace` as a list of per-path cards;
  `ui/js/usage-stats.js` renders a simpler period-picker view.

The gap: planning cost has no storage, no aggregation layer, no dashboard row.

## Options

### Cost Storage

**Option A — Attach to a synthetic planning "task" per session.** For each
planning session (per workspace group), create or reuse a `Task` record with
`kind = "planning"` and no kanban state. Each round writes a
`TurnUsageRecord` to that task, and `UsageBreakdown[planning]` accumulates.
The `ByWorkspace` aggregation then picks it up for free once the task's
`WorktreePaths` are set to the group's paths.

- Pro: Reuses `TaskUsage`, `TurnUsageRecord`, and the per-turn storage pipeline
  unchanged. The `planning` `SandboxActivity` slot already exists.
- Con: A task-shaped record for something that isn't a task is a light abuse of
  the model. Must hide the synthetic task from the kanban board and task list
  endpoints (filter by `kind = "planning"`).

**Option B — Dedicated planning usage store.** New file
`~/.wallfacer/planning/<group-key>/usage.jsonl`, one `TurnUsageRecord`-shaped
line per round. `stats.go` loads these alongside task usage when building
`ByWorkspaceGroup`.

- Pro: Clean separation; planning storage is purpose-built and keyed directly
  by `GroupKey`. No synthetic tasks to hide.
- Con: Two usage data sources to merge at read time. Separate retention, backup,
  and rebuild-index logic. A new file format for something close to an
  existing type.

### Aggregation Key

**Option X — Add `ByWorkspaceGroup` alongside `ByWorkspace`.** Extend
`StatsResponse` with a new `ByWorkspaceGroup map[string]UsageStat` keyed by
`GroupKey(sortedPaths)` (or a stable group name). Populate from both task
usage (by resolving each task's `WorktreePaths` to the group it belongs to)
and planning usage. Keep `ByWorkspace` for backwards compatibility.

- Pro: Non-breaking. Matches the user's scoping model for planning. The UI can
  show both breakdowns.
- Con: Two parallel breakdowns in the response; UI has to pick a default.

**Option Y — Replace `ByWorkspace` with group-keyed buckets.** Change the
existing bucket to group by `GroupKey` only. Individual-path breakdowns go
away.

- Pro: One consistent bucketing scheme.
- Con: Breaks consumers of the current `ByWorkspace` shape (modal-stats.js
  keys off paths). Loses per-path visibility for multi-repo groups where users
  might still want it.

## Open Questions

1. Option A or B for storage? Leaning A — the `planning` activity slot and
   turn-record plumbing are already there, and a filter on `kind = "planning"`
   in the task list endpoints is cheap.
2. Option X or Y for aggregation? Leaning X — additive is safer and the UI
   can promote the group view to the default without removing per-path data.
3. Where does planning cost appear in the UI — as a new row in modal-stats
   (alongside `ByWorkspace` / `ByActivity`), as a new tile in usage-stats,
   or both?
4. Do we show cumulative planning cost only, or also a per-round timeline
   (small sparkline in the focused planning view)? Timeline is easy if we use
   Option A since `TurnUsageRecord` already carries timestamps.
5. Retention: planning usage piggybacks on task tombstone retention if we
   pick Option A; otherwise we need a separate retention policy.

## Affects

- `internal/planner/` — capture per-round usage from the agent exec result and
  hand it to the store.
- `internal/store/` — either write `TurnUsageRecord` entries under a synthetic
  planning task (Option A) or add a dedicated planning usage store (Option B).
- `internal/handler/planning.go` — wire the usage capture into the message
  handler's turn loop.
- `internal/handler/stats.go` — add `ByWorkspaceGroup` to `StatsResponse` and
  include planning usage in the aggregation.
- `internal/handler/usage.go` — include planning activity in the period
  rollup; confirm `BySubAgent` surfaces it.
- `internal/workspace/groups.go` — expose `GroupKey` / a stable group name
  helper for stats bucketing if not already public-enough.
- `ui/js/modal-stats.js` — render `ByWorkspaceGroup` and a planning row in
  `ByActivity`.
- `ui/js/usage-stats.js` — ensure the `BySubAgent` list shows `planning`.
