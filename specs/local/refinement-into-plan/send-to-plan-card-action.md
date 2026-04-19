---
title: Task Card Send to Plan Action
status: complete
depends_on:
  - specs/local/refinement-into-plan/explorer-task-prompts-section.md
affects:
  - ui/partials/task-detail-modal.html
  - ui/js/tasks.js
effort: small
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: eaad28c7-8434-4cec-b437-223a6cc0464c
---



# Task Card Send to Plan Action

## Goal

Replace the card-level "Refine with AI" button with a "Send to Plan" action. Clicking it invokes the `openPlanForTask` helper defined by the explorer spec.

## What to do

1. **Markup.** In the task card or detail modal (`ui/partials/task-detail-modal.html` and the backlog card template), swap the Refine button for a "Send to Plan" button. Keep the same visual placement.
2. **Wiring.** In `ui/js/tasks.js`, bind the new button to `openPlanForTask(task.id)` (defined by the explorer spec). Remove the previous Refine modal open handler only if it is safe to leave the modal code unreferenced (the full deletion happens in the retirement task); for now, leaving it unbound is fine.
3. **Button visibility.** The button is visible on tasks in `backlog` and `waiting`. Hidden on `in_progress`, `committing`, `done`, `failed`, `cancelled`, archived, and tombstoned tasks. (Matches the explorer's status set plus the card-detail visibility rule; in_progress is excluded because the lock would block writes anyway.)

## Tests

- `ui/js/tasks.test.js::sendToPlanButton_visibleStates` — rendered on backlog and waiting, hidden otherwise.
- `ui/js/tasks.test.js::sendToPlanButton_invokesHelper` — clicking the button calls `openPlanForTask` with the card's task id.

## Boundaries

- Do NOT remove `ui/js/refine.js` or the Refine modal partial here; retirement task owns that.
- Do NOT change server endpoints. Thread create already accepts `focused_task` from the message-schema task.
- Do NOT reorder other card action buttons beyond the swap.
- Do NOT redefine `openPlanForTask`; it is owned by the explorer spec. Import or reference, do not duplicate.
