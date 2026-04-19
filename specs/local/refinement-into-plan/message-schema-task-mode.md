---
title: Planning Message Schema and Task-Mode Threads
status: complete
depends_on: []
affects:
  - internal/handler/planning.go
  - internal/handler/planning_threads.go
  - internal/planner/conversation.go
effort: medium
created: 2026-04-19
updated: 2026-04-20
author: changkun
dispatched_task_id: fd1181a8-a288-46ce-a233-e076ab046b23
---


# Planning Message Schema and Task-Mode Threads

## Goal

Add `focused_task` to the Plan messaging schema and thread-create API so a planning thread can be pinned to a task UUID for its lifetime. This is the foundation every other child spec builds on.

## What to do

1. **Conversation model.** In `internal/planner/conversation.go`, add a `FocusedTask string` field to `Message` (peer to `FocusedSpec`) and to `SessionInfo`. Exactly one of the two is non-empty per message; this is the thread's "mode". Update NDJSON serialization/deserialization so old `messages.jsonl` files (no `FocusedTask` field) read as file-mode.
2. **Message validation.** In `internal/handler/planning.go::SendPlanningMessage`, reject requests where both `focused_spec` and `focused_task` are set with 422. Resolve `focused_task` against the store; return 404 if missing or tombstoned.
3. **Thread mode pinning.** First non-empty `focused_spec` or `focused_task` on a thread fixes its mode in `SessionInfo`. Subsequent messages in the opposite mode are rejected with 409 (caller should create a new thread).
4. **Thread create.** `POST /api/planning/threads` body gains optional `focused_task` (UUID). When set, the thread is created in task mode with that task as the pin; mutually exclusive with any future `focused_spec` on the thread. `internal/handler/planning_threads.go` stores the mode in the session file on creation.
5. **Status reflection.** Thread list response includes a `mode: "spec" | "task"` field and, for task-mode threads, the pinned `task_id`.

## Tests

- `internal/planner/conversation_test.go::TestMessageFocusedTask_SerDe` — round-trip a task-mode message through NDJSON; back-compat reading a file without `FocusedTask`.
- `internal/handler/planning_test.go::TestSendPlanningMessage_BothFocusedFields` — 422 when both set.
- `TestSendPlanningMessage_UnknownFocusedTask` — 404 for bogus UUID.
- `TestSendPlanningMessage_ModeMismatch` — 409 when a file-mode thread receives a `focused_task` message (and vice versa).
- `internal/handler/planning_threads_test.go::TestCreateThread_TaskMode` — thread is stored with task mode and pinned task_id; listed threads expose the mode field.

## Boundaries

- Do NOT implement the `update_task_prompt` tool yet (separate task).
- Do NOT add the new event types yet (separate task).
- Do NOT change undo behavior yet (separate task).
- Do NOT wire the explorer section or card button (separate tasks).
- Do NOT delete any refine code yet (retirement is the final task).
