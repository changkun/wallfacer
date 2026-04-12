---
title: "Archival: Dispatch handler guards"
status: validated
depends_on:
  - specs/local/spec-coordination/spec-archival/core-model.md
affects:
  - internal/handler/specs_dispatch.go
  - internal/handler/specs_dispatch_test.go
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Archival: Dispatch handler guards

## Goal

Make `internal/handler/specs_dispatch.go` explicitly aware of `StatusArchived`:
(1) reject dispatch attempts on archived specs with a clear error message, and
(2) treat archived `depends_on` targets as already satisfied so they contribute
no `DependsOn` edge to the resulting kanban task.

## What to do

### Reject dispatch on archived specs (explicit error message)

In the per-spec validation loop (around lines 85-88 of `specs_dispatch.go`),
the existing check reads:
```go
if s.Status != spec.StatusValidated {
    errs = append(errs, ...)
}
```

Update the error message to distinguish the archived case:
```go
if s.Status != spec.StatusValidated {
    msg := fmt.Sprintf("spec status is %q, must be %q", s.Status, spec.StatusValidated)
    if s.Status == spec.StatusArchived {
        msg = fmt.Sprintf("spec status is %q — unarchive the spec first before dispatching", s.Status)
    }
    errs = append(errs, dispatchError{SpecPath: relPath, Error: msg})
    continue
}
```

### Treat archived dependencies as satisfied

In the dependency resolution loop (around lines 116-147), when resolving
`depends_on` targets that are not in the current dispatch batch:

```go
// After checking if dep is in batch...
// Look up the dep's spec to check its status
depSpec, err := parseDepSpec(dep)
if err != nil { /* skip */ }
if depSpec.Status == spec.StatusArchived {
    // Archived deps are considered already satisfied — no DependsOn edge
    continue
}
if depSpec.DispatchedTaskID != nil {
    taskDeps = append(taskDeps, *depSpec.DispatchedTaskID)
}
```

The `parseDepSpec` lookup mirrors the existing logic that reads `DispatchedTaskID`
from the dep's frontmatter. The check is: if `Status == StatusArchived`, skip the
dep entirely (contribute no blocker task ID, regardless of `dispatched_task_id`).

### No change to undispatch

Undispatch (`dispatched_task_id` clearing) on an archived spec is caught by the
existing per-spec validation in `validate.go` (dispatch consistency rule still applies
to archived specs since it is an error-level rule). No handler change needed.

## Tests

Add to `specs_dispatch_test.go`:
- `TestDispatch_ArchivedSpecRejectedWithMessage` — dispatch request for an archived
  spec: response contains an error with the word "unarchive" in the message
- `TestDispatch_ArchivedDependencyTreatedAsSatisfied` — dispatch a validated spec
  that `depends_on` an archived spec: the task is created without a `DependsOn`
  blocker for the archived dep; HTTP 200 response
- `TestDispatch_ArchivedDependencyInBatch` — batch dispatch where one spec is archived:
  the archived spec itself gets the archived rejection error; the other (valid) specs
  in the batch succeed

## Boundaries

- Do NOT change the dispatch endpoint URL or request/response schema
- Do NOT change the rollback logic
- Do NOT change per-spec validation for any status other than archived
- Do NOT touch `internal/spec/` package code in this task
