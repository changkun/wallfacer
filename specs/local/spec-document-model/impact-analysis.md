---
title: Impact Analysis
status: validated
track: local
depends_on:
  - specs/local/spec-document-model/spec-tree-builder.md
affects:
  - internal/spec/
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Impact Analysis

## Goal

Implement reverse dependency computation and impact analysis. Given a spec, answer "what specs depend on me (directly and transitively)?" This is computed on the fly by inverting `depends_on` edges — no stored `depended_on_by` field.

## What to do

1. Create `internal/spec/impact.go` with:

   - `Impact` struct:
     ```go
     type Impact struct {
         Direct     []string // spec paths that directly depend on the target
         Transitive []string // all spec paths reachable via reverse dependency edges (excluding direct)
     }
     ```

   - `BuildReverseIndex(tree *SpecTree) map[string][]string` — inverts all `depends_on` edges. For each spec A that has `depends_on: [B, C]`, add A to the reverse list of B and C. Returns a map from spec path to list of direct dependents.

   - `ComputeImpact(tree *SpecTree, specPath string) (*Impact, error)` — computes direct and transitive impact of a spec:
     1. Build reverse index.
     2. Direct: specs in the reverse index for `specPath`.
     3. Transitive: BFS/DFS from direct dependents through the reverse index. Exclude the direct set from the transitive set.
     4. For non-leaf specs: also include specs that depend on any leaf in the subtree (the non-leaf's design governs its children).

   - `UnblockedSpecs(tree *SpecTree, completedPath string) []*SpecNode` — given a spec that just reached `complete`, find all specs whose `depends_on` are now fully satisfied (all dependencies are `complete`). Useful for surfacing what's newly ready to dispatch.

2. The reverse index and impact queries should handle:
   - Cross-tree dependencies (specs depending on specs in different tracks).
   - Non-leaf impact expansion (non-leaf's children are part of its "scope").
   - Missing targets in `depends_on` gracefully (skip, don't crash — validation catches these).

## Tests

- `TestBuildReverseIndex_Simple`: A depends on B. Reverse index has B -> [A].
- `TestBuildReverseIndex_Multiple`: A depends on B and C. Reverse index has B -> [A], C -> [A].
- `TestBuildReverseIndex_SharedDep`: A and B both depend on C. Reverse index has C -> [A, B].
- `TestBuildReverseIndex_NoDeps`: Spec with no dependencies. Reverse index is empty for that spec.
- `TestComputeImpact_DirectOnly`: A depends on B (no further chain). Impact of B: direct=[A], transitive=[].
- `TestComputeImpact_Transitive`: A -> B -> C. Impact of C: direct=[B], transitive=[A].
- `TestComputeImpact_Diamond`: A -> C, B -> C, D -> A, D -> B. Impact of C: direct=[A, B], transitive=[D].
- `TestComputeImpact_CrossTree`: Dependency across tracks still resolves.
- `TestComputeImpact_NonLeaf`: Non-leaf spec's impact includes dependents of its children.
- `TestComputeImpact_NoImpact`: Spec with no dependents returns empty impact.
- `TestComputeImpact_MissingTarget`: `depends_on` pointing to non-existent spec — no crash.
- `TestUnblockedSpecs_Simple`: B depends on A. A completes -> B is unblocked.
- `TestUnblockedSpecs_MultiDep`: C depends on A and B. Only A completes -> C not unblocked. Both complete -> C unblocked.
- `TestUnblockedSpecs_AlreadyComplete`: Already-complete specs are not returned as unblocked.

## Boundaries

- Do NOT implement event-driven notifications (e.g., "notify when spec completes").
- Do NOT implement the `affects`-based file-level impact detection (that's in spec-drift-detection).
- Do NOT store the reverse index — compute it fresh each time.
- Do NOT modify SpecTree or SpecNode types.
