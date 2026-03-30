---
title: Progress & Cost Tracking
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-explorer.md
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox.md
affects:
  - ui/js/
  - internal/handler/
  - internal/spec/
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Progress & Cost Tracking

## Design Problem

How are recursive progress aggregation and per-spec cost attribution implemented across the spec explorer, focused view, and cost dashboard? The parent spec decides that progress is shown as `done/total` leaf counts at every tree level, and costs are tracked per spec file (split between planning cost and execution cost). These must compose across arbitrarily deep spec trees and aggregate up to non-leaf specs.

Key constraints:
- Progress is recursive: a non-leaf spec's count includes all leaves in its entire subtree, not just direct children
- Cost is attributed per spec file, per round, proportionally when multiple specs are modified in one round
- Both planning cost (tokens spent iterating on the spec) and execution cost (tokens spent by dispatched tasks) must be shown side-by-side
- Non-leaf specs aggregate both cost types from their subtree
- The cost dashboard (existing) extends to show planning cost alongside execution cost
- Multi-repo spec forests must aggregate correctly across repos

## Context

The existing progress tracking in `internal/spec/progress.go` already computes recursive leaf counts. `spec.Progress(node)` returns `(done, total)` by walking the subtree and counting leaves with `status: complete`. This is computed on the fly — no separate storage.

The existing cost tracking lives in `internal/store/models.go`:
- `TaskUsage` — accumulated input/output tokens, cache tokens, and cost
- `UsageBreakdown` — per-activity cost attribution (implementation, testing, refinement, title, oversight, etc.)
- `TurnUsageRecord` — per-turn usage snapshots

The planning session introduces a new cost dimension: tokens spent in the planning sandbox, attributed to spec files rather than tasks. This doesn't fit the existing per-task model.

## Options

### Cost Attribution

**Option A — Extend TaskUsage with a planning activity.** Add `SandboxActivityPlanning` to the activity enum. Planning cost is tracked as a "planning task" in the existing usage system. Per-spec attribution is a layer on top: the planning session logs which spec was focused or modified per round, and the UI aggregates.

- Pro: Reuses existing usage tracking infrastructure. The cost dashboard already shows per-activity breakdowns.
- Con: Planning isn't a task — shoehorning it into the task model creates confusion. Per-spec attribution (splitting a round's cost across multiple modified specs) is new logic on top.

**Option B — Separate planning cost store.** A new `~/.wallfacer/planning/<fingerprint>/costs.json` tracks per-round, per-spec cost entries. Each entry records: round number, focused spec, modified specs, tokens used, cost. The UI reads this alongside task usage data.

- Pro: Clean separation. Planning cost model is purpose-built for per-spec attribution. No task model pollution.
- Con: Two cost data sources. The cost dashboard must merge them. Separate storage means separate backup/retention.

**Option C — Spec frontmatter cost field.** Each spec file carries a `planning_cost` field in its frontmatter, updated by the planning agent after each round. Execution cost comes from the linked task's `TaskUsage`.

- Pro: Cost lives with the spec. No separate store. The spec file is the single source of truth for planning cost.
- Con: Frequent frontmatter writes for cost updates. Clutters the frontmatter with operational data. Doesn't break down per-round — only shows cumulative cost.

### Progress Display

**Option X — Server-computed, pushed via spec tree API.** The spec tree API (from the spec-explorer sub-design) includes `progress: {done, total}` for every node. The server computes this using `spec.Progress()`. Updates are pushed when specs change status.

- Pro: Single source of truth. Frontend just renders the numbers. Computation is cheap (tree walk).
- Con: Requires spec tree API to refresh on every status change.

**Option Y — Client-computed from spec tree data.** The frontend receives the full spec tree (with status per node) and computes progress locally. No server-side progress calculation needed.

- Pro: No server round-trip for progress updates. Instant re-computation when the UI receives a status change.
- Con: Duplicates the progress algorithm in JS. Must stay in sync with the server-side implementation.

## Open Questions

1. How is per-spec cost attribution split when a round modifies multiple specs? Options: equal split, proportional to characters changed, proportional to tokens spent reading each file, or attribute to the focused spec only.
2. Should the focused view show a cost breakdown timeline (cost per round over time) or just cumulative totals?
3. How does cost aggregate across multi-repo spec forests? Per-repo subtotals, or a single global total?
4. Should the spec explorer show cost alongside progress (e.g., "4/6 · $2.54") or only in the focused view?
5. How does planning cost integrate with the existing `GET /api/usage` and `GET /api/stats` endpoints?

## Affects

- `internal/spec/progress.go` — already implemented; may need JSON serialization for API response
- `internal/handler/` — spec tree API includes progress data; cost data endpoint
- `internal/store/` or `~/.wallfacer/planning/` — planning cost storage
- `ui/js/` — spec explorer progress badges, focused view cost display, cost dashboard integration
- `ui/js/` — existing cost dashboard extended with planning cost column
