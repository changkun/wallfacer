---
title: Add SandboxActivityPlanning constant
status: validated
depends_on: []
affects:
  - internal/store/models.go
effort: small
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Add SandboxActivityPlanning constant

## Goal

Add the `SandboxActivityPlanning` constant to the sandbox activity enum so the planning sandbox can be routed, tracked, and attributed separately from task execution activities.

## What to do

1. In `internal/store/models.go`, add a new `SandboxActivityPlanning` constant:
   ```go
   SandboxActivityPlanning SandboxActivity = "planning"
   ```
   Add it after `SandboxActivityIdeaAgent` in the const block.

2. Append `SandboxActivityPlanning` to the `SandboxActivities` slice (the slice that controls sandbox routing eligibility).

3. In `internal/store/models.go`, add a new `TaskKindPlanning` constant to the `TaskKind` type:
   ```go
   TaskKindPlanning TaskKind = "planning"
   ```
   This parallels `TaskKindIdeaAgent` — planning tasks are a distinct kind that the auto-promoter and UI can filter on.

## Tests

- `TestSandboxActivityPlanning`: Verify `SandboxActivityPlanning` is in the `SandboxActivities` slice.
- `TestTaskKindPlanning`: Verify `TaskKindPlanning` is a valid TaskKind constant and can be stored/retrieved on a Task.

## Boundaries

- Do NOT add any handler, runner, or planner logic — this task only adds constants.
- Do NOT modify sandbox routing logic — routing will be wired in a later task.
- Do NOT add UI changes.
