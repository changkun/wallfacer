---
title: Explorer Task Prompts Virtual Section
status: validated
depends_on:
  - specs/local/refinement-into-plan/message-schema-task-mode.md
affects:
  - internal/handler/explorer.go
  - internal/apicontract/routes.go
  - ui/js/explorer.js
  - ui/js/spec-mode.js
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: 19bcfecf-9528-4cbb-b7fd-d032f08f94e1
---


# Explorer Task Prompts Virtual Section

## Goal

Expose backlog (and optionally waiting) tasks in the workspace explorer as virtual entries. Selecting an entry opens Plan in task mode pinned to that task.

## What to do

1. **New endpoint.** `GET /api/explorer/task-prompts?status=backlog,waiting` in `internal/handler/explorer.go`. Default status set is `backlog`; `waiting` is opt-in via query. `failed` and other states are rejected with 422. Returns `[{task_id, title, status, updated_at}]` sorted by `updated_at` desc.
2. **API contract.** Register the new route in `internal/apicontract/routes.go` so the generated `ui/js/generated/routes.js` picks it up.
3. **Explorer UI.** In `ui/js/explorer.js`, render a collapsible top-level section labelled "Task Prompts" above the workspace roots. Each entry shows the task title, a status badge, and the updated timestamp. Include a toggle on the section header to include `waiting` tasks.
4. **Selection routing and helper.** Define `openPlanForTask(taskId)` in `ui/js/spec-mode.js` (or a sibling Plan helper module). Behavior:
   - Look for an existing non-archived task-mode thread pinned to `taskId`. If found, activate it.
   - Otherwise, call `POST /api/planning/threads` with `{name: "Task prompt: <title>", focused_task: taskId}` and activate the new thread.
   - Switch the UI into Plan mode.

   Clicking a Task Prompts entry in the explorer invokes this helper with the entry's task id. The send-to-plan card action (sibling spec) reuses the same helper.
5. **Live updates.** Subscribe to the existing `/api/tasks/stream` SSE feed in the explorer tree code so the Task Prompts section refreshes when tasks are created, moved, or archived. No server changes needed for the feed itself.
6. **Breadcrumb.** Update `ui/js/spec-mode.js` so the Plan view header renders `Task Prompt · <title> (<status>)` when the pinned selection is a task. The existing `Spec · <path>` breadcrumb stays for file-mode threads.

## Tests

- `internal/handler/explorer_test.go::TestTaskPromptsEndpoint_DefaultBacklog` — returns backlog tasks by default, excludes waiting.
- `TestTaskPromptsEndpoint_WithWaiting` — `?status=backlog,waiting` includes both.
- `TestTaskPromptsEndpoint_RejectsFailed` — `?status=failed` returns 422.
- `TestTaskPromptsEndpoint_ExcludesArchivedAndTombstoned` — tasks soft-deleted or archived do not appear.
- `ui/js/explorer.test.js::taskPromptsSection_renders` — section renders given a mock response, includes the status toggle.
- `ui/js/explorer.test.js::taskPromptsSection_clickSelectsTask` — clicking an entry calls `openPlanForTask` with the right id.
- `ui/js/spec-mode.test.js::openPlanForTask_reusesExistingThread` — existing non-archived task-mode thread is reactivated rather than recreated.
- `ui/js/spec-mode.test.js::openPlanForTask_createsThreadWhenMissing` — posts to `/api/planning/threads` with the expected payload.

## Boundaries

- Do NOT add task-prompt entries to the spec explorer (`ui/js/spec-explorer.js`). This is the workspace explorer only.
- Do NOT persist the status toggle across sessions in this task; a simple in-memory toggle is fine.
- Do NOT implement the card-level "Send to Plan" button here; that lives in its own task.
