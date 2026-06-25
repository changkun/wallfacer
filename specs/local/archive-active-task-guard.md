---
title: Archive Guard Only Blocks Active Dispatched Tasks
status: archived
depends_on: []
affects:
  - internal/handler/specs.go
  - internal/handler/specs_test.go
effort: small
created: 2026-06-15
updated: 2026-06-25
author: changkun
dispatched_task_id: null
---

# Archive Guard Only Blocks Active Dispatched Tasks

## Problem

Archiving a spec cascades to the whole subtree. The guard in
`ArchiveSpec` (`internal/handler/specs.go:232`) rejects the cascade with a
`409 Conflict` when the primary spec or any descendant still carries a non-nil
`dispatched_task_id`:

```go
if t.spec.DispatchedTaskID != nil {
    http.Error(w,
        fmt.Sprintf("%s: cancel the dispatched task before archiving", t.relPath),
        http.StatusConflict)
    return
}
```

The check looks only at whether a link exists, not at the linked task's state.
A spec whose children are all `done` (a fully shipped tree) still cannot be
archived, because each completed child keeps its `dispatched_task_id` pointing at
its finished task. The user hits "Conflict" with no recourse short of manually
cancelling already-completed tasks, which is nonsensical. This is the observed
case: a `complete` spec with 8/8 done children refuses to archive.

The block is only meaningful for tasks still in flight: archiving a spec whose
agent is actively running, queued, or awaiting feedback would orphan live work.
Once a task is terminal, the link is just provenance and should not block.

## Goal

Block archiving only when a target spec's dispatched task is in an **active**
(non-terminal) state. Allow archiving when the task is terminal, when the link is
stale (task no longer exists), or when there is no link at all.

## Design

In `ArchiveSpec`, replace the non-nil check with a task-state lookup. The
`Handler` already holds `h.store`; `specs_dispatch.go` uses
`h.store.GetTask(ctx, uuid)` with the same `dispatched_task_id` parsing pattern.

State classification, mirroring the terminal set used by dispatch-undo
(`specs_dispatch.go` treats `done`/`cancelled` as "already terminal"):

| Task status                                          | Blocks archive? |
| ---------------------------------------------------- | --------------- |
| `in_progress`, `committing`, `waiting`, `backlog`    | yes (active)    |
| `done`, `failed`, `cancelled`                        | no (terminal)   |
| task not found (stale `dispatched_task_id`)          | no              |
| `dispatched_task_id` is nil                          | no              |
| `dispatched_task_id` fails to parse as a UUID        | no (treat as stale) |

Rationale for the active set:
- `in_progress` / `committing`: an agent or the commit pipeline is running.
- `waiting`: the task is paused awaiting the user's feedback, i.e. live unfinished
  work, not abandoned.
- `backlog`: queued to run; archiving would orphan a task that will start.
- `failed`: no agent running and progress only resumes on explicit retry, so it is
  treated as terminal here (archiving a tree with a dead failed task is the user's
  call, not a footgun). This is the one judgment call; if it proves surprising,
  moving `failed` into the active set is a one-line change.

Add a small helper rather than inlining:

```go
// dispatchedTaskBlocksArchive reports whether a spec's dispatched task is in an
// active (non-terminal) state that should prevent archiving. A nil link, an
// unparseable id, or a missing/terminal task does not block.
func (h *Handler) dispatchedTaskBlocksArchive(ctx context.Context, id *string) bool {
    if id == nil {
        return false
    }
    taskID, err := uuid.Parse(*id)
    if err != nil {
        return false
    }
    task, err := h.store.GetTask(ctx, taskID)
    if err != nil {
        return false // stale linkage; task gone
    }
    switch task.Status {
    case store.TaskStatusDone, store.TaskStatusFailed, store.TaskStatusCancelled:
        return false
    default:
        return true
    }
}
```

Loop body becomes:

```go
if h.dispatchedTaskBlocksArchive(r.Context(), t.spec.DispatchedTaskID) {
    http.Error(w,
        fmt.Sprintf("%s: task is still active (%s); cancel or finish it before archiving",
            t.relPath, taskStatus),
        http.StatusConflict)
    return
}
```

Include the live status in the message so the user knows why (now legible end to
end, since the frontend `api()` helper surfaces plain-text bodies as of
`f0a76e5c`). The helper can return the status alongside the bool, or the loop can
re-fetch; prefer returning `(blocked bool, status store.TaskStatus)` to avoid a
second `GetTask`.

`UnarchiveSpec` is unaffected.

## Acceptance Criteria

1. Archiving a `complete` spec whose descendants are all `done` succeeds (no 409).
2. Archiving a spec whose dispatched task is `in_progress`, `committing`,
   `waiting`, or `backlog` returns `409 Conflict` with a body naming the live
   status.
3. Archiving a spec whose dispatched task is `done`, `failed`, or `cancelled`
   succeeds.
4. Archiving a spec whose `dispatched_task_id` points at a non-existent task
   succeeds (stale linkage does not block).
5. The blocked-archive 409 body is non-empty plain text and reaches the UI alert
   verbatim (already covered by `client.test.ts`; backend test asserts the body).
6. Regression tests in `specs_test.go` cover each row of the table above, using a
   store seeded with tasks in the relevant states.

## Out of Scope

- Clearing `dispatched_task_id` on archive. The link is retained as provenance and
  for lossless unarchive; only the guard's interpretation of it changes.
- Frontend changes. The plain-text error surfacing already shipped.
- Bulk "cancel all active tasks then archive" affordance.

## Outcome

Implemented as designed. `ArchiveSpec` now calls a new
`Handler.activeDispatchedTask` helper that resolves the `dispatched_task_id` via
`h.store.GetTask` and blocks only on non-terminal status; the 409 body names the
live status. The `failed` judgment call landed in the terminal/allow set as
specced. Tests in `specs_test.go` cover all table rows (active states block via a
subtest matrix, terminal states archive, stale link archives) and were verified to
fail without the guard change. The earlier frontend fix (`f0a76e5c`) already
surfaces the plain-text body so the message reaches the UI verbatim.
