---
title: TaskUsage cache-token undercount fix
status: archived
depends_on:
  - specs/local/backend-redundancy-cleanup.md
affects:
  - internal/handler/stats.go
  - internal/handler/usage.go
  - internal/store/models.go
  - internal/store/tasks_update.go
effort: small
created: 2026-06-01
updated: 2026-06-15
author: changkun
dispatched_task_id: null
---


# TaskUsage cache-token undercount fix

`internal/handler/usage.go:24-30` defines `addUsage(dst, src
*TaskUsage)` that sums all five usage fields (`InputTokens`,
`OutputTokens`, `CacheReadInputTokens`, `CacheCreationTokens`,
`CostUSD`).

`internal/handler/stats.go` accumulates the same data into a parallel
`UsageStat` type but inlines field-by-field `+=` at ~7 call sites
(`ByStatus`, `ByActivity`, `ByFailureCategory`, daily map, etc.).
**Most of those buckets drop `CacheReadInputTokens` and
`CacheCreationTokens` entirely**, so the stats response undercounts
cache token usage. That's a correctness bug, not just duplication.

`internal/store/tasks_update.go:197-216`
(`AccumulateSubAgentUsage`) inlines the same five-field update twice
in one function.

## Scope

1. Add `(*TaskUsage).Add(other TaskUsage)` on the `store.TaskUsage`
   type that sums all five fields. Move it into the `store` package so
   both handler and store callers can use it.
2. Promote the `UsageStat`/`DayStat` accumulation in
   `internal/handler/stats.go` to use the new helper. The fix is to
   either:
   - have `UsageStat` embed `store.TaskUsage` (preferred — the JSON
     shape stays compatible as long as field order is consistent), or
   - keep `UsageStat` separate and provide an `addUsageStat` helper
     that sums all five fields.
3. Migrate `AccumulateSubAgentUsage` in
   `internal/store/tasks_update.go` to call the new method.
4. Migrate `addUsage` in `internal/handler/usage.go` to call the new
   method and keep `addUsage` as a thin wrapper, or delete it
   entirely.

## Tests

- Regression test that creates a task, accumulates usage with non-zero
  cache fields, calls `GET /api/stats`, and asserts the cache token
  fields are non-zero in `ByStatus`, `ByActivity`,
  `ByFailureCategory`, and `DailyUsage` entries (the current code
  drops them).
- Unit test for `TaskUsage.Add` covering the all-zero, partial, and
  all-set cases.

## Out of scope

- Restructuring the `StatsResponse` JSON shape beyond preserving cache
  fields in every bucket.
- The `TopTasks` shape — it only carries cost, not tokens, which is
  intentional.
