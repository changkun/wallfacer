---
title: Spec Lifecycle Transitions
status: complete
depends_on:
  - specs/local/spec-document-model/spec-model-types.md
affects:
  - internal/spec/
effort: small
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Spec Lifecycle Transitions

## Goal

Implement status transition validation for specs, enforcing the lifecycle state machine defined in the spec document model. This mirrors the pattern used by `store.ValidateTransition()` for tasks.

## What to do

1. In `internal/spec/lifecycle.go`, define the allowed transitions map:

   ```go
   var allowedTransitions = map[SpecStatus][]SpecStatus{
       StatusVague:     {StatusDrafted},
       StatusDrafted:   {StatusValidated, StatusStale},
       StatusValidated: {StatusComplete, StatusStale},
       StatusComplete:  {StatusStale},
       StatusStale:     {StatusDrafted, StatusValidated},
   }
   ```

2. Implement `ValidateTransition(from, to SpecStatus) error` — returns nil if the transition is allowed, a descriptive error otherwise. Use a sentinel error `ErrInvalidTransition` for wrapping.

3. Implement `ValidStatuses() []SpecStatus` — returns all valid status values (useful for validation).

4. Implement `ValidTracks() []SpecTrack` and `ValidEfforts() []SpecEffort` — returns all valid enum values.

## Tests

- `TestValidateTransition_AllValid`: Table-driven test covering every allowed transition.
- `TestValidateTransition_AllInvalid`: Table-driven test covering disallowed transitions (e.g., `vague` -> `complete`, `complete` -> `drafted`).
- `TestValidateTransition_SameStatus`: Same-to-same transitions should be rejected.
- `TestValidateTransition_ErrorWrapping`: Verify returned error wraps `ErrInvalidTransition`.
- `TestValidStatuses`: Verify all 5 statuses returned.
- `TestValidTracks`: Verify all 4 tracks returned.
- `TestValidEfforts`: Verify all 4 effort levels returned.

## Boundaries

- Do NOT implement automatic status changes or triggers.
- Do NOT modify the Spec struct — use the types from spec-model-types.
- Do NOT implement progress-based status updates (that's in progress-tracking).
