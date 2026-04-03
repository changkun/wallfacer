---
title: Ideation Migration to Planning Worker
status: validated
track: local
depends_on: []
affects:
  - internal/runner/ideate.go
  - internal/runner/ideate_test.go
  - internal/handler/ideate.go
  - internal/planner/planner.go
effort: medium
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Ideation Migration to Planning Worker

## Goal

Refactor the ideation agent to run inside the existing planning worker
container via `Planner.Exec()` instead of launching its own ephemeral
container. This unifies the container lifecycle and eliminates per-run
startup overhead (~2-5s).

## What to do

1. Add a `Runner` reference or `Planner` reference so the ideation code
   can call `Planner.Exec()`. The runner already has access to the sandbox
   backend — add `SetPlanner(p *planner.Planner)` to the Runner, or pass
   the planner through the existing dependency chain from `initServer()`.

2. Refactor `Runner.RunIdeation()` in `internal/runner/ideate.go`:
   - Instead of calling `r.backend.Launch(ctx, spec)` with a fresh
     `buildIdeationContainerSpec()`, call `r.planner.Exec(ctx, cmd)` where
     `cmd` is the same `buildAgentCmd(prompt, model)` args
   - The planner's worker container already has the correct workspace
     mounts (RO for all workspaces) — ideation only reads
   - Read `Handle.Stdout()` and `Handle.Stderr()` the same way
   - Call `Handle.Wait()` for exit code
   - Parse output with existing `parseOutput()` and `extractIdeas()`

3. Remove `buildIdeationContainerSpec()` — it's no longer needed since
   the planner container spec handles mounts and labels.

4. Keep the container name tracking in `taskContainers` — use the planner
   container's name (from `Handle.Name()`) so log streaming and kill
   operations still work.

5. Ensure the planner is auto-started if not already running when ideation
   triggers. The `Planner.Exec()` already requires `Start()` to have been
   called — add an auto-start check: if `!p.IsRunning()` then `p.Start(ctx)`
   before exec. This makes the "first caller creates the container" pattern
   work for ideation.

6. Keep the fallback retry logic (Claude → Codex on token limit error)
   working. When retrying with Codex, the planner container uses the Claude
   sandbox — this may need to fall back to ephemeral for Codex retries.
   For now, skip Codex fallback when running through the planner (log a
   warning instead). Codex compatibility is deferred to
   `planning-codex-compat.md`.

7. Update `internal/handler/ideate.go` if it references the container name
   directly — it should use the planner's container handle instead.

## Tests

- `TestRunIdeation_ViaPlanner` — mock planner, verify `Exec()` called with
  correct args including `-p` and `--output-format stream-json`
- `TestRunIdeation_PlannerAutoStart` — planner not started, verify it gets
  auto-started before exec
- `TestRunIdeation_OutputParsing` — verify ideas are still correctly parsed
  from planner exec output (same format as before)
- `TestRunIdeation_CodexFallbackSkipped` — verify Codex fallback is skipped
  with a log warning when running through planner
- Update existing ideation tests to work with the new planner-based flow

## Boundaries

- Do NOT change the ideation prompt or idea parsing logic — only the
  container launch path changes
- Do NOT add Codex support to the planner — that's planning-codex-compat
- Do NOT change the ideation scheduling or auto-trigger logic in
  `internal/handler/ideate.go` — only the container execution path
- Do NOT modify the ideation history management — it stays in the runner
