# Task 4: RunBackground Workspace Lifecycle Management

**Status:** Todo
**Depends on:** Task 1, Task 3
**Phase:** 2 (Runner Multi-Store Awareness)
**Effort:** Small

## Goal

Wire `RunBackground()` to capture the workspace key at dispatch time and
manage the Manager's task count, so the store stays alive for the duration
of the task.

## What to do

1. Modify `RunBackground()` in `internal/runner/runner.go`:

   ```go
   func (r *Runner) RunBackground(taskID uuid.UUID, prompt, sessionID string, resumed bool) {
       wsKey := r.currentWSKey()
       r.taskWSKey.Store(taskID, wsKey)
       r.workspaceManager.IncrementTaskCount(wsKey)

       label := "run:" + taskID.String()[:8]
       r.backgroundWg.Add(label)
       go func() {
           defer r.backgroundWg.Done(label)
           defer r.taskWSKey.Delete(taskID)
           defer r.workspaceManager.DecrementAndCleanup(wsKey)
           r.Run(taskID, prompt, sessionID, resumed)
       }()
   }
   ```

2. Ensure the defer order is correct: cleanup happens after `Run()`
   returns, `taskWSKey.Delete` before `DecrementAndCleanup` so no new
   lookups can find the stale mapping.

## Tests

- `TestRunBackgroundCapturesWSKey` — verify `taskWSKey` is populated
  before `Run()` starts.
- `TestRunBackgroundIncrementsTaskCount` — verify `IncrementTaskCount`
  is called with the correct key.
- `TestRunBackgroundCleansUpOnCompletion` — after `Run()` returns, verify
  `taskWSKey` entry is deleted and `DecrementAndCleanup` was called.

## Boundaries

- Do NOT change the `Run()` execution path yet (task 5).
- Do NOT change Handler.
