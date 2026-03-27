# Task 5: Replace r.store with taskStore() in Execution Path

**Status:** Todo
**Depends on:** Task 3, Task 4
**Phase:** 2 (Runner Multi-Store Awareness)
**Effort:** Large

## Goal

Ensure `Run()` and all functions it calls use the task's own store (via
`taskStore(taskID)`) rather than `r.store`, which may change during
execution due to workspace switches.

## What to do

1. Audit all `r.store` references in the runner execution path. Key files:
   - `internal/runner/execute.go` — `Run()`, `executeTask()`, `handleExecution()`
   - `internal/runner/commit.go` — `commitAndPush()`
   - `internal/runner/ideation.go` — `runIdeationTask()`
   - `internal/runner/refinement.go` — `runRefinement()`
   - `internal/runner/oversight.go` — oversight generation
   - `internal/runner/usage.go` — usage recording
   - `internal/runner/title.go` — title generation

2. Replace each `r.store` reference in the execution path with
   `r.taskStore(taskID)`. Since `taskID` is available throughout the
   execution chain, this should be straightforward.

   **Strategy**: Use `taskStore(taskID)` at each call site rather than
   capturing once at entry. This way, if the mapping changes (unlikely but
   safe), the accessor always returns the correct store.

3. Leave `r.store` references that are NOT in the execution path unchanged:
   - `applyWorkspaceSnapshot()` — updates `r.store` for the viewed group
   - `startBoardSubscriptionLoop()` — uses viewed store for board cache
   - `currentStore()` — returns viewed store for API callers
   - Worktree GC — operates on viewed store

4. Do NOT change function signatures. The `taskStore()` accessor is a
   method on Runner, so no parameter threading needed.

## Tests

- `TestRunUsesTaskStore` — start a task in group A, switch to group B
  mid-execution (mock), verify the task's store operations go to A's store.
- `TestRunFallbackToCurrentStore` — task with no `taskWSKey` mapping uses
  `currentStore()`.

## Risk

This is the most invasive change in the runner. Every `r.store` reference
in the execution path must be found and replaced. Missing one causes tasks
to write to the wrong store after a workspace switch.

## Boundaries

- Do NOT change Handler or Manager.
- Do NOT change non-execution-path code (board subscription, worktree GC).
