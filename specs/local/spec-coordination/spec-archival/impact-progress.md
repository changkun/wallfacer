---
title: "Archival: Impact analysis and progress tracking exclusions"
status: validated
depends_on:
  - specs/local/spec-coordination/spec-archival/core-model.md
affects:
  - internal/spec/impact.go
  - internal/spec/impact_test.go
  - internal/spec/progress.go
  - internal/spec/progress_test.go
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Archival: Impact analysis and progress tracking exclusions

## Goal

Update `internal/spec/impact.go` and `internal/spec/progress.go` so that archived
specs are invisible to the live graph: they contribute no impact, do not appear as
unblocked candidates, count as satisfied dependencies, and are excluded from all
progress totals.

## What to do

### `internal/spec/impact.go`

1. **`Adjacency(tree)`** — skip archived specs as both sources and sinks when
   building the forward adjacency map. In the loop over `tree.All`:
   ```go
   for path, node := range tree.All {
       if node.Value == nil || node.Value.Status == StatusArchived {
           continue  // archived specs contribute no edges
       }
       for _, dep := range node.Value.DependsOn {
           // also skip edges that point to archived specs
           if depNode, ok := tree.All[dep]; ok && depNode.Value != nil &&
               depNode.Value.Status == StatusArchived {
               continue
           }
           adj[path] = append(adj[path], dep)
       }
   }
   ```

2. **`ComputeImpact(tree, specPath)`** — return empty `Impact{}` immediately if the
   target spec is archived:
   ```go
   node, ok := tree.All[specPath]
   if !ok || node.Value == nil || node.Value.Status == StatusArchived {
       return &Impact{}
   }
   ```
   Also skip archived nodes when collecting seed paths from non-leaf subtrees
   (`collectLeafPaths` is called here — see item 3).

3. **`collectLeafPaths(node, paths)`** — skip archived nodes:
   ```go
   func collectLeafPaths(node *Node, paths *[]string) {
       if node.Value != nil && node.Value.Status == StatusArchived {
           return
       }
       if node.IsLeaf {
           *paths = append(*paths, node.Value.Path)
           return
       }
       for _, child := range node.Children {
           collectLeafPaths(child, paths)
       }
   }
   ```

4. **`UnblockedSpecs(tree, completedPath)`** — skip archived candidates in the
   results loop:
   ```go
   for _, candidate := range reverse[completedPath] {
       depNode, ok := tree.All[candidate]
       if !ok || depNode.Value == nil || depNode.Value.Status == StatusArchived {
           continue
       }
       // ... rest of existing logic
   }
   ```

5. **`allDepsComplete(tree, node)`** — treat archived dependencies as satisfied
   (same semantics as `StatusComplete`):
   ```go
   for _, dep := range node.Value.DependsOn {
       depNode, ok := tree.All[dep]
       if !ok || depNode.Value == nil {
           return false
       }
       if depNode.Value.Status != StatusComplete &&
           depNode.Value.Status != StatusArchived {
           return false
       }
   }
   return true
   ```

### `internal/spec/progress.go`

6. **`NodeProgress(node)`** — two guards:
   - Leaf guard: if the leaf is archived, return `Progress{Complete: 0, Total: 0}` (excluded from counts):
     ```go
     if node.IsLeaf {
         if node.Value == nil || node.Value.Status == StatusArchived {
             return Progress{}
         }
         if node.Value.Status == StatusComplete {
             return Progress{Complete: 1, Total: 1}
         }
         return Progress{Complete: 0, Total: 1}
     }
     ```
   - Non-leaf guard: if the non-leaf itself is archived, return `Progress{}` immediately
     (mask the entire subtree):
     ```go
     if node.Value != nil && node.Value.Status == StatusArchived {
         return Progress{}
     }
     ```

7. **`TreeProgress(tree)`** — no change needed; it calls `NodeProgress` per node,
   which now correctly excludes archived branches.

## Tests

Add to `impact_test.go`:
- `TestComputeImpact_ArchivedTarget` — `ComputeImpact` on an archived spec returns empty `Impact`
- `TestAdjacency_SkipsArchived` — archived spec's `depends_on` edges not in adjacency map;
  edges pointing to archived specs also excluded
- `TestUnblockedSpecs_SkipsArchived` — completing a dep does not surface archived candidates
- `TestAllDepsComplete_ArchivedDepSatisfied` — spec with one archived dep and one complete dep:
  `allDepsComplete` returns `true`
- `TestComputeImpact_ArchivedNonLeaf` — non-leaf archived spec: `Impact{}` (no descendants included)

Add to `progress_test.go`:
- `TestNodeProgress_ArchivedLeaf` — archived leaf: `Progress{0, 0}` (excluded)
- `TestNodeProgress_ArchivedNonLeaf` — archived non-leaf: `Progress{0, 0}` (subtree masked)
- `TestNodeProgress_MixedWithArchived` — non-leaf with 2 complete leaves and 1 archived leaf:
  `Progress{Complete: 2, Total: 2}` (archived leaf excluded from denominator)

## Boundaries

- Do NOT change the `Impact` or `Progress` types
- Do NOT change `TreeProgress` logic beyond what `NodeProgress` already handles
- Do NOT touch `validate.go`, handlers, or UI code
- Do NOT implement drift detection skip (that is future work in spec-drift-detection.md)
