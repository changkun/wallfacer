---
title: "Dispatch → validated + folder dispatch"
status: drafted
depends_on: []
affects:
  - internal/handler/specs_dispatch.go
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
effort: medium
---

# Dispatch → `validated` + Folder Dispatch

Two tightly linked concerns:
1. **Small write**: on dispatch, write `status: validated` alongside
   `dispatched_task_id`. Defensive today; load-bearing for folder
   dispatch.
2. **Folder dispatch**: lift the "non-leaf specs cannot be dispatched"
   rejection so a design spec can dispatch its whole subtree in one
   batch, with every spec in the subtree marked `validated`.

---

## Current State

`DispatchSpecs` in `internal/handler/specs_dispatch.go`:

- Pre-dispatch guard rejects specs not at `validated` (line 85).
- Writes `dispatched_task_id` but not `status`.
- Rejects non-leaf paths with "non-leaf specs cannot be dispatched."
- Undispatch writes `status: validated` when clearing the task ID.

---

## Part 1 — Dispatch Writes `validated` (Idempotent)

In the existing frontmatter-write phase, extend the update map:

```go
spec.UpdateFrontmatter(abs, map[string]any{
    "dispatched_task_id": taskID.String(),
    "status":             string(spec.StatusValidated),
    "updated":            time.Now(),
})
```

Idempotent on the happy path (pre-dispatch guard already required
`validated`). Becomes meaningful when folder dispatch lands — subtree
members may still be `drafted` at dispatch time.

---

## Part 2 — Folder Dispatch

### What changes

Today `DispatchSpecs` accepts one or more spec paths and requires each
to be a leaf. Under folder dispatch, accepting a non-leaf path expands
to its leaves:

1. Resolve the input path.
2. If the spec is a leaf, behave as today.
3. If the spec is a non-leaf, walk its subtree (using
   `spec.BuildTree`'s parent→children relation) and collect every
   leaf. The non-leaf itself is not dispatched (non-leaves never get
   a `dispatched_task_id`).
4. Validate each leaf individually under the existing pre-dispatch
   rules (not archived, not already dispatched, dependencies `complete`
   or `archived`).
5. Create one board task per leaf in a single batch.
6. Write each leaf's `dispatched_task_id` and `status: validated`.
7. Additionally, write `status: validated` on **every non-leaf
   ancestor in the subtree** (including the dispatched non-leaf
   itself) that was `drafted`. Design specs ship from `drafted` to
   `validated` when the user commits to implementation.

### Subtree walking

Use the existing tree structure from `BuildTree`. A helper in
`internal/spec/`:

```go
func SubtreeSpecs(tree *Tree, root string) (leaves, nonLeaves []*Node)
```

Returns leaves in DAG-dispatch order and non-leaves in any order (they
only need their status flipped, not dispatched).

### Partial failure

If one leaf in a subtree fails pre-dispatch validation (e.g., its
dependencies aren't complete), the whole batch is rejected — atomic.
Same rollback logic the existing batch dispatch uses. The user gets a
structured error listing which leaves failed and why; they fix those
specs and re-submit.

---

## Commit

After frontmatter writes, single commit via the shared
`commitSpecChanges` helper. Subject:
- Leaf dispatch: `<path>: dispatch` (as today, effectively)
- Folder dispatch: `<root>: dispatch (N leaves, M design specs validated)`

Uses the per-workspace commit mutex from
[drift-pipeline.md](drift-pipeline.md) §7.

---

## UI Implications

The focused view's Dispatch button is currently hidden for non-leaves.
Under folder dispatch:
- Show Dispatch on non-leaves that have at least one live leaf in
  the subtree (after archive filtering).
- Click → confirmation dialog with the leaf count and subtree
  summary, same pattern as the archive non-leaf confirmation.
- On success, refresh the spec tree and show impacted leaves on the
  board.

---

## Acceptance

- Dispatching a `drafted` leaf writes `status: validated` +
  `dispatched_task_id`.
- Dispatching a leaf already at `validated` is idempotent.
- Dispatching a non-leaf subtree creates one board task per leaf,
  marks every leaf `validated`, and marks every `drafted` non-leaf
  ancestor in the subtree `validated`.
- Partial failure in the batch rolls back all frontmatter writes and
  all created tasks.
- Archived specs in the subtree are skipped (dispatch guard already
  enforces this at leaf level; extend the walker to skip archived
  non-leaves too — see
  [propagation-algorithm.md](propagation-algorithm.md) archived
  pruning).
- Unit tests cover leaf, non-leaf subtree, mixed statuses in a
  subtree, partial-failure rollback.

---

## Open Questions

1. **Should folder dispatch require the non-leaf to be `validated`
   first?** If the parent is `drafted`, it hasn't been blessed for
   execution. Tentative: require the non-leaf to be `validated` (use
   [explicit-validate.md](explicit-validate.md) first), then the
   folder dispatch promotes subtree members. Keeps the validation
   gate meaningful.
2. **Independent leaf dispatch after folder dispatch.** A user might
   want to re-dispatch one leaf of an already-dispatched subtree
   (e.g., after a fix). Current dispatch rejects "already dispatched"
   — is that right? Tentative: yes, require `/wf-spec-dispatch -u`
   (undispatch) first. Folder dispatch is a one-shot.
3. **Non-leaf dispatch scope.** Some reviewers may want this as a
   separate spec, isolating risk. Tentative: keep together because
   Part 1 is meaningless without Part 2 — the defensive write on leaf
   dispatch is what makes subtree validation coherent.
