---
title: Task Card Send to Plan Action
status: validated
depends_on:
  - specs/local/refinement-into-plan/message-schema-task-mode.md
affects:
  - ui/partials/task-detail-modal.html
  - ui/js/tasks.js
  - ui/js/spec-mode.js
effort: small
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Task Card Send to Plan Action

## Goal

Replace the card-level "Refine with AI" button with a "Send to Plan" action. Clicking it opens Plan mode in a task-mode thread pinned to the card's task, creating the thread if necessary.

## What to do

1. **Markup.** In the task card or detail modal (`ui/partials/task-detail-modal.html` and the backlog card template), swap the Refine button for a "Send to Plan" button. Keep the same visual placement.
2. **Helper.** Add `openPlanForTask(taskId)` in `ui/js/spec-mode.js` (or a shared Plan helper). Behavior:
   - Look for an existing non-archived task-mode thread pinned to `taskId`. If found, activate it.
   - Otherwise, call `POST /api/planning/threads` with `{name: "Task prompt: <title>", focused_task: taskId}` and activate the new thread.
   - Switch the UI into Plan mode.
3. **Wiring.** In `ui/js/tasks.js`, bind the new button to `openPlanForTask(task.id)`. Remove the previous Refine modal open handler only if it is safe to leave the modal code unreferenced (the full deletion happens in the retirement task); for now, leaving it unbound is fine.
4. **Button visibility.** The button is visible on tasks in `backlog` and `waiting`. Hidden on `in_progress`, `committing`, `done`, `failed`, `cancelled`, archived, and tombstoned tasks. (Matches the explorer's status set plus the card-detail visibility rule; in_progress is excluded because the lock would block writes anyway.)

## Tests

- `ui/js/tasks.test.js::sendToPlanButton_visibleStates` — rendered on backlog and waiting, hidden otherwise.
- `ui/js/spec-mode.test.js::openPlanForTask_reusesExistingThread` — if a non-archived task-mode thread exists for the task, it is reactivated rather than a new one being created.
- `ui/js/spec-mode.test.js::openPlanForTask_createsThreadWhenMissing` — posts to `/api/planning/threads` with the expected payload.

## Boundaries

- Do NOT remove `ui/js/refine.js` or the Refine modal partial here; retirement task owns that.
- Do NOT change server endpoints. Thread create already accepts `focused_task` from the message-schema task.
- Do NOT reorder other card action buttons beyond the swap.
