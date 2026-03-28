# Task 8: Configuration and Documentation

**Status:** Todo
**Depends on:** Task 3
**Phase:** 3 (Robustness)
**Effort:** Small

## Goal

Add the `WALLFACER_TASK_WORKERS` environment variable to the
configuration system, settings UI, and documentation.

## What to do

1. In `internal/envconfig/`, add `TaskWorkers` field (bool, default true).

2. Wire it through to `LocalBackend` initialization in `server.go` or
   wherever the backend is created.

3. Update `docs/guide/configuration.md` with the new env var.

4. Update `CLAUDE.md` if container execution is mentioned.

5. Update `docs/internals/orchestration.md` or relevant internals doc
   to describe the per-task worker architecture.

## Tests

- `TestEnvConfigTaskWorkersDefault` — verify default is true.
- `TestEnvConfigTaskWorkersDisabled` — set to false, verify parsed.

## Boundaries

- Do NOT add the env var to the settings UI modal (it's an advanced
  tuning knob, not a user-facing toggle).
