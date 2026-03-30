---
title: SSE Task State Integration
status: complete
depends_on:
  - specs/local/pixel-agents/task-05-renderer-and-view-toggle.md
  - specs/local/pixel-agents/task-08-character-manager.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 9: SSE Task State Integration

## Goal

Connect the office view to the existing SSE task stream so characters
appear, change state, and disappear in response to live task updates.

## What to do

1. In `ui/js/office/office.js`, subscribe to task state changes:
   - The existing `startTasksStream()` in `api.js` processes SSE events and
     updates the global `_state.tasks` array via reducer functions in
     `task-stream.js`
   - After each SSE-driven render cycle, the board calls `renderTasks()`.
     Hook into this same flow: when office is visible, also call
     `officeSync()` with the current task list.
   - Alternatively, add a lightweight observer: `onTasksChanged(callback)`
     that `task-stream.js` calls after applying snapshot/update/delete.
     Register the office sync as a listener.

2. Implement `officeSync(tasks)` in `office.js`:
   - Filter tasks to active (non-archived) — archived tasks don't get characters
   - Call `characterManager.syncTasks(tasks)`
   - If task count changed significantly (>= 2 new desks needed), regenerate
     layout via `generateOfficeLayout(taskCount)` and update renderer
   - Handle the initial snapshot: on first SSE `snapshot` event, generate
     the full office layout and populate all characters

3. Handle edge cases:
   - Office hidden when SSE event arrives: buffer the latest task list,
     apply it when office becomes visible (avoid wasted work)
   - Workspace switch: clear all characters and regenerate layout
   - Empty task list: show empty office (just furniture, no characters)

4. Wire into existing `api.js` / `task-stream.js`:
   - Add a `_taskChangeListeners` array in `state.js`
   - `registerTaskChangeListener(fn)` and `notifyTaskChangeListeners(tasks)`
   - Call `notifyTaskChangeListeners` at the end of `applyTasksSnapshot`,
     `applyTaskUpdated`, `applyTaskDeleted` in `task-stream.js`
   - `office.js` registers itself via `registerTaskChangeListener(officeSync)`

## Tests

- `office-sse.test.js`:
  - After `applyTasksSnapshot` with 3 tasks, office sync creates 3 characters
  - `applyTaskUpdated` with status change → character state updates
  - `applyTaskDeleted` → character enters DESPAWN
  - New task added via update → new character spawns
  - Task listener registration: callback is invoked on snapshot
  - When office is hidden, sync is deferred (no character updates)
  - Workspace switch clears all characters

## Boundaries

- Do NOT modify the SSE connection logic or EventSource handling
- Do NOT change how the board view processes task updates
- Do NOT implement spawn/despawn effects rendering (that's task 10)
- Keep the hook minimal: one array + notify pattern, no pub/sub framework
