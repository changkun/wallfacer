---
title: Retire /api/ideate facade or document it as routine sugar
status: drafted
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
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

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
