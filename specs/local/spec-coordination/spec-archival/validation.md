---
title: "Archival: Validation skips for archived specs"
status: validated
depends_on:
  - specs/local/spec-coordination/spec-archival/core-model.md
affects:
  - internal/spec/validate.go
  - internal/spec/validate_test.go
  - internal/spec/validate_tree_test.go
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Archival: Validation skips for archived specs

## Goal

Update `internal/spec/validate.go` so that archived specs are exempt from the
warning-level per-spec checks that become noise after archival, and so that
cross-spec checks treat archived specs as "below glass": invisible to status
consistency, stale propagation, and dependency-archived soft notes.

## What to do

### Per-spec validation (`validate.go`)

1. **`checkBodyNotEmpty`** — extend the early-return guard from `StatusVague` to also
   skip `StatusArchived`:
   ```go
   if s.Status == StatusVague || s.Status == StatusArchived || s.Status == "" {
       return nil
   }
   ```

2. **`checkAffectsExist`** — add an archived guard at the top of the function:
   ```go
   if s.Status == StatusArchived {
       return nil
   }
   ```

3. **`checkValidEnums`** — no change needed; `StatusArchived` is now in `ValidStatuses()`
   (added by `core-model.md`), so the enum check will automatically accept it.

### Cross-spec validation (`validate.go`)

4. **`checkStatusConsistency`** — skip archived non-leaves entirely (they mask their
   subtree by design). Add a guard inside the existing loop after the `StatusComplete`
   check:
   ```go
   if node.Value.Status == StatusArchived {
       continue
   }
   ```
   Also update `hasIncompleteLeaf` to treat archived leaves as "not incomplete"
   (return `false` when `node.Value.Status == StatusArchived`).

5. **`checkStalePropagation`** — two guards needed:
   - Skip archived stale specs as sources (they should not propagate):
     ```go
     if node.Value.Status != StatusStale {  // existing
         continue
     }
     // archived stale specs are silent
     if node.Value.Status == StatusArchived {
         continue
     }
     ```
     Wait — if `Status == StatusArchived` it won't pass the `Status != StatusStale` check anyway.
     Instead, the second guard is on the *dependency* side: skip archived specs as sinks
     (a live validated spec depending on an archived spec should not trigger stale-propagation):
     ```go
     // Inside the dependents loop:
     if depNode.Value.Status == StatusArchived {
         continue
     }
     ```

6. **New soft note — "dependency-is-archived"** — add a new cross-spec check
   function `checkArchivedDependencies(tree *Tree) []Result`. For each live spec
   (non-archived) whose `DependsOn` contains an archived spec path, emit one
   `SeverityWarning` result with rule `"dependency-is-archived"` and message:
   `"depends on archived spec %q — consider removing the edge or documenting why it still matters"`.
   Call this function from `ValidateTree` after `checkStalePropagation`.

## Tests

Add to `validate_test.go`:
- `TestValidateSpec_ArchivedBodyEmpty` — archived spec with empty body: no `body-not-empty` warning
- `TestValidateSpec_ArchivedAffectsMissing` — archived spec with non-existent `affects` path: no `affects-exist` warning
- `TestValidateSpec_ArchivedStatusValid` — archived spec passes `valid-status` check (no error)

Add to `validate_tree_test.go`:
- `TestValidateTree_ArchivedNonLeafIncompleteChildren` — archived non-leaf with incomplete children: no `status-consistency` warning
- `TestValidateTree_ArchivedDependencyNoStalePropagation` — validated spec depending on an archived (stale) spec: no `stale-propagation` warning
- `TestValidateTree_LiveSpecDependsOnArchived` — live validated spec depending on archived spec: one `dependency-is-archived` warning (severity warning, not error)
- `TestValidateTree_ArchivedSpecDependsOnMissing` — archived spec with missing `depends_on` target: `depends-on-exist` error still fires (structural rules apply)

## Boundaries

- Do NOT change error-level per-spec rules (required fields, date format, self-dependency,
  dispatch consistency, DAG acyclicity, unique dispatches) — these apply to all specs including archived
- Do NOT change `checkDependsOnExist` — missing `depends_on` targets are always errors
- Do NOT touch `internal/spec/impact.go` or `progress.go` — those are in `impact-progress.md`
- Do NOT add handler or UI changes
