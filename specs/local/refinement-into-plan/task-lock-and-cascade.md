---
title: Task Movement Lock and Thread Cascade
status: validated
depends_on:
  - specs/local/refinement-into-plan/update-task-prompt-tool.md
affects:
  - internal/handler/planning.go
  - internal/handler/tasks.go
  - internal/runner/
  - internal/store/tasks_update.go
  - internal/handler/planning_threads.go
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: 70a69655-79fb-4405-9f9e-e227c6cdb9b3
---


# Task Movement Lock and Thread Cascade

## Goal

Prevent a task from transitioning while a task-mode Plan turn is actively writing to it, and cascade thread archival when the task archives, deletes, or leaves backlog between turns. Mirrors the pre-spec refinement lock without stranding tasks behind idle threads.

## What to do

1. **In-flight tracking.** The planning handler already tracks in-flight turns per thread (needed for interrupt). Expose a helper `IsTaskLocked(taskID) bool` that returns true iff any task-mode thread currently has an in-flight turn pinned to that task.
2. **Auto-promoter guard.** The runner's auto-promoter skips tasks where `IsTaskLocked(taskID)` returns true. Log a debug entry naming the thread so users can diagnose.
3. **Manual transition guard.** In `PATCH /api/tasks/{id}` and the drag handler, reject state changes on a locked task with 409 and a body pointing at the thread id holding the lock.
4. **Cascade on leave-backlog.** When a task transitions out of `backlog` or `waiting` (either automatically or by user drag), auto-archive every non-archived task-mode thread pinned to that task. Future `update_task_prompt` tool calls on those threads hard-fail with a clear message. Implementation point: hook into `store.UpdateTaskStatus`'s `OnDone`/post-transition callback.
5. **Cascade on archive or soft-delete.** When a task is archived (`POST /api/tasks/{id}/archive`) or tombstoned (`DELETE /api/tasks/{id}`), auto-archive its task-mode threads. Unarchiving the task (`POST /api/tasks/{id}/unarchive` or `/restore`) unarchives only threads that were archived as part of this cascade (track with a small flag in `SessionInfo`, e.g. `AutoArchivedByTaskLifecycle=true`).
6. **Error message.** The `update_task_prompt` tool, on a cascade-archived thread, returns an error the agent can show to the user: "task has moved past backlog; start a new task to refine a new prompt."

## Tests

- `internal/handler/planning_test.go::TestIsTaskLocked_TrueDuringTurn` — set up an in-flight turn, assert true; end turn, assert false.
- `internal/runner/auto_promote_test.go::TestAutoPromoter_SkipsLockedTask` — locked task stays in backlog even when capacity is free.
- `internal/handler/tasks_test.go::TestPatchTask_RejectedWhenLocked` — 409 with the thread id in the response.
- `internal/handler/planning_threads_test.go::TestCascade_ArchiveOnTaskLeavesBacklog` — task moves to in_progress, its task-mode thread flips to archived with the cascade flag set.
- `TestCascade_UnarchivesOnTaskUnarchive` — unarchiving the task unarchives only cascade-archived threads; user-archived threads stay.
- `TestUpdateTaskPromptTool_FailsOnCascadeArchivedThread` — after cascade, tool returns the documented error.

## Boundaries

- Do NOT lock a task while a thread merely exists idle. The lock is tied to in-flight turns only.
- Do NOT cascade threads when a task archives/deletes cascade-archived threads that the user manually unarchived since; track intent carefully.
- Do NOT change the spec-mode undo cascade that cancels dispatched tasks.
