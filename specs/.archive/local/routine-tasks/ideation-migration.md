---
title: Migrate ideation to a system routine
status: archived
depends_on:
  - specs/local/routine-tasks/engine-integration.md
  - specs/local/routine-tasks/routine-card-ui.md
affects:
  - internal/handler/ideate.go
  - internal/handler/handler.go
  - internal/handler/config.go
  - internal/cli/server.go
  - ui/js/ideate.js
  - docs/guide/refinement-and-ideation.md
  - CLAUDE.md
  - AGENTS.md
effort: medium
created: 2026-04-18
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---


# Migrate ideation to a system routine

## Goal

Collapse the ideation singleton onto the new routine primitive. The
scheduler/timer plumbing in `internal/handler/ideate.go` is removed; a
`system:ideation`-tagged routine card is the source of truth for the
ideation schedule. Legacy `/api/ideate` endpoints and the `ideation*`
config keys become thin views onto that routine. Nothing user-visible
regresses.

## What to do

1. On startup (in `StartRoutineEngine` or an adjacent bootstrap), look for
   a routine card with `Tags` containing `system:ideation`. If none
   exists, create one with:
   - `Kind: TaskKindRoutine`
   - `Tags: ["system:ideation"]`
   - `RoutineSpawnKind: TaskKindIdeaAgent`
   - `RoutineIntervalSeconds`: seeded from existing `ideation_interval`
     config (default 60 * 60 if unset)
   - `RoutineEnabled`: seeded from existing `ideation` config (default
     true)
   - `Prompt`: short fixed string like "Ideation routine" (the actual
     per-fire prompt is built by `BuildIdeationPrompt`).

   This is idempotent — subsequent starts find the existing routine.

2. Remove from `internal/handler/handler.go`:
   - `ideationMu`, `ideationEnabled`, `ideationInterval`, `ideationNextRun`,
     `ideationTimer`, `ideationExploitRatio` fields.
   - Any code that touches them.

3. Remove from `internal/handler/ideate.go`:
   - `StartIdeationWatcher`, `maybeScheduleNextIdeation`, `scheduleIdeation`.
   - Keep `createIdeaAgentTask` but route it through the engine's fire
     func so manual `POST /api/ideate` still works.

4. `GET/POST/DELETE /api/ideate` become thin shims:
   - `GET /api/ideate` — find the system routine, return `{enabled,
     running, next_run_at}` (running = any in_progress idea-agent task).
   - `POST /api/ideate` — `h.routineEngine.Trigger(systemRoutineID)`.
   - `DELETE /api/ideate` — cancel the active idea-agent task if any
     (unchanged behavior).

5. `internal/handler/config.go` — the `ideation`, `ideation_interval`,
   `ideation_next_run`, `ideation_exploit_ratio` keys become read/write
   views onto the system routine's fields. Writes call the existing
   store writers; reads look up the routine and format its fields.
   `ideation_exploit_ratio` stays on the handler for now (or moves to a
   tag on the routine card) — pick whichever keeps the patch smaller;
   justify in the commit message.

6. `internal/cli/server.go` — remove the separate `StartIdeationWatcher`
   call. `StartRoutineEngine` now covers both.

7. UI (`ui/js/ideate.js`):
   - Settings panel still shows the ideation toggle/interval/trigger
     controls. They now POST/PATCH to `/api/routines/{system-id}` (or the
     legacy `/api/ideate` shims — pick whichever fits the existing
     controls with less churn).
   - The system routine also appears as a routine card on the board (with
     a "system" chip). No special hiding; users can edit its schedule
     from either surface.

8. Documentation:
   - `docs/guide/refinement-and-ideation.md` — note that ideation is now
     a preinstalled routine; controls are available in Settings
     (legacy) and on the board (new).
   - `CLAUDE.md` and `AGENTS.md` — update the ideation and routines
     sections; keep the legacy endpoint aliases documented.

## Tests

- `internal/handler/ideate_test.go` (migrate existing tests, add new):
  - `TestBootstrap_CreatesSystemIdeationRoutine` — fresh store yields
    exactly one routine with `Tags: ["system:ideation"]`.
  - `TestBootstrap_Idempotent` — second startup does not duplicate.
  - `TestBootstrap_SeedsFromLegacyConfig` — pre-existing
    `ideation_interval=30` config yields a routine with 30-minute
    interval.
  - `TestConfigKeys_AreViewsOntoRoutine` — `PUT /api/config` with
    `ideation_interval=15` updates the system routine's
    `RoutineIntervalSeconds` and vice versa.
  - `TestLegacyIdeateEndpoints_WorkViaShim` — `GET/POST/DELETE /api/ideate`
    behave as before.
  - `TestDisableIdeation_StopsFiring` — set `ideation=false`, advance
    fake clock past interval, assert no idea-agent task spawns.

- UI smoke test (`ui/js/tests/ideate.test.js`): existing ideation controls
  still PATCH the correct endpoint and reflect the server state.

## Boundaries

- No new cron-expression feature.
- Do not remove the legacy `/api/ideate` endpoints — they stay as shims
  for back-compat. Plan a separate spec to retire them once downstream
  clients (if any) migrate.
- Do not touch the underlying `runner.RunIdeation` or `BuildIdeationPrompt`
  — the migration is purely about who calls them.
- No changes to rejected-idea history or exploit-ratio math.
