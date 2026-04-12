---
title: Planning Tile in usage-stats.js
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/progress-cost-tracking/usage-planning-merge.md
  - specs/local/spec-coordination/spec-planning-ux/progress-cost-tracking/planning-window-config.md
affects:
  - ui/js/usage-stats.js
  - ui/js/tests/usage-stats.test.js
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Planning Tile in usage-stats.js

## Goal

Surface planning usage in the `usage-stats.js` period-picker view so
`BySubAgent["planning"]` renders alongside the other sandbox
activities, and the picker's default window honors
`WALLFACER_PLANNING_WINDOW_DAYS`.

## What to do

1. In `ui/js/usage-stats.js`, inspect the existing `renderStats` /
   `by_sub_agent` rendering path. If it already iterates
   `data.by_sub_agent` as a generic map, verify `planning` renders
   without special-casing. If the renderer has a hard-coded label
   table, add a `planning` entry with a sensible display name (e.g.
   "Planning").
2. On modal open (or stats initialization), read
   `planning_window_days` from `/api/config` and set the period
   selector's default value accordingly. If the server returns `0`,
   select the "All time" option. Fall back to the current hard-coded
   default if the config value is missing.
3. No structural changes to the layout — the planning row slots into
   the existing `by_sub_agent` table.

## Tests

Extend `ui/js/tests/usage-stats.test.js`:

- `renders planning row in by_sub_agent table` — feed mock
  `/api/usage` response with a `planning` entry; assert the row and
  its label appear.
- `defaults period selector from config` — mock `/api/config` to
  return `planning_window_days = 30`; assert the selector's initial
  value is `30`.
- `falls back when config missing` — mock `/api/config` without the
  field; assert the existing hard-coded default is used.
- `period change refetches /api/usage` — change the selector; assert
  `fetch` is called with `?days=<new>`.

## Boundaries

- Do not modify the `/api/usage` server logic; that's the sibling
  task.
- Do not introduce new response fields or change the
  `by_sub_agent` schema.
- Do not touch `modal-stats.js`; its block is a sibling task.
- Do not add sparklines here — timelines belong in the stats modal,
  not the period-picker tile.
