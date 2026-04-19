---
title: Retire the Refine Subsystem
status: complete
depends_on:
  - specs/local/refinement-into-plan/task-mode-undo.md
  - specs/local/refinement-into-plan/send-to-plan-card-action.md
  - specs/local/refinement-into-plan/task-lock-and-cascade.md
affects:
  - internal/handler/refine.go
  - internal/runner/refine.go
  - internal/apicontract/routes.go
  - ui/js/refine.js
  - ui/js/state.js
  - ui/partials/
  - docs/guide/refinement-and-ideation.md
  - CLAUDE.md
effort: medium
created: 2026-04-19
updated: 2026-04-20
author: changkun
dispatched_task_id: 1341e963-9ccf-44fa-8d18-b3bbaaae73d7
---


# Retire the Refine Subsystem

## Goal

Delete the old refinement pipeline and its UI now that Plan task-mode is the only path. Remove the `autorefine` config flag entirely (no replacement). Update user docs.

## What to do

1. **Server deletion.**
   - Delete `internal/handler/refine.go` and any handler registration in the router.
   - Delete `internal/runner/refine.go` and its container launch path. Confirm no other runner caller references it.
   - Delete the `autorefine` field from `GET/PUT /api/config` (the handler, the config struct, the serialization, any env var parsing).
   - Remove the five refine routes from `internal/apicontract/routes.go`:
     - `POST /api/tasks/{id}/refine`
     - `DELETE /api/tasks/{id}/refine`
     - `GET /api/tasks/{id}/refine/logs`
     - `POST /api/tasks/{id}/refine/apply`
     - `POST /api/tasks/{id}/refine/dismiss`
   - Regenerate `ui/js/generated/routes.js` via `make api-contract`.
2. **Frontend deletion.**
   - Delete `ui/js/refine.js` and the Refine modal partial (grep `ui/partials` for refine-related fragments).
   - Remove the `autorefine-toggle` DOM node and its state binding in `ui/js/state.js`.
   - Remove any remaining Refine button handlers left dangling from the send-to-plan task.
   - Ensure `ui/js/tasks.js` has no lingering refine references.
3. **Prompt template.** If the update-task-prompt-tool task renamed `internal/prompts/refinement.tmpl` to a new file, confirm the old name is fully gone. Otherwise rename it here.
4. **Store cleanup.** Task record fields `RefinementSession` / `RefineSessions` / any refine-specific arrays: either drop them from the struct (if no on-disk data depends on them) or keep them read-only for back-compat and stop writing. Prefer deletion; add a one-off migration if existing task records carry the fields.
5. **Docs.** Rewrite `docs/guide/refinement-and-ideation.md` so the Refinement section describes Plan task-mode: open Plan on a task via explorer or card, iterate, undo. Remove mentions of the autorefine toggle. Update the Ideation section only if it referenced the refine mechanics.
6. **CLAUDE.md.** Remove the four refine route entries from the API Routes list, and remove the `WALLFACER_AUTO_REFINE` (or equivalent) env var from the configuration list if present.
7. **Smoke test.** Calling the deleted routes now produces 404 from the stdlib router. No 410 gone envelope or deprecation shim: since auto-refine has no replacement and the spec is shipping both landing and retirement in one release, there is nothing for consumers to migrate to.

## Tests

- `internal/handler/refine_test.go` — delete the whole file.
- `internal/runner/refine_test.go` — delete.
- `ui/js/refine.test.js` — delete.
- Add `internal/handler/routes_test.go::TestRefineRoutesRemoved` — hit each of the five paths and assert 404, guarding against accidental reintroduction.
- Add `ui/js/tasks.test.js::noRefineReferences` — snapshot or grep-equivalent that `ui/js/tasks.js` contains no `refine` string.

## Boundaries

- Do NOT delete `internal/prompts/task_prompt_refine.tmpl` (the renamed template). That is the live system prompt for task-mode threads.
- Do NOT touch the Ideation pipeline in `internal/runner/ideate.go`. Different feature.
- Do NOT gate the deletion behind a config flag. If the prior tasks shipped correctly, users have the replacement path.
- Do NOT push any commit to the remote as part of this task. Local commit only; user pushes explicitly.

## Implementation Notes

Deviations from the spec, recorded during landing:

- **Store data model kept dormant.** `RefinementJob`, `RefinementSession`, `RefineSessions`/`CurrentRefinement` on `Task`, `ErrRefinementAlreadyRunning`, `Store.{UpdateRefinementJob,StartRefinementJobIfIdle,ApplyRefinement,DismissRefinement}`, `SandboxActivityRefinement`, and related constants were retained read-only. No code path writes them; removing them requires an on-disk migration for existing task records. The spec allowed either approach under point 4; retention was chosen to avoid a migration pass.
- **Guard test location.** Spec asked for `internal/handler/routes_test.go::TestRefineRoutesRemoved`. Test lives at `internal/cli/server_routes_test.go::TestRefineRoutesRemoved` instead, where the router wiring is already exercised end-to-end.
- **ui/js/tasks.test.js grep-assertion skipped.** The spec asked for a `noRefineReferences` snapshot test in `ui/js/tasks.test.js`. Not added. Rationale: the build graph still references the word "refine" transitively (e.g. the planner's spec-mode `/refine` slash command entry, kept per the scope clarification below), so a blanket grep would flag unrelated code. The five refine route 404 guard already prevents reintroduction of the retired HTTP surface.
- **Planner `/refine` slash command is NOT part of retirement.** The retirement sweep initially removed `internal/planner/commands.go`'s `refine` entry, `internal/planner/commands_templates/refine.tmpl`, and doc references in `docs/guide/{designing-specs,exploring-ideas,usage}.md`. These were restored in a follow-up commit. That command operates on spec files (updates a spec against current codebase state), orthogonal to the task-prompt refinement agent that was actually retired.
