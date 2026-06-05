---
title: Retire /api/ideate facade or document it as routine sugar
status: archived
depends_on:
  - specs/local/backend-redundancy-cleanup.md
  - specs/local/vue-frontend-migration.md
  - specs/local/routine-tasks.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/ideate.go
  - internal/cli/server.go
  - ui/js/generated/routes.js
  - frontend/src/
  - ui/js/
effort: small
created: 2026-06-01
updated: 2026-06-05
author: changkun
dispatched_task_id: null
---

## Outcome — archived, premise no longer holds (2026-06-05)

This spec assumed `/api/ideate` is **routine-backed** — that it
scaffolds a `system:ideation` routine card with
`RoutineSpawnFlow=brainstorm`, making the triple "a thin convenience
facade over `/api/routines`." That stopped being true.

The current code retired the always-on ideation routine entirely:

- `internal/handler/routines_engine.go:reconcileRoutines` now **deletes**
  any routine card still carrying the `system:ideation` tag from a
  prior deployment, via `cleanupLegacyIdeationRoutine`. Nothing creates
  one.
- `POST /api/ideate` (`TriggerIdeation`) creates a one-shot
  `Kind=idea-agent` task and runs it through the normal execute path —
  not a routine, not a `/api/routines` facade.
- `GET /api/ideate` reports running status; `DELETE /api/ideate`
  cancels in-flight idea-agent tasks. The `enabled` / `next_run_at`
  response fields are vestigial.
- `system:ideation` survives only as a legacy-cleanup lookup key.

So neither option survives review: **Option A** (route ideation through
the routines list with a `system:ideation` filter) is moot — there is no
such routine. **Option B** (annotate the triple as "a thin facade over
`/api/routines`") would document a relationship that no longer exists;
`internal/handler/ideate.go`'s in-code comments are already accurate.

No surface change is warranted. Archived rather than implemented.

**Separate follow-up (not this spec):** several docs/comments still
describe the retired routine-backed model and are stale — `CLAUDE.md`
("Ideation — `/api/ideate` (routine-backed; …)"),
`docs/internals/automation.md` ("Fire system:ideation routine"),
`internal/cli/server.go:251`, and `internal/handler/config.go:373`.
Reconciling them needs the ideation-interval scheduler path mapped and
belongs in a focused doc-accuracy pass, not here.

# Retire /api/ideate facade or document it as routine sugar

`GET/POST/DELETE /api/ideate` is **routine-backed**: it scaffolds a
routine card with `Tags=["system:ideation"]` and
`RoutineSpawnFlow=brainstorm`. Once
[routine-tasks.md](../routine-tasks.md) shipped, routines became the
primary primitive, accessible at `/api/routines`. The `/api/ideate`
triple is now a thin convenience facade.

Two options — pick during implementation:

## Option A — remove the facade entirely

Surface ideation in the routines list with a `tag = system:ideation`
filter on the frontend. The Settings → Ideation panel becomes a
prefilter view over the existing routines list, not its own
endpoint group.

Pro: pure surface reduction (three routes deleted).
Con: requires the frontend ideation UI to learn the routines API.

## Option B — keep the facade, document it as sugar

Annotate `/api/ideate` in the contract as a thin facade over
`/api/routines` with the `system:ideation` tag. Leave the handler in
place; add a comment block at the top of `internal/handler/ideate.go`
pointing readers at `routines.go`.

Pro: zero frontend change. Pro: keeps the Settings page tractable.
Con: surface stays at three routes.

## Recommendation

Default to **Option B** unless the cloud or multi-user roadmap
demands a single routines-tag query path. Re-evaluate after
multi-user.

## Acceptance (Option A)

- `/api/ideate` GET/POST/DELETE removed from
  `internal/apicontract/routes.go`.
- `internal/handler/ideate.go` deleted or reduced to the
  routine-scaffolding helper that `/api/routines` already calls
  internally.
- Frontend Settings → Ideation rewritten against
  `/api/routines?tag=system:ideation`.

## Acceptance (Option B)

- Contract entry for the three routes gains a `// thin facade over
  /api/routines with system:ideation tag` comment.
- A short doc block lands at the top of
  `internal/handler/ideate.go`.
- Pointer added in `docs/internals/api-and-transport.md`.
