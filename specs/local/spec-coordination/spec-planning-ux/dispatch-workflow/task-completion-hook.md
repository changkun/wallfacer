---
title: Task Completion Hook for Spec Status
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/dispatch-workflow/spec-frontmatter-writer.md
  - specs/local/spec-coordination/spec-planning-ux/dispatch-workflow/task-spec-source-field.md
affects:
  - internal/store/tasks_update.go
  - internal/spec/lifecycle.go
effort: small
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Task Completion Hook for Spec Status

## Goal

When a task transitions to `done`, check if it was dispatched from a spec (via `SpecSourcePath`). If so, update the spec file's `status` to `complete` and `updated` timestamp. This is layer 1 of the task completion feedback system — deterministic, instant metadata update with no agent involvement.

Note: The parent spec's three-layer design calls for an intermediate `done` status (layer 1 sets `done`, layer 2 confirms `complete`). Since drift assessment (layer 2) is deferred, this initial implementation sets `complete` directly. When layer 2 is implemented, this hook will be updated to set a `done` intermediate status, and a new `done` → `complete` transition will be added to the spec lifecycle state machine.

## What to do

1. In `internal/store/tasks_update.go`, in the `UpdateTaskStatus` function (around line 37-39 where `TaskStatusDone` is handled), add a hook after the status update:
   - Check if `t.SpecSourcePath` is non-empty
   - If so, call `spec.UpdateFrontmatter(specFilePath, map[string]any{"status": "complete", "updated": today})` where `specFilePath` is resolved to the absolute path in the workspace
   - Log the spec status update for observability
   - Errors from `UpdateFrontmatter` should be logged but NOT block the task status transition (the task completing is more important than the spec metadata update)

2. The store needs access to workspace paths to resolve `SpecSourcePath` (which is relative, e.g., `specs/local/foo.md`) to an absolute filesystem path. Add a `specRootDirs []string` field to the Store (or accept it as a parameter) so the hook can find the spec file. The spec file should be searched in all workspace directories (same logic as `GetSpecTree`).

3. Ensure the hook fires for all completion paths:
   - Normal completion: `committing` → `done` (runner execute flow)
   - Manual "Mark as Done": `waiting` → `committing` → `done` (handler flow)

## Tests

- `TestCompletionHook_UpdatesSpecStatus` — create a task with `SpecSourcePath`, transition to `done`, verify spec file's `status` is `complete` and `updated` is today
- `TestCompletionHook_NoSpecPath` — transition a task without `SpecSourcePath` to `done`, verify no spec file changes
- `TestCompletionHook_SpecFileNotFound` — task has `SpecSourcePath` but file doesn't exist, verify task still transitions to `done` (error logged, not fatal)
- `TestCompletionHook_AlreadyComplete` — spec is already `complete`, verify no error (idempotent)

## Boundaries

- Do NOT implement drift assessment (layer 2) — that's a future extension
- Do NOT add a `done` intermediate status to the spec lifecycle — defer until layer 2
- Do NOT modify the task state machine
- Do NOT trigger any agent or sandbox execution
- Do NOT block task completion on spec update failures

## Implementation notes

- **Callback pattern instead of store field**: The spec suggested adding `specRootDirs []string` to the Store. Instead, the implementation uses a `Store.OnDone func(Task)` callback set by the server layer. This keeps the store decoupled from workspace and spec packages — the store doesn't import `internal/spec` at all. The callback receives a deep-cloned task and runs in a goroutine outside the store lock.
- **Exported `CurrentWorkspaces`**: Added `Handler.CurrentWorkspaces()` as an exported wrapper around the existing `currentWorkspaces()` so the server layer can pass it to the hook closure. The closure calls it on each invocation to get the current workspace list (workspaces can change at runtime).
