---
title: Task Spec Source Field
status: validated
depends_on: []
affects:
  - internal/store/models.go
  - internal/store/
  - internal/handler/tasks.go
  - ui/js/generated/routes.js
effort: small
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Task Spec Source Field

## Goal

Add a `SpecSourcePath` field to the Task model so tasks dispatched from specs carry a reverse link back to their source spec. This enables the UI to render spec badges on task cards and "View Source Spec" links in the task modal.

## What to do

1. In `internal/store/models.go`, add `SpecSourcePath string` field to the `Task` struct (near the `Tags` field around line 312). Include JSON tag `json:"spec_source_path,omitempty"`.

2. In `internal/store/tasks_create_delete.go`, ensure `CreateTaskWithOptions` accepts and persists the `SpecSourcePath` field. The field should be part of the `TaskCreateOptions` struct if one exists, or passed through the Task struct directly.

3. In `internal/handler/tasks.go`, update `batchTaskInput` struct to include `SpecSourcePath string` field so the batch creation API can set it. Wire it through to `CreateTaskWithOptions`.

4. Run `make api-contract` to regenerate `ui/js/generated/routes.js` if needed (the field is on the Task model, not a new route, so this may be a no-op).

5. Verify the field survives task serialization/deserialization by checking `saveTask`/`loadTask` paths — since the Task struct uses JSON serialization, the new field should automatically persist.

## Tests

- `TestTaskSpecSourcePath_Persists` — create a task with `SpecSourcePath` set, reload from disk, verify field is preserved
- `TestTaskSpecSourcePath_EmptyByDefault` — create a task without setting the field, verify it's empty string
- `TestTaskSpecSourcePath_InBatchCreate` — create tasks via batch API with `SpecSourcePath`, verify field on returned tasks
- `TestTaskSpecSourcePath_InTaskJSON` — marshal/unmarshal a task with the field, verify round-trip

## Boundaries

- Do NOT add any spec-related API endpoints
- Do NOT add UI rendering changes (that's a separate task)
- Do NOT modify the task state machine or lifecycle
- Do NOT add validation that the spec path exists (the dispatch handler will handle that)
