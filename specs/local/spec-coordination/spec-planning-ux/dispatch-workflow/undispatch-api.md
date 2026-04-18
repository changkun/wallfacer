---
title: Undispatch API Endpoint
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/dispatch-workflow/dispatch-api.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/
  - ui/js/generated/routes.js
effort: small
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Undispatch API Endpoint

## Goal

Implement `POST /api/specs/undispatch` — cancels the linked board task, clears `dispatched_task_id` from the spec file, and returns the spec to `validated` status. This allows users to undo a dispatch and refine the spec before re-dispatching.

## What to do

1. **Add route** in `internal/apicontract/routes.go`: define `UndispatchSpecs` route as `POST /api/specs/undispatch` in the specs section.

2. **Create handler** in `internal/handler/specs_dispatch.go` (extend the file created by the dispatch-api task):

   ```
   func (h *Handler) UndispatchSpecs(w http.ResponseWriter, r *http.Request)
   ```

   Request body:
   ```json
   {"paths": ["specs/local/foo.md"]}
   ```

   Handler logic:
   a. Parse request body, validate `paths` is non-empty
   b. For each spec path, read and parse the spec file
   c. Validate: spec must have a non-null `dispatched_task_id`
   d. Look up the linked task by ID. If the task exists and is in a cancellable state (`backlog`, `in_progress`, `waiting`, `failed`), cancel it via `store.UpdateTaskStatus` to `cancelled`
   e. Use `spec.UpdateFrontmatter()` to clear `dispatched_task_id` (set to `null`) and set `status` back to `validated`, update `updated` timestamp
   f. If the task is already `done` or `cancelled`, still clear the spec's `dispatched_task_id` but skip task cancellation

   Response:
   ```json
   {
     "undispatched": [{"spec_path": "...", "task_id": "..."}],
     "errors": [{"spec_path": "...", "error": "..."}]
   }
   ```

3. **Run `make api-contract`** to regenerate route artifacts.

## Tests

- `TestUndispatchSpecs_CancelsTask` — undispatch a spec whose task is in `backlog`, verify task cancelled and spec frontmatter cleared
- `TestUndispatchSpecs_RunningTask` — undispatch a spec whose task is `in_progress`, verify task cancelled
- `TestUndispatchSpecs_DoneTask` — undispatch a spec whose task is `done`, verify spec frontmatter cleared but task status unchanged
- `TestUndispatchSpecs_NotDispatched` — attempt to undispatch a spec without `dispatched_task_id`, expect 400 error
- `TestUndispatchSpecs_SpecReturnsToValidated` — verify spec status is set back to `validated` after undispatch

## Boundaries

- Do NOT handle dependent task cleanup (if task B depends on task A and A is undispatched, task B's dependency remains — the user must handle this)
- Do NOT modify the dispatch handler
- Do NOT add UI changes
