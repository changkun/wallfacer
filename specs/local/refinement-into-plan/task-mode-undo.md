---
title: Task-Mode Undo via Event Rewind
status: complete
depends_on:
  - specs/local/refinement-into-plan/update-task-prompt-tool.md
  - specs/local/refinement-into-plan/prompt-round-events.md
affects:
  - internal/handler/planning_undo.go
  - internal/handler/planning.go
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: 51c1b36a-55b1-477f-b588-b6e8672c355f
---



# Task-Mode Undo via Event Rewind

## Goal

Make `POST /api/planning/undo?thread=<id>` branch on thread mode: spec-mode keeps its existing git-revert path, task-mode uses event rewind.

## What to do

1. **Branch on mode.** At the top of `UndoPlanningRound` in `internal/handler/planning_undo.go`, resolve the thread's mode from `SessionInfo`. File-mode: fall through to the existing git-revert path. Task-mode: dispatch to a new `undoTaskModeRound` helper.
2. **Event rewind.** `undoTaskModeRound` finds the most recent `prompt_round` event for the thread on the pinned task that has not yet been superseded by a `prompt_round_revert`. It restores `prev_prompt` onto `task.Prompt` atomically and appends a `prompt_round_revert` event referencing the reverted round number. No git operations.
3. **Empty undo.** If no unreverted `prompt_round` events exist for this thread, return 409 with a shape matching the git-revert "nothing to undo" response.
4. **Cascade with dispatched tasks.** Unlike spec-mode undo (which cancels dispatched tasks whose link was added in the reverted commit), task-mode undo only rewinds the pinned task's prompt. No board task is cancelled.
5. **Response shape.** Keep the response JSON identical to the git-revert path so the UI does not need to branch: `{reverted_round, thread_id, mode: "task"}`.

## Tests

- `internal/handler/planning_undo_test.go::TestUndo_TaskMode_RewindsLastRound` — two rounds, one undo, assert `task.Prompt` matches round-1 output and a `prompt_round_revert` event is appended.
- `TestUndo_TaskMode_RepeatedUndo` — two rounds, two undos, assert prompt is back to the original and two `prompt_round_revert` events exist.
- `TestUndo_TaskMode_NothingToUndo` — 409 when the thread has no prompt rounds yet.
- `TestUndo_FileMode_Unchanged` — spec-mode thread still uses the git-revert path and returns the same shape as before.
- `TestUndo_TaskMode_DoesNotTouchGit` — assert no git commands are invoked for task-mode undo (use a fake git runner or count invocations).

## Boundaries

- Do NOT modify the spec-mode git-revert machinery in `planning_undo.go`.
- Do NOT handle "task was deleted after the round was written" here; that edge is covered by the cascade task.
- Do NOT expose a new undo endpoint; same URL, just a branch inside.
