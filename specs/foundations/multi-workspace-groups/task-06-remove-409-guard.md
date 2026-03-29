---
title: "Remove 409 Blocking Check in UpdateWorkspaces"
status: complete
track: foundations
depends_on:
  - specs/foundations/multi-workspace-groups/task-02-modify-switch.md
  - specs/foundations/multi-workspace-groups/task-05-run-uses-taskstore.md
affects:
  - internal/handler/workspace.go
effort: small
created: 2026-03-27
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 6: Remove 409 Blocking Check in UpdateWorkspaces

## Goal

Remove the HTTP 409 guard that prevents workspace switching when tasks are
in progress. With multi-store support, switching is always safe.

## What to do

1. In `internal/handler/workspace.go`, remove the block at lines 147-158
   that checks for in-progress/committing tasks and returns 409.

2. The rest of `UpdateWorkspaces()` stays the same — it still calls
   `h.workspace.Switch()` and returns the updated config.

## Tests

- `TestUpdateWorkspacesAllowedDuringInProgress` — create a task with
  status `InProgress`, call `UpdateWorkspaces`, verify it succeeds
  (previously would return 409).
- Verify existing workspace switching tests still pass.

## Boundaries

- Do NOT change watchers or store subscriptions (tasks 7-8).
