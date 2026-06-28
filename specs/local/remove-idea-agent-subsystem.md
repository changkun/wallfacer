---
title: Remove the idea-agent (brainstorm) subsystem and the test-only flow
status: complete
depends_on: []
affects:
  - internal/runner/
  - internal/handler/
  - internal/flow/
  - internal/agents/
  - internal/store/
  - internal/envconfig/
  - internal/constants/
  - internal/apicontract/
  - frontend/src/
  - docs/
effort: large
created: 2026-06-28
updated: 2026-06-28
author: changkun
dispatched_task_id: null
---

# Feature: Remove the idea-agent (brainstorm) subsystem and the test-only flow

## Goal

Retire two built-ins that clutter the agent/flow catalog a fresh user first sees:

- The **idea-agent / brainstorm** feature in full: the `ideate` agent, the
  `brainstorm` flow, the dedicated runner (`runner/ideate*.go`), the HTTP
  surface (`/api/ideate`), the `idea-agent` task kind, the ideation config
  knobs, and the composer/board UI that special-cases it. Brainstorming is
  replaced by an ordinary scheduled routine plus an example prompt, which the
  routines engine already supports.
- The **test-only** flow. Test verification already runs inside the `implement`
  flow (the agon verification step), so a standalone test flow is redundant.

This is a deliberate simplification of the surface that motivated the unified
agent-graph work: fewer, clearer built-ins. It is a removal, not a rewrite.

## Decisions

1. **`TaskKind` stays; only `TaskKindIdeaAgent` goes.** The type is shared by
   `TaskKindTask`/`TaskKindPlanning`/`TaskKindRoutine`. Remove the `idea-agent`
   constant, `Task.IsIdeaAgent()`, and `SandboxActivityIdeaAgent`.

2. **No data deletion.** `Task.Kind` is a free-form string column. Historical
   rows with `Kind == "idea-agent"` persist and render as ordinary cards once
   the special handling is gone. No schema change, no destructive migration.

3. **Legacy routines degrade gracefully.** `flow/registry.go`'s
   `ResolveLegacyKind` maps `idea-agent -> brainstorm`; drop that case. A
   routine whose `RoutineSpawnFlow` is empty (or was the removed `brainstorm`)
   resolves to the default flow (`implement`) via the existing fallback, with a
   one-line log on first reconcile. `RoutineSpawnKind` field is retained on the
   wire for back-compat but is no longer special-cased.

4. **`Flow.SpawnKind` is retained.** Its only consumer was the brainstorm flow.
   Keeping the (now always-empty) generic field avoids rippling into the flow
   write API just shipped in M6.2a; it is a candidate for a later dead-field
   sweep, called out here so it is not forgotten.

5. **Replacement = routine + example prompt.** Brainstorming is achievable
   today by a scheduled routine running an ordinary task. Ship one example
   brainstorm prompt so the capability is discoverable (exact placement decided
   in the implementation slice: composer example prompts or a seeded routine).

6. **Config knobs removed.** `ideation_exploit_ratio`, `ideation_categories`,
   and the ignore-pattern export leave `GET/PUT /api/config`; the matching
   `Handler`/`Runner` accessors and `constants.DefaultIdeationExploitRatio` go
   with them.

## Surface removed

- HTTP: `GET/POST/DELETE /api/ideate` (status/trigger/cancel).
- Built-in flows: `brainstorm`, `test-only`.
- Built-in agent: `ideate` (the `IdeaAgent` descriptor; `inspector.go`).
- Task kind: `idea-agent`; sandbox activity `idea_agent`.
- Env: `WALLFACER_SANDBOX_IDEA_AGENT`.
- Config: `ideation_exploit_ratio`, `ideation_categories`.
- UI: composer empty-prompt allowance, idea-agent task badges/categories, the
  brainstorm default in the routine creator.

## Status: complete

