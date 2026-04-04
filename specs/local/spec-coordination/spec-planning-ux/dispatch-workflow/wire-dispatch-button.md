---
title: Wire Dispatch Button in Spec Mode
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/dispatch-workflow/dispatch-api.md
affects:
  - ui/js/spec-mode.js
  - ui/js/generated/routes.js
effort: small
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Wire Dispatch Button in Spec Mode

## Goal

Implement the `dispatchFocusedSpec()` function in `ui/js/spec-mode.js` (currently a no-op stub at line 476) to call `POST /api/specs/dispatch` with the focused spec's path. Show loading state on the button, handle errors with user feedback, and refresh the spec tree on success.

## What to do

1. In `ui/js/spec-mode.js`, implement `dispatchFocusedSpec()` (line 476):
   - Guard: if `_focusedSpecPath` is null, return early
   - Set the dispatch button (`#spec-dispatch-btn`) to disabled/loading state
   - Call `POST /api/specs/dispatch` with `{paths: [_focusedSpecPath], run: false}`
   - Use the route constant from `routes.js` (will be generated after dispatch-api task runs `make api-contract`)
   - On success: show a brief success toast/notification, refresh the spec tree (trigger SSE reconnect or re-fetch), and update the focused spec view to reflect the new `dispatched_task_id`
   - On error: show error message to user, restore button state
   - Re-enable button after completion

2. Add a confirmation step before dispatching: show a small confirmation prompt ("Dispatch this spec to the task board?") since dispatch creates a task and modifies the spec file. Use the existing modal/dialog patterns in the codebase.

3. After successful dispatch, optionally offer to switch to board mode to see the created task (e.g., a "View on Board" link in the success notification).

## Tests

- Frontend test in `ui/js/tests/`: mock the fetch call to `POST /api/specs/dispatch`, verify `dispatchFocusedSpec()` sends the correct payload and handles success/error responses

## Boundaries

- Do NOT implement multi-select dispatch (separate task)
- Do NOT implement undispatch UI
- Do NOT modify the dispatch API endpoint
- Do NOT add keyboard shortcut changes (the `d` shortcut is already wired to `dispatchFocusedSpec()`)
