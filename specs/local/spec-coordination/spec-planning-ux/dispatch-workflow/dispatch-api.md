---
title: Dispatch API Endpoint
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/dispatch-workflow/spec-frontmatter-writer.md
  - specs/local/spec-coordination/spec-planning-ux/dispatch-workflow/task-spec-source-field.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/
  - internal/spec/validate.go
  - ui/js/generated/routes.js
effort: medium
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Dispatch API Endpoint

## Goal

Implement `POST /api/specs/dispatch` — the atomic dispatch endpoint that creates board tasks from validated specs and writes `dispatched_task_id` back to spec files. This is the core backend for the dispatch workflow: both single-spec and multi-spec dispatch use this endpoint.

## What to do

1. **Add route** in `internal/apicontract/routes.go`: define `DispatchSpecs` route as `POST /api/specs/dispatch` in the specs section.

2. **Enforce leaf-only dispatch** in the handler: validate that each spec is a leaf (no child specs in a corresponding subdirectory) before dispatching. The `checkDispatchConsistency` rule in `internal/spec/validate.go` enforces this at the validation level; the handler adds a filesystem check via `spec.IsLeafPath()`.

3. **Create handler** in `internal/handler/specs_dispatch.go` (new file):

   ```
   func (h *Handler) DispatchSpecs(w http.ResponseWriter, r *http.Request)
   ```

   Request body:
   ```json
   {"paths": ["specs/local/foo.md", "specs/local/bar.md"], "run": false}
   ```

   Handler logic:
   a. Parse request body, validate `paths` is non-empty
   b. For each spec path, read and parse the spec file from the workspace filesystem
   c. Validate each spec:
      - Status must be `validated` (or `stale` for re-dispatch, where `dispatched_task_id` was previously set)
      - `dispatched_task_id` must be `null` (not already dispatched), unless re-dispatching a stale spec
   d. Resolve dependency wiring: for each spec's `depends_on`, look up the referenced spec's `dispatched_task_id` to build the task's `DependsOn` list. Dependencies that aren't dispatched yet get empty deps (the task will have no blockers for those).
   e. Build `batchTaskInput` entries:
      - `Ref` = spec path (for symbolic dependency resolution within the batch)
      - `Prompt` = spec body (markdown content after frontmatter)
      - `Tags` = `["spec-dispatched"]` plus the spec's track
      - `SpecSourcePath` = spec path (reverse linkage)
      - `DependsOnRefs` = resolved dependency task IDs from step (d), plus symbolic refs for specs being dispatched in the same batch
   f. Call the internal batch task creation logic (extract and reuse the core of `BatchCreateTasks` or call `store.CreateTaskWithOptions` directly in topological order)
   g. On success, use `spec.UpdateFrontmatter()` to write `dispatched_task_id` back to each spec file
   h. If `run` is true, also transition the created tasks from `backlog` to `in_progress`

   Response:
   ```json
   {
     "dispatched": [{"spec_path": "...", "task_id": "..."}],
     "errors": [{"spec_path": "...", "error": "..."}]
   }
   ```

4. **Run `make api-contract`** to regenerate route artifacts.

5. **Error handling**: if task creation succeeds but frontmatter write fails, the handler should cancel/delete the created tasks and return an error (atomic: both succeed or both fail).

## Tests

- `TestDispatchSpecs_SingleSpec` — dispatch one validated spec, verify task created with correct prompt and spec body, verify `dispatched_task_id` written to spec file
- `TestDispatchSpecs_BatchWithDependencies` — dispatch multiple specs where spec B depends on spec A, verify task B's `DependsOn` includes task A's ID
- `TestDispatchSpecs_RejectsNonValidated` — attempt to dispatch a `drafted` spec, expect 400 error
- `TestDispatchSpecs_RejectsAlreadyDispatched` — attempt to dispatch a spec that already has `dispatched_task_id`, expect 400 error
- `TestDispatchSpecs_SpecSourcePath` — verify created tasks have `SpecSourcePath` set to the spec path
- `TestDispatchSpecs_RunFlag` — dispatch with `run: true`, verify tasks transition to `in_progress`
- `TestDispatchSpecs_AtomicRollback` — simulate frontmatter write failure, verify no tasks are created
- `TestDispatchSpecs_EmptyPaths` — empty paths array returns 400

## Boundaries

- Do NOT implement undispatch (separate task)
- Do NOT implement the task completion hook (separate task)
- Do NOT implement drift assessment or layer 2/3 feedback
- Do NOT modify frontend code (separate tasks)
- Do NOT implement the planning agent `/dispatch` command (nice-to-have)

## Implementation notes

- **Leaf-only dispatch enforced**: The handler checks `spec.IsLeafPath()` (filesystem-based leaf detection) and rejects non-leaf specs. The `checkDispatchConsistency` validation rule and `isLeaf` parameter on `ValidateSpec` were temporarily removed but have been restored — non-leaf dispatch broke progress tracking, drift propagation, and impact analysis.
- **Simplified topological sort**: The spec suggested Kahn's algorithm for batch ordering, but since dependencies are resolved via pre-assigned UUIDs (not creation order), sequential creation works correctly without topological sort. The pre-assigned UUIDs ensure cross-batch references are valid regardless of creation order.
- **Partial success not supported**: The spec's response format shows both `dispatched` and `errors` suggesting partial success. The implementation returns all-or-nothing for task creation (any creation failure rolls back), but validation errors are collected per-spec before creation begins. A batch with some invalid specs and some valid specs will only dispatch the valid ones.
