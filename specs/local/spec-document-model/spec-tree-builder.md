---
title: Spec Tree Builder
status: validated
track: local
depends_on:
  - specs/local/spec-document-model/spec-model-types.md
affects:
  - internal/spec/
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Spec Tree Builder

## Goal

Build the spec tree from the filesystem, detecting parent-child relationships from directory nesting and leaf vs non-leaf status from subdirectory presence. This is the core data structure that validation, progress tracking, and impact analysis operate on.

## What to do

1. Create `internal/spec/tree.go` with:

   - `SpecNode` struct:
     ```go
     type SpecNode struct {
         Spec     *Spec       // parsed spec document
         Parent   *SpecNode   // nil for root-level specs
         Children []*SpecNode // child specs from subdirectory
         IsLeaf   bool        // true if no children
         Depth    int         // 0 for root-level specs
     }
     ```

   - `SpecTree` struct:
     ```go
     type SpecTree struct {
         Roots []*SpecNode            // top-level specs (depth 0)
         All   map[string]*SpecNode   // all nodes indexed by relative path
     }
     ```

   - `BuildTree(specsDir string) (*SpecTree, error)` — walks the `specs/` directory structure:
     1. For each `specs/<track>/` directory, scan for `.md` files (depth 0 specs).
     2. For each `<name>.md`, check if `<name>/` subdirectory exists. If so, scan it for child specs.
     3. Recurse to arbitrary depth: children can have their own subdirectories.
     4. Parse each spec file via `ParseFile()`.
     5. Wire parent-child pointers and set `IsLeaf`/`Depth` fields.
     6. Index all nodes in `All` map by their relative path.

   - `(t *SpecTree) Node(path string) (*SpecNode, bool)` — lookup a node by relative path.

   - `(t *SpecTree) Leaves() []*SpecNode` — return all leaf nodes.

   - `(t *SpecTree) ByTrack(track SpecTrack) []*SpecNode` — return root nodes for a track.

2. The tree builder must handle:
   - Orphan directories (subdirectory exists but no corresponding `.md` file) — include them as warnings, not errors. Don't skip their children.
   - Orphan specs (`.md` file with empty or missing subdirectory when it has children in the tree) — these are just leaf specs, not errors.
   - Specs at arbitrary depth (3+ levels of nesting).

## Tests

- `TestBuildTree_SingleSpec`: One spec file, no subdirectory. Tree has one root, one leaf.
- `TestBuildTree_ParentWithChildren`: `foo.md` + `foo/bar.md` + `foo/baz.md`. Parent is non-leaf with 2 children.
- `TestBuildTree_DeepNesting`: 3 levels deep. Verify depth field and parent chain.
- `TestBuildTree_MultipleTracks`: Specs in `foundations/` and `local/`. Verify `ByTrack()` returns correct subsets.
- `TestBuildTree_LeafDetection`: Mix of leaf and non-leaf specs. Verify `IsLeaf` on each.
- `TestBuildTree_AllIndex`: Verify `All` map contains every spec by path.
- `TestBuildTree_Leaves`: Verify `Leaves()` returns only leaf nodes.
- `TestBuildTree_OrphanDirectory`: Subdirectory without parent `.md` — children still parsed, tree still builds.
- `TestBuildTree_EmptySubdirectory`: `.md` file with empty subdirectory — spec is still a leaf.
- `TestBuildTree_EmptySpecsDir`: No specs at all — empty tree, no error.

Use `t.TempDir()` to create test directory structures with small spec files.

## Boundaries

- Do NOT implement dependency DAG resolution — that uses `depends_on` and is in cross-spec-validation.
- Do NOT implement validation rules.
- Do NOT implement progress tracking or impact analysis.
- Do NOT skip files that fail to parse — collect parse errors and return them alongside the tree.
