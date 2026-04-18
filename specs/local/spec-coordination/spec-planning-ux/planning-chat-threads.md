---
title: Planning Chat Threads
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-chat-agent.md
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox.md
  - specs/local/spec-coordination/spec-planning-ux/undo-snapshots.md
affects:
  - internal/planner/
  - internal/handler/
  - internal/apicontract/
  - ui/js/planning-chat.js
  - ui/partials/spec-mode.html
  - ui/css/spec-mode.css
  - docs/internals/api-and-transport.md
effort: large
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Planning Chat Threads

## Problem

The specs UI chat currently has one conversation thread per workspace group. Users who plan multiple initiatives in parallel (for example, "auth refactor" and "CI cleanup") have to either interleave unrelated messages in a single thread — polluting Claude Code's session context — or clear the thread and lose history every time they switch topics. There's no way to park a line of thinking and come back to it later.

At the same time, unlike board tasks (where per-task isolation is essential), planning conversations share the same read-only workspace and the same `specs/` write overlay. There's no isolation benefit to running each thread in its own container; the cost (container startup, RAM, session management) is pure overhead.

**Goal:** let users maintain any number of named conversation threads per workspace group, each with its own Claude Code session and history, while all threads share the single planner sandbox container.

## Constraints

- **Shared container.** One planner sandbox container per workspace group. All threads route through it.
- **One turn at a time.** The container runs one agent invocation at a time. Messages sent to other threads while one is in-flight are queued (frontend) and drained in FIFO order when the container goes idle.
- **Per-thread session.** Each thread has its own Claude Code `session_id`. `--resume` uses the thread's session, so conversation context stays independent.
- **Per-thread history.** Each thread has its own `messages.jsonl` — no cross-contamination in the persisted log.
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
  active.json               # {active_thread_id: "..."} — UI preference
  threads/
    <thread-id>/
      messages.jsonl
      session.json
