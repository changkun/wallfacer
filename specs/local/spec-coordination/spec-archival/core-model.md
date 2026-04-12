---
title: "Archival: StatusArchived constant and lifecycle transitions"
status: validated
depends_on: []
affects:
  - internal/spec/model.go
  - internal/spec/lifecycle.go
  - internal/spec/lifecycle_test.go
  - internal/spec/model_test.go
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Archival: StatusArchived constant and lifecycle transitions

## Goal

Add `StatusArchived` to the spec model and wire the four new lifecycle transitions
(`drafted/complete/stale → archived`, `archived → drafted`) into the state machine.
This is the foundational change that all other archival tasks depend on.

## What to do

1. **`internal/spec/model.go`** — Add the constant after `StatusStale` (line 20):
   ```go
   StatusArchived Status = "archived"
   ```

2. **`internal/spec/lifecycle.go`** — Extend the transition map with four new edges:
   ```go
   StatusDrafted:  {StatusValidated, StatusStale, StatusArchived},
   StatusComplete: {StatusStale, StatusArchived},
   StatusStale:    {StatusDrafted, StatusValidated, StatusArchived},
   StatusArchived: {StatusDrafted},
   ```
   Update `ValidStatuses()` to return 6 statuses (append `StatusArchived` to the slice).

3. **`internal/spec/lifecycle_test.go`** — Update existing tests:
   - `TestStatusMachine_AllValid`: add the 4 new valid pairs
     (`drafted→archived`, `complete→archived`, `stale→archived`, `archived→drafted`)
   - `TestStatusMachine_AllInvalid`: add invalid pairs that must be rejected
     (`vague→archived`, `validated→archived`, `archived→complete`,
     `archived→validated`, `archived→stale`)
   - `TestValidStatuses`: assert `len == 6`; assert `StatusArchived` is in the set

4. **`internal/spec/model_test.go`** (if it tests `ValidStatuses` or the enum list):
   update any hardcoded counts or string lists.

## Tests

- `TestStatusMachine_AllValid` — all 4 new transitions return `nil` error
- `TestStatusMachine_AllInvalid` — invalid `archived` transitions return `statemachine.ErrInvalidTransition`
- `TestValidStatuses_Count` — `len(ValidStatuses()) == 6`
- `TestValidStatuses_ContainsArchived` — `StatusArchived` present in `ValidStatuses()`

## Boundaries

- Do NOT change any validation logic (that is task `validation.md`)
- Do NOT change impact, progress, or handler code in this task
- Do NOT add UI changes in this task
- The `StatusMachine.Validate(from, to)` call site stays unchanged;
  only the transition map and `ValidStatuses()` slice change
