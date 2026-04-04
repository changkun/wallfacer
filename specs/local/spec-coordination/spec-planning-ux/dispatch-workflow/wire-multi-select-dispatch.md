---
title: Wire Multi-Select Dispatch in Spec Explorer
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/dispatch-workflow/dispatch-api.md
affects:
  - ui/js/spec-explorer.js
effort: small
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Wire Multi-Select Dispatch in Spec Explorer

## Goal

Implement `dispatchSelectedSpecs()` in `ui/js/spec-explorer.js` (currently a console.log stub at line 531) to call `POST /api/specs/dispatch` with all selected spec paths. The batch endpoint handles dependency wiring atomically.

## What to do

1. In `ui/js/spec-explorer.js`, implement `dispatchSelectedSpecs()` (line 531):
   - Guard: if `_selectedSpecPaths` is empty, return early
   - Collect paths from `_selectedSpecPaths` Set into an array
   - Show confirmation: "Dispatch N specs to the task board?"
   - Set the dispatch button (`#spec-dispatch-selected-btn`) to disabled/loading state
   - Call `POST /api/specs/dispatch` with `{paths: [...paths], run: false}`
   - On success: show success notification with count of dispatched specs, clear `_selectedSpecPaths`, uncheck all checkboxes, refresh spec tree
   - On error: show error details (the response includes per-spec errors), restore button state
   - Handle partial success: if some specs dispatched and others failed, show both success count and error details

2. After successful batch dispatch, update the `_updateDispatchSelectedButton()` to hide the button (since selection is cleared).

## Tests

- Frontend test: mock fetch, verify `dispatchSelectedSpecs()` sends correct paths array and handles success/partial-error responses

## Boundaries

- Do NOT modify the spec explorer's selection/checkbox logic
- Do NOT modify the dispatch API endpoint
- Do NOT implement single-spec dispatch (separate task)
