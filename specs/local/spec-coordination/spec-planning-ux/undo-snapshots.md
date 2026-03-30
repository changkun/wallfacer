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
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Undo & Snapshot System

## Design Problem

How should per-round snapshots be captured and restored to support undo in the planning session? The parent spec decides that each chat round (one user message that triggers file writes) creates an implicit snapshot, and undo reverts all writes from that round as one unit. The design must define the snapshot mechanism, storage format, undo stack management, and the UI interaction for triggering undo.

Key constraints:
- Snapshots must be lightweight — planning sessions may produce 50+ rounds
- Only files the agent actually modified are snapshotted (not the full workspace)
- Snapshots must be per-round, not per-file — a single undo reverts all writes from one agent response
- The undo stack persists with the planning session (survives close/reopen)
- Multiple undos walk back through the stack in reverse order
- Undo must handle file creation (undo = delete the file) and file deletion (undo = restore the file)

## Context

The planning sandbox operates directly on the workspace filesystem. The agent reads specs, writes modified specs, creates new spec files, and potentially deletes specs during breakdown operations. All writes are confined to `specs/`.

Git is available in the workspace. The spec files are version-controlled. Git's stash mechanism or lightweight commits could serve as the snapshot backend.

The existing task system doesn't have undo — tasks are fire-and-forget. The planning session is the first interactive editing workflow where undo matters.

## Options

**Option A — Git stash-based snapshots.** Before each agent round, `git stash push -- specs/` captures the current state. Undo pops the stash. The stash stack maps to the undo stack.

- Pro: Uses existing git infrastructure. Stashes are lightweight, composable, and persist across sessions. `git stash pop` is atomic. The user can also access stashes via git CLI for advanced recovery.
- Con: Git stash operates on the entire working tree diff, not just the files the agent will modify. If the user has unstaged changes outside `specs/`, stash captures those too (or needs `--keep-index` gymnastics). Stash doesn't track file creations cleanly (new untracked files need `--include-untracked`).

**Option B — Git commit-based snapshots.** Before each agent round, create a lightweight commit on a snapshot branch (`planning-snapshots/<fingerprint>`). Undo resets to the previous commit. The snapshot branch is local-only (never pushed).

- Pro: Full git history of planning changes. Each snapshot is a proper commit with a message ("before round 12"). Reset is clean. Works correctly with file creation and deletion. Easy to inspect (`git log planning-snapshots/...`).
- Con: Creates many small commits on a side branch. Pollutes reflog. Must manage branch creation/cleanup. More complex than stash.

**Option C — File-copy snapshots.** Before each agent round, copy the affected files to a snapshot directory (`~/.wallfacer/snapshots/<fingerprint>/<round>/`). Each snapshot directory contains the pre-modification versions of files the agent will modify. Undo copies files back from the snapshot directory.

- Pro: Simple, no git dependency for the snapshot mechanism. Fine-grained (only affected files, not the whole `specs/` tree). Storage location is outside the repo (no git noise).
- Con: Must track file creation/deletion explicitly (snapshot needs a manifest of operations). More disk I/O. Must handle the case where a file was created by the agent (undo = delete) vs. modified (undo = restore previous content).

## Open Questions

1. How does undo interact with concurrent edits? If the user edits a spec file between round N and undo of round N, the restored version would overwrite the user's edit. Should undo warn if the file has been modified since the snapshot?
2. Should undo be available only in the UI (undo button per agent response), or also as a chat command ("undo the last change")?
3. How does undo interact with dispatch? If the agent dispatched a spec (creating a kanban task + updating `dispatched_task_id`) and the user undoes that round, should the kanban task also be cancelled?
4. What happens when the undo stack limit is reached (e.g., 50 rounds)? Oldest snapshots are silently dropped? Or the user is warned that early rounds are no longer undoable?
5. Should there be a redo mechanism (undo the undo)? This doubles the complexity but is expected in editor-like experiences.

## Affects

- `internal/handler/` or `internal/planner/` — snapshot capture before agent writes, restore on undo request
- `ui/js/` — undo button per agent response in the chat stream, undo stack status indicator
- `~/.wallfacer/snapshots/` or git stash/branch — snapshot storage
- Spec file system — restore operations must handle create/modify/delete correctly
