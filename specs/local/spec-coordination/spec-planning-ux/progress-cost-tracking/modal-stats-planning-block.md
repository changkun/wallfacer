---
title: Planning Block in modal-stats.js
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/progress-cost-tracking/stats-planning-section.md
  - specs/local/spec-coordination/spec-planning-ux/progress-cost-tracking/planning-window-config.md
affects:
  - ui/js/modal-stats.js
  - ui/js/tests/modal-stats.test.js
effort: medium
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Planning Block in modal-stats.js

## Goal

Render a new "Planning" section in the stats modal, sibling to the
existing `ByWorkspace` and `ByActivity` blocks. For each workspace
group, show tokens, cost, round count, and a per-round sparkline
driven by `Timestamp` from the stats endpoint.

## What to do

1. In `ui/js/modal-stats.js`:
   - Read `data.planning` from the `/api/stats` response (populated by
     the `Planning Section in /api/stats` task).
   - Add a `renderPlanning(data)` function that iterates
     `data.planning` and emits one row per group: `Label` (fall back
     to the key if empty), `usage.input_tokens`, `usage.output_tokens`,
     `usage.cache_*`, `usage.cost_usd`, `round_count`, and a sparkline.
   - Wire `renderPlanning` into the same render pipeline that already
     calls `renderByWorkspace` / `renderByActivity`. Add the new
     section markup next to the existing workspace/activity blocks.
2. Sparkline: each point is `{timestamp, cost_usd, tokens}`. Use an
   inline SVG polyline keyed on normalized `cost_usd` for the y-axis
   and index order for the x-axis. Reuse any existing sparkline
   utility if one lives in `ui/js/` already; otherwise keep it to ~40
   lines of vanilla SVG.
3. Seed the stats modal's period selector (if one is already wired)
   from `/api/config`'s `planning_window_days` as the default value.
   When the user changes the selector, request `/api/stats?days=N`
   and re-render; the planning block updates alongside the existing
   sections.
4. Escape all group labels and numeric formats through the existing
   HTML-escape helper used by `renderByWorkspace`.
5. Hide the Planning section entirely when `data.planning` is empty
   (no records across any group) rather than rendering an empty table.

## Tests

Extend `ui/js/tests/modal-stats.test.js` (create it if missing,
following the pattern of other `ui/js/tests/` files):

- `renders planning rows per group` — feed mock response with two
  groups; assert DOM has both group labels and their totals.
- `hides planning section when empty` — mock response with empty
  `planning`; assert the Planning section is not in the DOM.
- `renders sparkline SVG per group` — mock response with timeline of
  three points; assert an `<svg>` with a polyline of three points is
  emitted.
- `escapes HTML in group labels` — feed a label with `<script>`;
  assert it is rendered as text, not a DOM node.
- `reloads stats on period change` — mock the selector change event;
  assert `fetch` is called with `/api/stats?days=<new>` and the
  planning block re-renders.

## Boundaries

- Do not modify `renderByWorkspace`, `renderByActivity`,
  `renderByStatus`, or `renderByFailureCategory`.
- Do not touch `usage-stats.js`; its tile is a sibling task.
- Do not change the `/api/stats` endpoint or response shape.
- Do not add client-side aggregation of planning records — the server
  has already summed them; the UI renders what it receives.
