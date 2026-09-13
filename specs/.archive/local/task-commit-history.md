---
title: Task commit history across feedback turns
status: complete
depends_on: []
affects:
  - internal/store/
  - internal/runner/
  - internal/handler/
  - internal/apicontract/
  - frontend/src/components/TaskDetail.vue
effort: medium
created: 2026-09-13
updated: 2026-09-13
author: changkun
dispatched_task_id: null
---

# Task commit history across feedback turns

## Overview

The Changes tab lists commits made during a task and its feedback iterations.
Each entry identifies the repository, commit, author, and capture turn. History
remains readable after worktree cleanup and agent history rewriting.

## Before implementation

`Task.CommitHashes` retains only each repository's final merged head.
`TaskDiff` serves an aggregate diff. The commit pipeline rebases and fast-forward
merges, then removes worktrees; snapshot repositories have no durable git log.

## Design

- Store captured commit metadata and patches in task blobs, separately from the
  board payload. Deduplicate by repository and hash; capture attempt and turn
  without replacing older records after retries, rebases, or amendments.
- Discover task commits relative to the worktree's merge base, or the snapshot
  root. Capture after implementation execution in subprocess and native Topos
  paths, and before final rebase and cleanup. Never create additional commits
  merely to populate the list.
- Expose authenticated GET `/api/tasks/{id}/commits`. Combine saved records with
  current read-only discovery while the worktree exists. Return repository,
  hash, subject, author/date, attempt/turn, and patch data.
- Render the list above the aggregate Changes diff. Expanding an entry shows
  its captured patch. Refresh using the task detail's existing live refresh.
- Persistence failure prevents destructive cleanup; a visible error preserves
  the worktree for retry. Tasks with no commits have an explicit empty state.

## Acceptance and verification

- A deterministic lifecycle test creates commits in two feedback iterations,
  completes the real commit pipeline, reopens storage after cleanup, and still
  reads both entries and patches.
- Test multiple repositories, unchanged worktrees, retry attribution, amended
  commits, and snapshot workspaces. Record capture on the native execution path.
- HTTP tests cover missing tasks and task-scoped results. UI tests cover the
  list, patch expansion, empty state, and refresh without navigation.
- The existing aggregate diff and final `CommitHashes` contract remain intact.

## Outcome

Implemented in `286a5e92`. Task blobs preserve metadata and patches independently
of board payloads. Capture covers subprocess and native execution, feedback
turns, and the final commit pipeline before worktree cleanup. The authenticated
commits endpoint serves persisted history and live discovery; Changes renders
expandable patches and refreshes with task detail data.

Lifecycle tests verify two feedback commits after cleanup and store reopening,
multiple repositories, amendments, retry attribution after history pruning,
plain folders, empty Git repositories, and native execution. A persistence
failure keeps the worktree and fails the task visibly. Patches have an explicit
2 MiB preview limit. HTTP tests cover isolation and missing tasks; component and
browser tests verify expansion, failure states, and live refresh.
