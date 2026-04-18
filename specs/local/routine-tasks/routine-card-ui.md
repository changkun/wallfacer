---
title: Routine card UI variant
status: complete
depends_on:
  - specs/local/routine-tasks/routines-api.md
affects:
  - ui/js/routines.js
  - ui/js/render.js
  - ui/css/board.css
  - ui/partials/sidebar.html
effort: medium
created: 2026-04-18
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Routine card UI variant

## Goal

Render routine cards (`task.kind === "routine"`) with a distinct look and
inline schedule controls. Users can create a routine, edit its interval
and enabled state, trigger a run manually, and see a live countdown to
the next run — all from the board card itself. No new view or modal.

## What to do

1. Create `ui/js/routines.js` exporting:

   ```js
   export function renderRoutineCard(task) {
     // Returns an HTMLElement with:
     //   - routine badge chip
     //   - prompt (same as regular task card)
     //   - interval selector (1, 5, 15, 30, 60, 180 minutes; custom input)
     //   - enabled toggle (checkbox)
     //   - next-run countdown span (auto-updates via setInterval)
     //   - "Run now" button
     //   - last-fired timestamp (relative: "fired 3m ago")
   }

   export function createRoutineFromPrompt(prompt, { intervalMinutes, spawnKind }) {
     return fetch(ROUTES.createRoutine, { method: 'POST', ... });
   }
   ```

2. In `ui/js/render.js` (the existing card renderer), branch on
   `task.kind === "routine"` and delegate to `renderRoutineCard`.

3. In `ui/css/board.css`, add `.card.card--routine` styling — distinct
   border color or icon so routines are visually separable from regular
   tasks. Match existing card chrome structure; no layout churn.

4. Countdown updater: single page-level `setInterval(1000)` that rewrites
   all `.routine-next-run` spans. Formats as `in 3m 12s` → `fired just
   now` → "re-arming…" during the brief gap between fire and
   reconcile.

5. Inline editors submit via `PATCH /api/routines/{id}/schedule`. On
   optimistic failure, revert the control to the server value.

6. "Run now" button calls `POST /api/routines/{id}/trigger` and flashes
   a toast on success.

7. Board composer: add a small "+ Routine" button next to the existing
   "+ New task" action. Clicking opens an inline form (prompt +
   interval-minutes + enabled) that POSTs to `/api/routines`.

## Tests

- `ui/js/tests/routines.test.js`:
  - `renderRoutineCard` returns an element with a `.card--routine` class.
  - Interval selector shows configured minutes.
  - Enabled toggle reflects `task.routine_enabled`.
  - "Run now" button POSTs to the trigger endpoint (mock `fetch`).
  - Countdown formatter: `formatCountdown(60_000)` → `"in 1m 0s"`;
    `formatCountdown(0)` → `"fired just now"`; `formatCountdown(null)`
    → `"paused"`.
  - Patch-on-change: flipping the enabled toggle fires a PATCH with
    `{enabled: false}`.
  - Kind filter: `render.js` dispatches to `renderRoutineCard` only when
    `task.kind === "routine"`.

## Boundaries

- Do not replace the existing ideation settings panel in this task; that
  UI remains functional until ideation migrates to a system routine.
- No minimap/spatial-canvas changes.
- No SSE plumbing beyond what already exists — routine cards arrive via
  the standard `/api/tasks/stream` list-update messages.
- No cron-expression UI; v1 is interval minutes only.
- Do not persist any state in localStorage.
