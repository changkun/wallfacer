---
title: Undo & Snapshot System
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox.md
affects:
  - internal/handler/
  - ui/js/
effort: medium
created: 2026-03-30
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Undo & Snapshot System

## Current Gap

Currently, when the planning chat agent writes spec files (via `h.planner.Exec` in
`internal/handler/planning.go`), those changes are left as uncommitted working tree
modifications. No commits happen, no snapshots are taken, and no undo stack is
maintained. After the exec goroutine saves the session ID and appends the assistant
message to the conversation log, the workspace filesystem is silently mutated with no
recovery path. This spec addresses that gap.

## Design Problem

How should per-round snapshots be captured and restored to support undo in the planning
session? The parent spec decides that each chat round (one user message that triggers
file writes) creates an implicit snapshot, and undo reverts all writes from that round
as one unit. The design must define the snapshot mechanism, storage format, undo stack
management, and the UI interaction for triggering undo.

Key constraints:
- Snapshots must be lightweight — planning sessions may produce 50+ rounds
- Only files the agent actually modified are snapshotted (not the full workspace)
- Snapshots must be per-round, not per-file — a single undo reverts all writes from one agent response
- The undo stack persists with the planning session (survives close/reopen)
- Multiple undos walk back through the stack in reverse order
- Undo must handle file creation (undo = delete the file) and file deletion (undo = restore the file)

## Context

The planning sandbox operates directly on the workspace filesystem. The agent reads specs,
writes modified specs, creates new spec files, and potentially deletes specs during
breakdown operations. All writes are confined to `specs/`.

Git is available in the workspace. The spec files are version-controlled. Git commits
serve as both the snapshot mechanism and the natural undo unit.

The existing task system doesn't have undo — tasks are fire-and-forget. The planning
session is the first interactive editing workflow where undo matters.

## Decision

**Option B is chosen: git commit-based snapshots.**

Each agent response that writes files is followed immediately by a `git add specs/ &&
git commit` on the current working branch. The commit message encodes the round number
and a short summary (e.g. `plan: round 12 — refine auth spec`). This commit is both
the snapshot and the undo unit — no separate snapshot storage is needed.

Undo is implemented as `git revert HEAD` (or `git reset --soft HEAD~1` if the branch is
local-only and no push has occurred). The undo target is the last planning commit, which
the server identifies by inspecting the reflog or commit message prefix (`plan: round`).

Rationale for choosing Option B:
- The working branch is already a git repo; no external storage is required.
- `git reset` cleanly handles file creation, modification, and deletion in one operation.
- The full history of planning changes is inspectable via `git log` and `git diff`.
- Commit messages give a human-readable audit trail of what each round changed.
- Round commits integrate naturally into the existing commit pipeline used by the task
  runner, so the planning branch can be pushed or merged without special-casing.
- No manifest or per-file tracking is needed: git tracks the complete working tree delta.

## Options

The following options were considered. Option B was selected (see Decision above).

**Option A — Git stash-based snapshots.** Before each agent round, `git stash push -- specs/`
captures the current state. Undo pops the stash. The stash stack maps to the undo stack.

- Pro: Uses existing git infrastructure. Stashes are lightweight, composable, and persist
  across sessions. `git stash pop` is atomic. The user can also access stashes via git CLI
  for advanced recovery.
- Con: Git stash operates on the entire working tree diff, not just the files the agent
  will modify. If the user has unstaged changes outside `specs/`, stash captures those
  too (or needs `--keep-index` gymnastics). Stash doesn't track file creations cleanly
  (new untracked files need `--include-untracked`).

**Option B — Git commit-based snapshots. (CHOSEN)** After each agent round that writes
files, create a commit on the working branch with a `plan: round N` prefix. Undo resets
to the previous commit. Each commit is the snapshot for that round.

- Pro: Full git history of planning changes. Each snapshot is a proper commit with a
  message. Reset is clean. Works correctly with file creation and deletion. Easy to
  inspect (`git log --oneline`). No side branch required.
- Con: Creates many small commits. Must identify and filter planning commits from
  non-planning commits when walking the undo stack.

**Option C — File-copy snapshots.** Before each agent round, copy the affected files to a
snapshot directory (`~/.wallfacer/snapshots/<fingerprint>/<round>/`). Each snapshot
directory contains the pre-modification versions of files the agent will modify. Undo
copies files back from the snapshot directory.

- Pro: Simple, no git dependency for the snapshot mechanism. Fine-grained (only affected
  files, not the whole `specs/` tree). Storage location is outside the repo (no git noise).
- Con: Must track file creation/deletion explicitly (snapshot needs a manifest of
  operations). More disk I/O. Must handle the case where a file was created by the agent
  (undo = delete) vs. modified (undo = restore previous content).

## Open Questions

1. How does undo interact with concurrent edits? If the user edits a spec file between
   round N and undo of round N, the working tree has diverged from the commit. Should
   undo warn the user if the working tree is dirty relative to the planning commit being
   reverted?
2. Should undo be available only in the UI (undo button per agent response), or also as
   a chat command ("undo the last change")?
3. How does undo interact with dispatch? If the agent dispatched a spec (creating a
   kanban task + updating `dispatched_task_id`) and the user undoes that round, the spec
   file reverts but the kanban task persists. Should the undo operation also cancel the
   dispatched task, or is that a separate manual step?
4. What happens when the undo stack limit is reached (e.g., 50 rounds)? Oldest planning
   commits are retained in the git log but flagged as no longer undoable? Or is there no
   practical limit given that git history is append-only?

## Affects

- `internal/handler/planning.go` — after exec completes successfully, run `git add specs/ && git commit` before appending the assistant message to the conversation log
- `internal/planner/` — may need a helper to run git commands in the workspace and to walk the commit log to find the undo target
- `ui/js/` — undo button per agent response in the chat stream, undo stack status indicator
- Spec file system — restore operations are delegated to `git reset` rather than custom file I/O