```

`threads.json` and `active.json` are written via `atomicfile.WriteJSON` (same helper `conversation.go` already uses). `messages.jsonl` and `session.json` keep their existing shapes — each thread is its own existing-shaped conversation.

**Archived threads** stay in `threads.json` with `archived: true`. They are hidden from the tab bar but surfaced in a submenu under the `+` button for restore. Files on disk are never deleted by archive.

### Migration (crash-safe)

On first load of an old-layout directory:

1. Generate a new ULID `<id>`.
2. Copy `messages.jsonl` and `session.json` into `threads/<id>/`.
3. Atomically write `threads.json` with `[{id, name: "Chat 1", archived: false, ...}]` and `active.json` with `{active_thread_id: <id>}`.
4. Delete the originals.

`threads.json` existing is the commit point. If the process crashes between steps 2 and 3, step 1's copies are overwritten on retry. If it crashes between 3 and 4, step 4 is re-run on retry (idempotent). No data loss.

### Backend: ThreadManager

`internal/planner/conversation.go` gains `ThreadManager` (either replacing or owning `ConversationStore`):

- `ListThreads(includeArchived bool) []ThreadSummary`
- `CreateThread(name string) (*Thread, error)`
- `RenameThread(id, name string) error`
- `ArchiveThread(id string) error`
- `UnarchiveThread(id string) error`
- `Thread(id string) (*Thread, error)`
- `ActiveThreadID() string` / `SetActiveThreadID(id string) error`

`Thread` exposes the same surface the current `ConversationStore` does: `AppendMessage`, `LoadMessages`, `GetSession`, `SetSession`.

### Backend: Planner

`internal/planner/planner.go` gains a `threadID` field on its in-flight exec state alongside `busy` and `liveLog`. Changes:

- `Planner.Exec(ctx, threadID, args...)` — the thread ID plumbs through so the planner resumes the right session and persists messages to the right thread.
- `Planner.LogReader(threadID) io.Reader` — returns the live log only if the in-flight thread matches, else `nil`. Handler returns 204 for non-matching thread (same as current "no exec in flight").
- Single-exec-at-a-time invariant is unchanged. `busy=true` → 409, same as today.

### Backend: HTTP routes

New routes (added to `internal/apicontract/routes.go` and `internal/handler/planning.go`):

| Method | Path | Body | Purpose |
|---|---|---|---|
| GET | `/api/planning/threads` | — | List non-archived threads (optionally `?includeArchived=true`). |
| POST | `/api/planning/threads` | `{name?}` | Create thread. Returns thread summary with ID. |
| PATCH | `/api/planning/threads/{id}` | `{name}` | Rename thread. |
| POST | `/api/planning/threads/{id}/archive` | — | Archive. 409 if thread is in-flight. |
| POST | `/api/planning/threads/{id}/unarchive` | — | Restore from archive. |
| POST | `/api/planning/threads/{id}/activate` | — | Set as active (UI preference). |

Existing message/stream/interrupt/clear/undo routes gain a `thread` query parameter. Default is the active thread for back-compat with scripts and existing tests.

`clear` is thread-scoped — it clears only that thread's `messages.jsonl` and `session.json`, not the whole group.

### Undo via `git revert`

The current undo does `git reset --hard HEAD~1` and requires the commit to be at HEAD. That breaks with multiple threads — thread A can only undo when its commit is the most recent, regardless of whether anyone else's commit is in between.

Switch to `git revert --no-edit <sha>`:

- Planning commits gain a `Plan-Thread: <thread-id>` trailer alongside `Plan-Round: N`.
- `/api/planning/undo?thread=<id>` finds the most recent commit where `Plan-Thread` matches the caller, then creates a forward revert commit.
- The revert commit itself carries `Plan-Thread: <id>` and `Plan-Round: N+1` (a no-op on the file side but keeps round numbering monotonic).
- Dispatched-task cancellation logic stays the same — it reads the diff of the reverted commit and cancels tasks dispatched in that round.
- On revert conflict (rare: two threads editing the same spec file), `git revert --abort` and return 409 with conflicting paths.
- Dirty-edit stash behavior (`planning_undo.go:89-107`) is preserved across the revert.

Trade-off: history keeps both the original commit and the revert, rather than making the original disappear. This is the correct trade for multi-thread — undo must always work regardless of commit ordering.

### Frontend: tab bar

Model closely on `renderHeaderWorkspaceGroupTabs` in `ui/js/workspace.js:429-509`:

- One tab per non-archived thread in `threads.json` order.
- `+` button creates a new thread. Overflow submenu under `+` lists archived threads with restore buttons.
- `×` on tab archives (with confirm). Rejected for the in-flight thread — toast message explains why.
- Pencil icon / double-click starts inline rename, reusing the `startInlineTabRename` pattern (`ui/js/workspace.js:1022-1064`). Commit on Enter, cancel on Escape or blur.
- Unread dot on inactive tabs when their last message timestamp is newer than the tab's `lastViewedAt` (stored in-memory per session).

### Frontend: per-thread state + global drain

Promote today's module-scoped chat state (`_streaming`, `_activeStream`, `_queue`, `_userScrolledUp`, `rawBuffer`, scroll position) into a per-thread record: `threads[threadId] = {queue, scrollPos, ...}`.

- Switching tabs abort-nothing: the in-flight exec keeps running on its thread and persists output there. Switching just calls `_loadHistory(threadID)` to render that thread's messages. If the active thread *is* the in-flight one, attach the SSE stream; otherwise render history only.
- **Global drain dispatcher.** In `_stopStreaming`, after clearing the busy flag, walk all threads in FIFO-of-enqueue order and fire the oldest queued message next — regardless of which thread the user is currently looking at. Without this, queued messages in background tabs would never fire until the user manually switched to them.

### Slash commands

`GET /api/planning/commands` is unchanged. Slash-command autocomplete operates on the active tab's input (no change to `.mention-dropdown` logic).

## Acceptance Criteria

- [ ] Old single-thread installations auto-migrate on first run. Existing conversation appears as "Chat 1" with its session preserved.
- [ ] Migration is crash-safe: if the process dies mid-migration, restarting completes it without data loss.
- [ ] `GET /api/planning/threads` lists threads in order. `POST` creates one. `PATCH` renames. `archive` / `unarchive` toggle visibility.
- [ ] Two threads in the same workspace group have independent Claude Code session IDs; sending in thread B does not affect thread A's session context.
- [ ] Sending in thread B while thread A is in-flight returns 202 with the message queued locally; the queued message fires automatically once A completes, even if the user never switches tabs.
- [ ] Attempting to send to thread A while thread A is already in-flight returns 409 (existing behavior, now scoped).
- [ ] `/api/planning/messages/stream?thread=<id>` returns 204 unless `<id>` is the in-flight thread.
- [ ] Interrupt and clear accept `thread` and are scoped to that thread.
- [ ] Archiving the in-flight thread returns 409 with a clear message.
- [ ] Planning commits carry `Plan-Thread: <id>` trailer.
- [ ] Undo creates a forward revert commit targeting the caller thread's most recent round, regardless of whether a different thread committed after.
- [ ] Undo conflict (same spec edited by two threads) aborts cleanly and returns 409 with conflicting paths.
- [ ] Tab bar renders above the chat messages. `+` creates, `×` archives, pencil/double-click renames inline (commit on Enter, cancel on Escape/blur). Archived threads appear in `+` submenu for restore.
- [ ] Inactive tabs show an unread dot when their last message timestamp is newer than their `lastViewedAt`.
- [ ] Switching tabs preserves each thread's scroll position and queued messages.

## Test Plan

**Go unit tests** (`internal/planner/conversation_test.go`, `internal/handler/planning_test.go`, `internal/handler/planning_git_test.go`):

- Round-trip `CreateThread` → `ListThreads` → `RenameThread` → `ArchiveThread` → `UnarchiveThread`.
- Migration from single-thread layout to threads layout, including the two crash-mid-migration variants (copy done but manifest missing; manifest done but originals still present).
- Session isolation: two threads, two different `SetSession` calls, `GetSession` returns the right one for each.
- Handler: send to A while B idle; send to A while A in-flight → 409; archive in-flight → 409; stream for non-in-flight thread → 204.
- Git: `Plan-Thread` trailer written and parsed correctly. Revert path creates a forward commit with the right trailers. Revert-with-conflict returns 409 with no half-applied state (working tree clean, no in-progress merge).

**Frontend tests** (`ui/js/planning-chat.test.js` or similar):

- Tab bar renders N tabs from a mocked thread list.
- Unread badge appears when a mocked message arrives for an inactive thread, clears on tab activation.
- Inline rename: Enter commits, Escape cancels, blur commits.
- Switching tabs restores per-thread scroll position and queue.

**Manual E2E:**

1. Start fresh with an old-layout install. Open specs UI. Verify old messages appear under "Chat 1."
2. Create "Auth refactor," rename to "Authentication rewrite," send a message, watch the stream.
3. While streaming, switch to "Chat 1" — verify stream continues in the background and messages appear there on switch-back.
4. Send a message in "Chat 1" while "Authentication rewrite" is streaming — verify it's queued, then fires on completion without user switching tabs.
5. Undo on "Authentication rewrite" creates a revert commit even after Chat 1 has committed newer rounds. Verify `git log` shows both the original and the revert, both carrying `Plan-Thread:` trailers.
6. Archive a thread; confirm the tab disappears, files remain, and the thread shows up in the `+` submenu's Archived section. Restore and send a message to confirm session context is intact.
7. Edge: workspace-group switch mid-stream cleanly cancels the in-flight thread and loads the new group's threads.
8. Edge: server restart mid-stream — on reload, `busy=false`, all threads show persisted history, no stream attaches.

## Non-Goals

- **Cross-thread messaging.** Threads do not reference each other. If a user wants context from another thread, they copy-paste.
- **Per-thread sandbox isolation.** Threads share the same container and the same read-only workspace mount. This is intentional — the whole point of threads is to reuse the container.
- **Per-thread dispatched-task tracking.** Threads own commits via `Plan-Thread` trailer, but dispatched board tasks remain workspace-group scoped (same as today). A thread's undo cancels tasks it dispatched, nothing more.
- **Soft-delete / tombstone retention.** This spec uses archive-only (no hard delete) to match user preference. Files on disk are retained indefinitely; a future spec can add a purge action if threads accumulate.
