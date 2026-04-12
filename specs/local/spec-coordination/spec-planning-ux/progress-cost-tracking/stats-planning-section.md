---
title: Planning Section in /api/stats
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/progress-cost-tracking/planning-usage-store.md
affects:
  - internal/handler/stats.go
  - internal/handler/stats_test.go
effort: medium
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Planning Section in /api/stats

## Goal

Extend `StatsResponse` with a new `Planning` field keyed by workspace
group, aggregated from the planning-usage store. Execution buckets
(`ByWorkspace`, `ByActivity`, `ByStatus`, `ByFailureCategory`, `TopTasks`,
`DailyUsage`) are not touched.

## What to do

1. Add a new field to `StatsResponse` in
   `internal/handler/stats.go` (around L15–L26):

   ```go
   Planning map[string]PlanningGroupStat `json:"planning"`
   ```

   Define `PlanningGroupStat` in the same file (or reuse `UsageStat` if
   the fields match). Suggested shape:

   ```go
   type PlanningGroupStat struct {
       Label      string       `json:"label"` // joined workspace basenames
       Paths      []string     `json:"paths"`
       Usage      UsageStat    `json:"usage"`
       RoundCount int          `json:"round_count"`
       Timeline   []RoundPoint `json:"timeline"` // for sparkline
   }
   type RoundPoint struct {
       Timestamp time.Time `json:"timestamp"`
       CostUSD   float64   `json:"cost_usd"`
       Tokens    int       `json:"tokens"`
   }
   ```

2. In `aggregateStats` (around L63–L186), after existing buckets are
   populated:
   - List subdirectories under `<root>/planning/`. Each dir name is a
     group key.
   - For each group key, call
     `store.ReadPlanningUsage(root, groupKey, since)` with the same
     cutoff the rest of the response already uses.
   - Reduce the returned records into a `PlanningGroupStat`: sum tokens
     and cost, count rounds, and emit one `RoundPoint` per record for
     the timeline.
   - Resolve `Paths` and `Label` from the active workspace-group
     registry so the UI can render a friendly name. If the key doesn't
     match any current group (stale data), set `Label` to the key and
     leave `Paths` empty.
3. Ensure the handler applies the existing `?days=` query param (if
   any) to planning aggregation as well — same `since` value for both
   task and planning rollups.

## Tests

Extend `internal/handler/stats_test.go`:

- `TestStats_PlanningEmpty` — no planning dir on disk; response has
  `Planning == map[string]PlanningGroupStat{}` (not nil).
- `TestStats_PlanningAggregation` — seed records for two group keys;
  assert each group's totals and `RoundCount`.
- `TestStats_PlanningRespectsSince` — seed records with old and new
  timestamps; query with a cutoff; assert old records are excluded
  from both totals and `Timeline`.
- `TestStats_PlanningTimelineOrder` — assert `Timeline` is
  chronologically ordered.
- `TestStats_ExecutionUnchanged` — run the test harness on a task set
  with no planning data; assert `ByWorkspace`, `ByActivity`,
  `ByStatus` are byte-equal to the pre-change baseline.

## Boundaries

- Do not modify `ByWorkspace`, `ByActivity`, `ByStatus`,
  `ByFailureCategory`, `TopTasks`, or `DailyUsage` — additive only.
- Do not touch `/api/usage` in this task (that's the sibling task).
- Do not render anything in the UI; that's the modal-stats task.
- Do not introduce a new env var for the cutoff; reuse the existing
  `?days=` mechanism.

## Implementation notes

- **`?days=N` is a new parameter on `/api/stats`**, not a pre-existing
  one. The spec said "reuse the existing `?days=` query param (if any)",
  but `/api/stats` did not accept any time-window param; only
  `/api/usage` did. The implementation adds `?days=N` to `/api/stats`
  and scopes it to the planning aggregation only. Execution buckets
  (`ByWorkspace`, `ByActivity`, etc.) deliberately ignore it to satisfy
  the additive-only boundary.
- **Aggregation lives in a separate function.** `aggregatePlanningStats`
  is a sibling to `aggregateStats`, not an inline extension. Keeping the
  two separate meant `aggregateStats`'s signature and behavior are
  unchanged, which was the simplest way to honor "execution buckets are
  not touched" at the test level (`TestGetStats_ExecutionUnchangedByPlanning`
  JSON-compares the two response shapes).
- **Active-group resolution is single-group.** `internal/workspace.Manager`
  exposes exactly one current group, so the handler passes
  `h.currentWorkspaces()` and marks only its key as "active." Past
  groups still on disk surface with `Label == key` and `Paths == nil`
  as specified.
- **Sibling doc updates**: added a `?days=N` note to the
  `/api/stats` row in `CLAUDE.md` and `docs/internals/api-and-transport.md`.
