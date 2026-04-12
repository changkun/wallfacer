---
title: Undo & Snapshot System
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox.md
affects:
  - internal/handler/planning.go
  - internal/planner/
  - internal/apicontract/routes.go
  - ui/js/
effort: medium
created: 2026-03-30
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Undo & Snapshot System

## Current Gap

When the planning chat agent writes spec files (via `h.planner.Exec` in
`internal/handler/planning.go`), those changes are left as uncommitted working tree
modifications. No commits happen, no snapshots are taken, and no undo stack is
maintained. After the exec goroutine saves the session ID and appends the assistant
message to the conversation log, the workspace filesystem is silently mutated with no
recovery path.

## Current State

The following infrastructure already exists:

- **Git utilities**: `internal/gitutil/` has `StashIfDirty()`, `StashPop()`, and
  `cmdexec.Git()` covers all git operations needed for committing and resetting
- **Planning exec flow**: `internal/handler/planning.go` runs `planner.Exec()` in a
  background goroutine and appends the result to the conversation log — the natural
  injection point for a post-exec commit step
- **Result extraction**: `conversation.go`'s `ExtractResultText()` extracts the agent's
  response summary from NDJSON — suitable as the commit message body
- **Spec write detection**: `git status --porcelain specs/` detects whether the agent
  made any file writes, so no-op rounds produce no commit
- **Dispatch integration**: `internal/handler/specs_dispatch.go` has `UndispatchSpecs`
  — needed for dispatch-aware undo

## Decision: Git Commit-Based Snapshots

Each planning round that writes spec files is immediately followed by a
`git add specs/ && git commit` in each workspace. The commit message prefix
`plan: round N — <summary>` identifies planning commits unambiguously in the git log.
Undo walks the log backwards, finds the last planning commit, and runs
`git reset --hard HEAD~1`.

**Why git commits:**
- No separate snapshot storage — the working branch is already a git repo
- `git reset --hard` atomically handles file creation, modification, and deletion
- `git log` provides a human-readable audit trail of all planning changes
- Planning commits integrate with the existing push/merge pipeline without
  special-casing
- Rounds with no file writes produce no commit; the undo stack only covers rounds that
  actually changed something

**Round numbering.** Count existing planning commits in the log
(`git log --format=%s --grep="^plan: round"`) and increment by one. This is cheap,
robust across session restarts, and monotonically increasing even after undo operations.

**Undo mechanics:**
1. Find the last planning commit: `git log --format="%H %s" --grep="^plan: round" -1`
2. Stash any user edits made after that commit: `gitutil.StashIfDirty()`
3. Reset: `git reset --hard HEAD~1`
4. Pop stash if one was created: `gitutil.StashPop()`
5. If the reverted commit dispatched a spec (added a `dispatched_task_id` line to
   frontmatter), call `UndispatchSpecs` for the affected task IDs

**Concurrent edits.** If the user hand-edited a spec between the planning commit and the
undo request, `StashIfDirty()` preserves those edits. The stash pop restores them on top
of the reverted tree. If pop produces a conflict, the endpoint returns an error with the
conflicting paths; the working tree is left with the reverted spec and the stash intact
for manual resolution.

**Dispatch-aware undo.** After `git reset --hard HEAD~1`, inspect the reverted diff
(`git diff HEAD HEAD@{1} -- specs/`) for `dispatched_task_id:` lines that were added.
For each task UUID found, call `UndispatchSpecs` — this cancels the kanban task and
clears the frontmatter link atomically from the user's perspective.

**No practical stack limit.** Planning commits accumulate in the git log; all of them
are undoable. The server walks the log on each undo request — no in-memory stack to
overflow.

## Remaining Work

1. **Post-exec commit** — In `internal/handler/planning.go`, after `planner.Exec()`
   returns without error, check `git status --porcelain specs/` in each workspace. If
   dirty, derive round N (count planning commits + 1), extract the summary from
   `conversation.ExtractResultText()`, and run:
   ```
   git add specs/
   git commit -m "plan: round N — <summary>"
   ```
   using `cmdexec.Git()`. Skip entirely if no writes occurred.

2. **Undo endpoint** — Add `POST /api/planning/undo` to `internal/handler/planning.go`.
   The handler:
   - Finds the latest planning commit via `git log`; returns 409 if none exists
   - Calls `gitutil.StashIfDirty()` on the workspace
   - Runs `git reset --hard HEAD~1`
   - Calls `gitutil.StashPop()` if a stash was created; on pop conflict, returns 409
     with conflicting paths and leaves stash intact
   - Inspects the reverted diff for dispatched task IDs; calls `UndispatchSpecs` for
     each
   - Returns `{round: N, summary: "...", files_reverted: [...]}`
   - Register route in `internal/apicontract/routes.go`

3. **UI undo button** — In the planning chat UI (`ui/js/`), add a single "Undo last
   round" button in the chat header (not per-message). The button is disabled when no
   planning commits exist. On click, call `POST /api/planning/undo` and refresh both
   the spec tree and the chat message state.

## Task Breakdown

| Child spec | Depends on | Effort | Status |
|------------|-----------|--------|--------|
| [Post-exec planning commit](undo-snapshots/post-exec-commit.md) | — | small | complete |
| [Undo API endpoint](undo-snapshots/undo-api.md) | post-exec-commit | small | validated |
| [UI undo button](undo-snapshots/undo-ui.md) | undo-api | small | validated |

```mermaid
graph LR
  A[Post-exec commit] --> B[Undo API endpoint]
  B --> C[UI undo button]
```

**Deferred:** Redo (forward stack), multi-workspace undo, and drift assessment are out
of scope. The three tasks above are sufficient for a complete undo workflow.

## Affects

- `internal/handler/planning.go` — post-exec `git add specs/ && git commit`; new
  `POST /api/planning/undo` handler
- `internal/apicontract/routes.go` — register `/api/planning/undo`
- `internal/planner/` — optional git helper for round number derivation
- `ui/js/` — undo button in the planning chat header
