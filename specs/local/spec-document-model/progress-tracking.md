---
title: Progress Tracking
status: complete
depends_on:
  - specs/local/spec-document-model/spec-tree-builder.md
affects:
  - internal/spec/
effort: small
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Progress Tracking

## Goal

Implement recursive progress aggregation for non-leaf specs. A non-leaf spec's progress is the count of `complete` leaves divided by total leaves in its subtree. This is computed on the fly — no stored state.

## What to do

1. Create `internal/spec/progress.go` with:

   - `Progress` struct:
     ```go
     type Progress struct {
         Complete int // number of complete leaves in subtree
         Total    int // total number of leaves in subtree
     }
     ```

   - `(p Progress) Fraction() float64` — returns `Complete / Total` (0.0 if Total is 0).

   - `(p Progress) String() string` — returns `"4/6 leaves done"` format.

   - `NodeProgress(node *SpecNode) Progress` — recursively computes progress for a node:
     - If leaf: `Progress{Complete: 1, Total: 1}` if status is `complete`, else `Progress{Complete: 0, Total: 1}`.
     - If non-leaf: sum the progress of all children.

   - `TreeProgress(tree *SpecTree) map[string]Progress` — returns progress for every non-leaf node, keyed by relative path.

## Tests

- `TestNodeProgress_Leaf_Complete`: Complete leaf returns `{1, 1}`.
- `TestNodeProgress_Leaf_Incomplete`: Non-complete leaf returns `{0, 1}`.
- `TestNodeProgress_NonLeaf_AllComplete`: Parent with 3 complete children returns `{3, 3}`.
- `TestNodeProgress_NonLeaf_Mixed`: Parent with 2 complete, 1 incomplete returns `{2, 3}`.
- `TestNodeProgress_DeepNesting`: Grandparent aggregates across all descendants (not just direct children). E.g., parent has 1 leaf child + 1 non-leaf child with 2 leaves -> grandparent is `{n, 3}`.
- `TestNodeProgress_NoChildren`: Non-leaf with no children (edge case) returns `{0, 0}`.
- `TestProgress_Fraction`: Verify float division and zero-total handling.
- `TestProgress_String`: Verify `"2/3 leaves done"` format.
- `TestTreeProgress_FullTree`: Build a multi-level tree and verify progress map contains entries for all non-leaf nodes with correct counts.

## Boundaries

- Do NOT implement any UI rendering of progress (that's in spec-planning-ux).
- Do NOT store progress — it's always computed fresh.
- Do NOT modify SpecNode or SpecTree types.
