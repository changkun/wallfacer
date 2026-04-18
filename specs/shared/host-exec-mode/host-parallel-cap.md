---
title: Host mode defaults to MAX_PARALLEL=1
status: complete
depends_on:
  - specs/shared/host-exec-mode/runner-host-switch.md
affects:
  - internal/runner/runner.go
  - internal/runner/runner_test.go
effort: small
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# Host mode defaults to MAX_PARALLEL=1

## Goal

When host mode is active, default `WALLFACER_MAX_PARALLEL=1` so concurrent claude/codex CLIs don't race on `~/.claude/__store.db` and `~/.claude/statsig/`. Users who have verified parallelism can still override explicitly.

## What to do

1. In `internal/runner/runner.go`, locate where `MaxParallel` is resolved from envconfig (grep `MaxParallel` / `WALLFACER_MAX_PARALLEL`). After the envconfig-driven value is computed but before it is applied:

   ```go
   if r.hostMode && cfg.MaxParallelExplicit == false && r.maxParallel > 1 {
       logger.Runner.Info("host mode: capping max parallel tasks to 1 (override with WALLFACER_MAX_PARALLEL=N)",
           "original", r.maxParallel)
       r.maxParallel = 1
   }
   ```

2. In `internal/envconfig/envconfig.go`, expose whether the user explicitly set `WALLFACER_MAX_PARALLEL`:
   - Add `MaxParallelExplicit bool` to the parsed config struct.
   - In `Parse`, set it to `true` iff the key is present in the parsed map (regardless of value). Use the same mechanism as existing "was it set?" checks if one exists; otherwise check the raw key map directly.

3. If there is no pre-existing "was it set" mechanism in envconfig, thread `MaxParallelExplicit` through the same plumbing you add in (2). Keep the change minimal — a single bool on the struct is sufficient.

4. In `Runner.HostMode()` tests (from `runner-host-switch.md`), add a doc comment that the cap applies *post-construction* so test fixtures that don't care about parallelism still see their configured value.

## Tests

In `internal/runner/runner_test.go`:

- `TestRunnerNew_HostMode_DefaultsMaxParallelToOne` — `cfg.SandboxBackend="host"`, `MaxParallelExplicit=false`, `MaxParallel=5`; assert `r.MaxParallel() == 1` (add a getter if none exists, or assert via a behavioral proxy — e.g., promote more than one task and confirm the second is not promoted).
- `TestRunnerNew_HostMode_RespectsExplicitMaxParallel` — same but `MaxParallelExplicit=true, MaxParallel=3`; assert `r.MaxParallel() == 3`.
- `TestRunnerNew_LocalMode_DoesNotCapMaxParallel` — `SandboxBackend=""`, `MaxParallel=5`; assert `r.MaxParallel() == 5`.

In `internal/envconfig/envconfig_test.go`:

- `TestParse_MaxParallelExplicit_True` — `WALLFACER_MAX_PARALLEL=3` in env; assert `cfg.MaxParallelExplicit == true`.
- `TestParse_MaxParallelExplicit_False` — key absent; assert `cfg.MaxParallelExplicit == false`.

## Boundaries

- Do **not** block the user from raising the cap — log and cap silently, don't error.
- Do **not** introduce a separate `WALLFACER_HOST_MAX_PARALLEL` var; reuse the existing `WALLFACER_MAX_PARALLEL`.
- Do **not** change behavior when backend=local; the cap is strictly a host-mode default.
- Do **not** serialize CLI invocations via a wallfacer-side mutex. If a user sets >1 explicitly, trust them — let the CLI's own locking decide.