All milestones shipped. Commits: R1 `ba8fe252` (spec); R2 `7684dde5` (frontend);
R3 `08bc4611` (HTTP/config); R4 `9165e5f1` (runner engine); R5 `261011c2` +
`7a3a4327` (catalog/kind/routines + dead-callback sweep); R6 docs + example
prompt (this commit). Final gate green: `go build ./...`, `go test ./...`
(0 fail), golangci-lint 0 issues; slug-fallback safety verified so legacy
`brainstorm`/`test-only` tasks and routines resolve to `implement`.

## Milestones (each commit keeps `go build` + `go test` + vitest green)

- **R1: spec.** This document. (DONE on commit.)
- **R2: frontend.** Drop the idea-agent UI: `composer.ts` (`flowAllowsEmptyPrompt`),
  `tagBadge.ts` (idea-agent badge), `TaskCard.vue` (brainstorm categories),
  `TaskComposer.vue` (empty-prompt path), `RoutinesPage.vue` (default flow ->
  `implement`), `badges.css`; update `composer.test.ts` + `tagBadge.test.ts`.
  Frontend compiles independently of the Go changes.
- **R3: HTTP + config leaves.** Delete `handler/ideate.go`; unregister the
  `/api/ideate` routes (`apicontract/routes.go`, `cli/server.go`); remove the
  ideation config knobs (`config.go`, `handler.go`, `constants`); fix
  `config_test.go`. `TaskKindIdeaAgent` still exists, so the tree stays green.
- **R4: runner subsystem.** Delete `runner/ideate.go`, `ideate_parse.go`,
  `ideate_workspace.go` and their tests; remove the runner wiring
  (`runner.go` ideate container + exploit-ratio fn, `agent_bindings.go`,
  `container.go` activity const, `prompts.go` ideation template) and the
  `execute.go` idea-agent completion block. Remove the now-dead runner refs to
  `TaskKindIdeaAgent`.
- **R5: catalog + kind + routines rewire.** Remove `brainstorm`/`test-only`
  from `flow/builtins.go`; drop the `idea-agent` case from `registry.go`;
  remove `TaskKindIdeaAgent`/`IsIdeaAgent`/`SandboxActivityIdeaAgent`
  (`store/models.go`), the `allowedRoutineSpawnKinds` entry (`routines.go`),
  the autopilot `IsIdeaAgent` skip (`tasks_autopilot.go`), the `tasks.go`
  brainstorm branch, the `IdeaAgent` agent (`agents/builtins.go` +
  `inspector.go`), and `envconfig` `IdeaAgentSandbox`. Fix every remaining test
  (flow, agents, routines, store, runner). This is the commit that closes the
  loop; the tree compiles only once all references are gone.
- **R6: example prompt + docs.** Add the example brainstorm prompt/routine;
  delete `docs/guide/refinement-and-ideation.md`; update README + `docs/guide/*`
  to drop ideate/brainstorm/test-only and describe the routine replacement.

## Test strategy

- Per CLAUDE.md, the removal carries regression guards, not just deletions:
  a test asserting the built-in flow set is exactly `{implement}` (plus user
  flows), a test asserting `BuiltinAgents` no longer contains `ideate`, and a
  routines test proving a scheduled routine fires against an ordinary flow with
  no idea-agent path. Deleted feature tests are removed; surviving tests that
  used `test-only`/`brainstorm` as fixtures switch to `implement`.
- `make build` (golangci-lint + vite-ssg) and `make test` (incl. the topos
  import guard) green before done.

## Risks

- **Wide blast radius (~46 files).** Mitigate with the leaf-first ordering above
  so each commit compiles; never remove `TaskKindIdeaAgent` until its last
  reference is gone (R5).
- **Hidden legacy data.** Existing idea-agent task/routine records must not
  crash dispatch. Decision 2/3 keep them readable; R5 includes the reconcile
  fallback + a test.
- **`test-only` is a test fixture.** Several flow/store tests use it as a
  convenient non-default flow slug; they move to `implement` in R5.

## Out of scope

- Removing the generic `Flow.SpawnKind` field (decision 4) — later dead-field sweep.
- Any change to the unified agent-graph UI (its own spec); this only shrinks the
  catalog that surface renders.
