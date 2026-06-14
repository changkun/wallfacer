---
title: Planning Chat Threads
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-chat-agent.md
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox.md
  - specs/local/spec-coordination/spec-planning-ux/undo-snapshots.md
affects:
  - internal/planner/threads.go
  - internal/planner/conversation.go
  - internal/planner/planner.go
  - internal/handler/planning_threads.go
  - internal/handler/planning.go
  - internal/handler/planning_undo.go
  - internal/handler/planning_git.go
  - internal/apicontract/routes.go
  - frontend/src/stores/planning.ts
  - frontend/src/components/plan/PlanningChatPanel.vue
  - docs/internals/api-and-transport.md
effort: large
created: 2026-04-12
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Planning Chat Threads

## Problem

The specs UI chat currently has one conversation thread per workspace group. Users who plan multiple initiatives in parallel (for example, "auth refactor" and "CI cleanup") have to either interleave unrelated messages in a single thread (polluting Claude Code's session context) or clear the thread and lose history every time they switch topics. There's no way to park a line of thinking and come back to it later.

At the same time, unlike board tasks (where per-task isolation is essential), planning conversations share the same workspace and the same `specs/` write surface. There's no isolation benefit to running each thread in its own execution context; the cost (session management overhead) is pure overhead.

**Goal:** let users maintain any number of named conversation threads per workspace group, each with its own Claude Code session and history, while all threads share the single planner execution context.

## Constraints

- **Shared execution context.** One planner per workspace group. All threads route through it. (At spec-authoring time this was a shared sandbox container; the planner now runs as a host process in the first configured workspace, see Outcome. The "one planner, many threads" invariant is unchanged.)
- **One turn at a time.** The planner runs one agent invocation at a time. Messages sent to other threads while one is in-flight are queued (frontend) and drained in FIFO order when the planner goes idle.
- **Per-thread session.** Each thread has its own Claude Code `session_id`. `--resume` uses the thread's session, so conversation context stays independent.
- **Per-thread history.** Each thread has its own `messages.jsonl` (no cross-contamination in the persisted log).
- **Undo isolation.** `git reset --hard HEAD~1` doesn't work when multiple threads interleave commits to `specs/`. Undo must work regardless of commit ordering.
- **Backward compatible.** Existing single-thread installations must migrate transparently on first run.

## Design

### Thread identity

- Thread ID: ULID (lexicographically sortable, URL-safe).
- Display name is separate from ID so rename is a metadata-only operation.
- Threads are scoped to the workspace-group fingerprint. Switching groups shows a different thread set.

### On-disk layout

```
~/.wallfacer/planning/<workspace-fingerprint>/
  threads.json              # manifest: ordered [{id, name, created, updated, archived}]
  active.json               # {active_thread_id: "..."} (UI preference)
  threads/
    <thread-id>/
      messages.jsonl
      session.json
```

`threads.json` and `active.json` are written via `atomicfile.WriteJSON` (same helper `conversation.go` already uses). `messages.jsonl` and `session.json` keep their existing shapes; each thread is its own existing-shaped conversation.

**Archived threads** stay in `threads.json` with `archived: true`. They are hidden from the tab bar but surfaced in a submenu under the `+` button for restore. Files on disk are never deleted by archive.

### Migration (crash-safe)

On first load of an old-layout directory:

1. Generate a new ULID `<id>`.
2. Copy `messages.jsonl` and `session.json` into `threads/<id>/`.
3. Atomically write `threads.json` with `[{id, name: "Chat 1", archived: false, ...}]` and `active.json` with `{active_thread_id: <id>}`.
4. Delete the originals.

`threads.json` existing is the commit point. If the process crashes between steps 2 and 3, step 1's copies are overwritten on retry. If it crashes between 3 and 4, step 4 is re-run on retry (idempotent). No data loss.

### Backend: ThreadManager

`internal/planner/threads.go` provides `ThreadManager`, which owns the per-thread `ConversationStore` instances:

- `List(includeArchived bool) []ThreadMeta`
- `Create(name string) (ThreadMeta, error)`
- `Rename(id, name string) error`
- `Archive(id string) error`
- `Unarchive(id string) error`
- `Meta(id string) (ThreadMeta, error)`
- `Store(id string) (*ConversationStore, error)` (resolves a thread to its conversation store)
- `ActiveID() string` / `SetActiveID(id string) error`
- `Touch(id string)` (updates last-activity for unread tracking)

Each per-thread `ConversationStore` exposes the same surface it always has: `AppendMessage`, `LoadMessages`, `LoadSession`, `SaveSession`.

### Backend: Planner

`internal/planner/planner.go` tracks the in-flight thread ID alongside `busy` and the live log:

- `Planner.Exec` plumbs the thread context so the planner resumes the right session and the handler persists messages to the right thread.
- The live log is exposed only when the in-flight thread matches the requested thread, else the handler returns 204 (same as "no exec in flight").
- Single-exec-at-a-time invariant is unchanged. `busy=true` returns 409, same as today (`SetBusy`, `IsBusy`, `BusyThreadID`).

### Backend: HTTP routes

Routes in `internal/apicontract/routes.go`, handlers in `internal/handler/planning_threads.go`:

| Method | Path | Body | Purpose |
|---|---|---|---|
| GET | `/api/planning/threads` | (none) | List threads (`?includeArchived=true` includes archived). Returns `{threads, active_id}`. |
| POST | `/api/planning/threads` | `{name?}` | Create thread. Returns thread summary with ID. |
| PATCH | `/api/planning/threads/{id}` | `{name}` or `{state}` | `{name}` renames. `{state: archived\|visible\|active}` archives, restores, or activates. |

The mutation verbs (archive / unarchive / activate) were consolidated into a single `PATCH .../{id}` with a `state` field rather than separate `/archive`, `/unarchive`, `/activate` sub-routes.

Existing message/stream/interrupt/clear/undo routes accept a `thread` query parameter. Default is the active thread for back-compat with scripts and existing tests (`threadIDFromRequest`).

`clear` is thread-scoped; it clears only that thread's `messages.jsonl` and `session.json`, not the whole group.

### Undo via `git revert`

The original undo did `git reset --hard HEAD~1` and required the commit to be at HEAD. That breaks with multiple threads; thread A can only undo when its commit is the most recent.

Switch to a forward `git revert` commit:

- Planning commits gain a `Plan-Thread: <thread-id>` trailer alongside `Plan-Round: N` (`internal/handler/planning_git.go`, `buildPlanCommitMessage`).
- `/api/planning/undo?thread=<id>` finds the most recent commit where `Plan-Thread` matches the caller, then creates a forward revert commit (`internal/handler/planning_undo.go`).
- The revert commit itself carries `Plan-Thread: <id>` and `Plan-Round: N+1` (a no-op on the file side but keeps round numbering monotonic and attributable).
- Dispatched-task cancellation logic reads the diff of the reverted commit and cancels tasks dispatched in that round.
- On revert conflict (rare: two threads editing the same spec file), `git revert --abort` and return 409 with conflicting paths.
- Dirty-edit stash behavior is preserved across the revert.

Trade-off: history keeps both the original commit and the revert, rather than making the original disappear. This is the correct trade for multi-thread; undo must always work regardless of commit ordering.

### Frontend: tab bar

Tab bar lives in `frontend/src/components/plan/PlanningChatPanel.vue` (Vue), backed by `frontend/src/stores/planning.ts`:

- One tab per non-archived thread in `threadOrder`.
- `+` button creates a new thread. Overflow submenu (`archiveMenuOpen`) under `+` lists archived threads with restore buttons.
- `×` on a tab archives (with confirm). Rejected for the in-flight thread (toast explains why).
- Pencil icon / double-click starts inline rename (`renameDraft`, `.pcp-tab-rename` input). Commit on Enter, cancel on Escape or blur.
- Unread dot on inactive tabs (`thread.unread`) set when a background thread receives a message; cleared on tab activation.

### Frontend: per-thread state + global drain

Per-thread state lives in the store as `threads[threadId]: PlanningThread` (queue, scroll, unread, streaming flags) rather than module-scoped globals.

- Switching tabs aborts nothing: the in-flight exec keeps running on its thread (`streamingThreadId`) and persists output there. Switching renders the target thread's history; if the active thread is the in-flight one, the SSE stream attaches, otherwise history-only.
- **Global drain dispatcher.** When the in-flight exec finishes, the store walks all threads and fires the oldest queued message next, regardless of which thread the user is currently viewing. Without this, queued messages in background tabs would never fire.

### Slash commands

`GET /api/planning/commands` is unchanged. Slash-command autocomplete operates on the active tab's input.

## Acceptance Criteria

- [x] Old single-thread installations auto-migrate on first run. Existing conversation appears as "Chat 1" with its session preserved.
- [x] Migration is crash-safe: if the process dies mid-migration, restarting completes it without data loss.
- [x] `GET /api/planning/threads` lists threads in order. `POST` creates one. `PATCH {name}` renames. `PATCH {state}` toggles archive/visible/active.
- [x] Two threads in the same workspace group have independent Claude Code session IDs; sending in thread B does not affect thread A's session context.
- [x] Sending in thread B while thread A is in-flight queues the message locally; the queued message fires automatically once A completes, even if the user never switches tabs.
- [x] Attempting to send while an exec is in flight returns 409 (existing behavior, now scoped).
- [x] `/api/planning/messages/stream?thread=<id>` returns 204 unless `<id>` is the in-flight thread.
- [x] Interrupt and clear accept `thread` and are scoped to that thread.
- [x] Archiving the in-flight thread is rejected with a clear message.
- [x] Planning commits carry `Plan-Thread: <id>` trailer.
- [x] Undo creates a forward revert commit targeting the caller thread's most recent round, regardless of whether a different thread committed after.
- [x] Undo conflict (same spec edited by two threads) aborts cleanly and returns 409 with conflicting paths.
- [x] Tab bar renders above the chat messages. `+` creates, `×` archives, pencil/double-click renames inline (commit on Enter, cancel on Escape/blur). Archived threads appear in `+` submenu for restore.
- [x] Inactive tabs show an unread dot when a background thread receives a message; clears on tab activation.
- [x] Switching tabs preserves each thread's scroll position and queued messages.

## Test Plan

**Go unit tests** (`internal/planner/threads_test.go`, `internal/planner/conversation_test.go`, `internal/handler/planning_test.go`, `internal/handler/planning_git_test.go`, `internal/handler/planning_undo_test.go`):

- Round-trip `Create` then `List` then `Rename` then `Archive` then `Unarchive` (`TestThreadManager_CreateRenameArchive`).
- Migration from single-thread layout to threads layout, including the two crash-mid-migration variants (`TestThreadManager_Migration_FromLegacyLayout`, `TestThreadManager_Migration_CrashAfterCopy`, `TestThreadManager_Migration_CrashAfterManifest`).
- Session isolation: two threads, two different `SaveSession` calls, `LoadSession` returns the right one for each (`TestThreadManager_SessionIsolation`).
- Active-thread fallback when the active thread is archived (`TestThreadManager_ActiveFallsBackWhenArchived`).
- Handler: send while busy returns 409; stream for non-in-flight thread returns 204.
- Git: `Plan-Thread` trailer written and parsed correctly. Revert path creates a forward commit with the right trailers. Revert-with-conflict returns 409 with no half-applied state.

**Frontend tests:**

- Tab bar renders N tabs from a thread list.
- Unread badge appears when a message arrives for an inactive thread, clears on tab activation.
- Inline rename: Enter commits, Escape cancels, blur commits.
- Switching tabs restores per-thread scroll position and queue.

**Manual E2E:**

1. Start fresh with an old-layout install. Open specs UI. Verify old messages appear under "Chat 1."
2. Create "Auth refactor," rename to "Authentication rewrite," send a message, watch the stream.
3. While streaming, switch to "Chat 1" and verify the stream continues in the background and messages appear there on switch-back.
4. Send a message in "Chat 1" while "Authentication rewrite" is streaming and verify it's queued, then fires on completion without user switching tabs.
5. Undo on "Authentication rewrite" creates a revert commit even after Chat 1 has committed newer rounds. Verify `git log` shows both the original and the revert, both carrying `Plan-Thread:` trailers.
6. Archive a thread; confirm the tab disappears, files remain, and the thread shows up in the `+` submenu's Archived section. Restore and send a message to confirm session context is intact.
7. Edge: workspace-group switch loads the new group's threads.
8. Edge: server restart mid-stream; on reload, `busy=false`, all threads show persisted history, no stream attaches.

## Non-Goals

- **Cross-thread messaging.** Threads do not reference each other. If a user wants context from another thread, they copy-paste.
- **Per-thread execution isolation.** Threads share the same planner and the same workspace. This is intentional; the whole point of threads is to reuse the planner.
- **Per-thread dispatched-task tracking.** Threads own commits via `Plan-Thread` trailer, but dispatched board tasks remain workspace-group scoped. A thread's undo cancels tasks it dispatched, nothing more.
- **Soft-delete / tombstone retention.** This spec uses archive-only (no hard delete). Files on disk are retained indefinitely; a future spec can add a purge action if threads accumulate.

## Outcome

Shipped. Verified against the code on 2026-06-14.

**Backend**

- `internal/planner/threads.go`: `ThreadManager` with `List`, `Create`, `Rename`, `Archive`, `Unarchive`, `Meta`, `Store`, `ActiveID`/`SetActiveID`, `Touch`. ULID thread IDs, `threads.json` manifest, `active.json` UI preference, crash-safe `loadOrMigrate` / `migrateOrInit` / `removeLegacyFiles`. Tests in `internal/planner/threads_test.go` cover create/rename/archive, session isolation, active-fallback, and all three migration crash points.
- `internal/planner/planner.go`: in-flight thread tracking (`busyThreadID`, `SetBusy`, `IsBusy`, `BusyThreadID`); single-exec-at-a-time invariant preserved.
- `internal/handler/planning_threads.go`: `ListPlanningThreads`, `CreatePlanningThread`, `PatchPlanningThread`; `threadIDFromRequest`, `lookupThreadStore`, `toThreadSummary`.
- `internal/handler/planning.go`: send/history/stream/clear/interrupt accept `?thread=`, default to the active thread, 202 on accept, 409 when busy, 204 on a stream for a non-in-flight thread.
- `internal/handler/planning_git.go` + `internal/handler/planning_undo.go`: `Plan-Thread: <id>` trailer on planning commits, forward `git revert` undo scoped per thread, conflict abort with 409.
- `internal/apicontract/routes.go`: `GET`/`POST /api/planning/threads`, `PATCH /api/planning/threads/{id}`.

**Frontend**

- `frontend/src/stores/planning.ts`: `PlanningThread` record, `threads`/`threadOrder`/`archivedThreads`/`activeThreadId`, `loadThreads`, `activeThread`, per-thread queue/scroll/unread/streaming state, global drain on completion.
- `frontend/src/components/plan/PlanningChatPanel.vue`: tab bar, `+` create, `×` archive (confirm), inline rename (`.pcp-tab-rename`, Enter/Escape/blur), archived submenu (`archiveMenuOpen`), unread dots, SSE attach scoped to `streamingThreadId`.

**Divergences from the spec as written**

- **No container.** The "shared planner container per workspace group" framing in Constraints and Non-Goals is historical. Planning now runs as a host process in the first configured workspace (`internal/planner/spec.go` `buildSpec`, no volumes, no per-thread container). The "one planner, many threads" invariant the spec depended on holds regardless.
- **Route consolidation.** Archive/unarchive/activate landed as a single `PATCH /api/planning/threads/{id}` with a `state` field, not the three separate POST sub-routes in the original route table.
- **API method names.** `ThreadManager` shipped with `List`/`Create`/`Rename`/`Archive`/`Unarchive`/`Meta`/`Store`/`ActiveID`/`SetActiveID`/`Touch`, not the spec's draft `ListThreads`/`CreateThread`/`ThreadSummary` names. Per-thread session/message access stayed on `ConversationStore` (`LoadSession`/`SaveSession`/`AppendMessage`/`LoadMessages`) rather than a new `Thread` type.
- **Task-mode extension (beyond original scope).** Threads gained a mode pin (spec-mode vs task-mode) and lifecycle cascade: `ThreadManager.CascadeArchiveForTask` / `CascadeUnarchiveForTask`, with the handler auto-archiving task-mode threads when their task closes and restoring them on reopen. Sends are rejected with 409 when a `focused_spec`/`focused_task` mismatches the thread's pinned mode.
- **UI moved to Vue.** The original `affects` named `ui/js/planning-chat.js`, `ui/partials/spec-mode.html`, `ui/css/spec-mode.css`. The vanilla `ui/` tree is deleted; the live implementation is `frontend/src/stores/planning.ts` + `frontend/src/components/plan/PlanningChatPanel.vue`.
