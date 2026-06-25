---
title: Generic background task launcher in runner
status: archived
depends_on:
  - specs/local/backend-redundancy-cleanup.md
affects:
  - internal/runner/runner.go
effort: small
created: 2026-06-01
updated: 2026-06-15
author: changkun
dispatched_task_id: null
---


# Generic background task launcher in runner

`internal/runner/runner.go` has four near-identical wrappers:

- `RunBackground` (`runner.go:361`)
- `SyncWorktreesBackground` (`runner.go:396`)
- `GenerateOversightBackground` (`runner.go:407`)
- `GenerateTitleBackground` (`runner.go:415`)

Each wraps one line:

```go
r.backgroundWg.Go("<label>:"+taskID.String()[:8], func() {
    r.X(...)
})
```

`RunBackground` is the outlier — it also captures the workspace key,
increments/decrements the workspace task count, and shutdown-guards on
`shutdownCtx.Err()`. Keep `RunBackground` as-is; collapse the other
three.

## Scope

Add a small private helper:

```go
func (r *Runner) taskBackground(label string, taskID uuid.UUID, fn func()) {
    r.backgroundWg.Go(label+":"+taskID.String()[:8], fn)
}
```

Then:

- `SyncWorktreesBackground` becomes one line.
- `GenerateOversightBackground` becomes one line.
- `GenerateTitleBackground` becomes one line.

The label-prefix convention (`"oversight:" + shortID`) is now
single-sourced.

## Tests

Existing runner tests cover the dispatch paths. Spot-check that the
generated background-goroutine names in
`r.backgroundWg.PendingGoroutines()` are unchanged in shape so
debug/runtime endpoints keep their existing output.

## Out of scope

- `RunBackground` — has additional workspace-counting logic that's
  worth keeping inline.
