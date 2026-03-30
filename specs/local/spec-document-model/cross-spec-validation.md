---
title: Cross-Spec Validation
status: complete
track: local
depends_on:
  - specs/local/spec-document-model/spec-tree-builder.md
  - specs/local/spec-document-model/per-spec-validation.md
affects:
  - internal/spec/
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Cross-Spec Validation

## Goal

Implement tree-wide validation rules that require the full spec tree context: DAG cycle detection, orphan detection, status consistency, stale propagation, track consistency, and unique dispatch IDs.

## What to do

1. In `internal/spec/validate.go`, add:

   - `ValidateTree(tree *SpecTree, repoRoot string) []ValidationResult` — runs all cross-spec rules on the full tree. Also runs `ValidateSpec` on each individual spec.

2. Implement these cross-spec rules:

   | Rule | Severity | Check |
   |------|----------|-------|
   | `dag-acyclic` | error | The `depends_on` graph has no cycles. On violation, report the full cycle path (e.g., "A -> B -> C -> A"). Use topological sort or DFS with coloring. |
   | `no-orphan-directories` | warning | A `<name>/` subdirectory should have a corresponding `<name>.md` parent spec |
   | `no-orphan-specs` | warning | A `<name>.md` with a `<name>/` subdirectory should have at least one child spec in it |
   | `status-consistency` | warning | A `complete` non-leaf spec should not have incomplete leaves in its subtree |
   | `stale-propagation` | warning | If a spec is `stale`, dependents that are still `validated` should be flagged |
   | `track-consistency` | warning | All specs in `specs/<track>/` should have `track: <track>` in frontmatter |
   | `unique-dispatches` | error | No two specs share the same `dispatched_task_id` (ignoring nulls) |

3. For DAG cycle detection, implement a DFS-based approach:
   - Build adjacency list from all `depends_on` edges across the tree.
   - Run DFS with three states (unvisited, in-progress, done).
   - On back-edge detection, reconstruct and report the cycle path.

4. For stale propagation, build the reverse dependency index (dependents of each spec) and walk forward from stale specs.

## Tests

- `TestValidateTree_Valid`: A well-formed tree with no issues returns no errors.
- `TestValidateTree_DirectCycle`: A -> B -> A cycle detected with full path in message.
- `TestValidateTree_TransitiveCycle`: A -> B -> C -> A cycle detected.
- `TestValidateTree_NoCycle`: Diamond dependency (A -> B, A -> C, B -> D, C -> D) is not a cycle.
- `TestValidateTree_OrphanDirectory`: Subdirectory without parent `.md` triggers warning.
- `TestValidateTree_OrphanSpec`: `.md` with empty subdirectory — no warning (it's just a leaf).
- `TestValidateTree_StatusConsistency`: Complete parent with incomplete leaf triggers warning.
- `TestValidateTree_StalePropagate`: Stale spec with `validated` dependent triggers warning.
- `TestValidateTree_TrackConsistency`: Spec in `local/` with `track: cloud` triggers warning.
- `TestValidateTree_UniqueDispatches`: Two specs with same dispatch ID triggers error.
- `TestValidateTree_NullDispatchesOK`: Multiple specs with null dispatch IDs — no error.
- `TestValidateTree_IncludesPerSpecErrors`: Individual spec errors also appear in tree validation results.
- `TestValidateTree_EmptyTree`: Empty tree returns no errors.

Use `t.TempDir()` to build complete test directory structures with multiple spec files.

## Boundaries

- Do NOT implement the validation CLI command or HTTP handler (that's in spec-planning-ux).
- Do NOT implement progress tracking or impact analysis — those are separate tasks.
- Do NOT modify the SpecTree or SpecNode types.

## Implementation notes

- **`track-consistency` delegates to per-spec rule**: The spec listed `track-consistency` as a cross-spec rule, but this is already fully covered by the per-spec `track-matches-path` rule (which compares `track` frontmatter to the spec's filesystem path). The cross-spec `checkTrackConsistency` is a no-op that documents this delegation.
- **`no-orphan-specs` rule merged with `no-orphan-directories`**: The spec defined both rules, but "orphan spec" (`.md` with empty subdirectory) is just a normal leaf spec — not an error or warning. The implementation only checks `no-orphan-directories` (subdirectory without parent `.md`), which is the actionable case.
