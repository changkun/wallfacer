---
title: Merge Planning Into /api/usage BySubAgent
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/progress-cost-tracking/planning-usage-store.md
affects:
  - internal/handler/usage.go
  - internal/handler/usage_test.go
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Merge Planning Into /api/usage BySubAgent

## Goal

`/api/usage` already aggregates task usage by sandbox activity into
`BySubAgent`. Extend it so planning usage (across all workspace groups)
flows into `BySubAgent["planning"]` and into the `Total`, respecting the
existing `?days=` window.

## What to do

1. In `internal/handler/usage.go`, right after the task-usage loop at
   L70–L74:
   - List `<root>/planning/` subdirectories. Each is a group key.
   - For each, call `store.ReadPlanningUsage(root, groupKey, cutoff)`
     with the same `cutoff` used for tasks (`time.Now().UTC().AddDate(0, 0, -days)`
     when `days > 0`; otherwise `time.Time{}`).
   - Sum the returned records into a single `TaskUsage` value (or
     whatever type `BySubAgent` already maps to; check
     `usageResponse`'s map signature). Merge that into
     `BySubAgent[store.SandboxActivityPlanning]` via the existing
     `addUsage` helper.
2. Also add the planning totals into the top-level `Total` so the
   summary number matches `sum(BySubAgent)`.
3. Leave `TaskCount` alone — it counts tasks, not planning rounds.

## Tests

Extend `internal/handler/usage_test.go`:

- `TestUsage_NoPlanningRecords` — no planning dir; response is
  identical to pre-change behavior (no `planning` key in
  `BySubAgent`).
- `TestUsage_PlanningMergedIntoBySubAgent` — seed records for one
  group; assert `BySubAgent["planning"]` reflects the summed
  tokens/cost and `Total` grew accordingly.
- `TestUsage_PlanningRespectsDaysWindow` — seed old + new records;
  query with `?days=1`; assert only recent records appear in
  `BySubAgent["planning"]`.
- `TestUsage_PlanningAcrossMultipleGroups` — seed two group dirs;
  assert `BySubAgent["planning"]` is the sum across groups.
- `TestUsage_TaskCountUnchangedByPlanning` — seed planning records
  with zero task data; assert `TaskCount == 0`.

## Boundaries

- No new top-level response fields. Only
  `BySubAgent[planning]` + `Total` change.
- Do not change the `BySubAgent` map key type or JSON key format.
- Do not touch `/api/stats` in this task.
- Do not add a new env var; reuse `?days=`.
