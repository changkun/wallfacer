---
title: WALLFACER_PLANNING_WINDOW_DAYS Config Knob
status: complete
depends_on: []
affects:
  - internal/envconfig/envconfig.go
  - internal/handler/config.go
  - docs/guide/configuration.md
  - CLAUDE.md
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# WALLFACER_PLANNING_WINDOW_DAYS Config Knob

## Goal

Expose a configurable default window (in days) for the planning-cost
analytics display. The server reads it from `.env`, exposes it via
`/api/config`, and the UI uses it as the default for the period picker
when it opens.

## What to do

1. In `internal/envconfig/envconfig.go`:
   - Add `PlanningWindowDays int` to the `Config` struct (around
     L17–L58).
   - Add `"WALLFACER_PLANNING_WINDOW_DAYS"` to `knownKeys`
     (L64–L98).
   - Parse the value as an int with a default of `30` (matches the
     existing 30-day option on the usage-stats period picker). A value
     of `0` means "all time".
2. In `internal/handler/config.go::buildConfigResponse` (around
   L135–L276), surface the value to the UI as
   `planning_window_days` in the JSON response. Follow the same
   pattern as other numeric env knobs already exposed there.
3. Document the variable in `docs/guide/configuration.md` (env var
   reference table) and in `CLAUDE.md` (configuration section). Keep
   the descriptions short and consistent with neighbouring entries.

## Tests

- Extend `internal/envconfig/envconfig_test.go`:
  - `TestParse_PlanningWindowDaysDefault` — unset env → `30`.
  - `TestParse_PlanningWindowDays` — explicit value → parsed int.
  - `TestParse_PlanningWindowDaysInvalid` — non-numeric → parse error
    or documented fallback (match sibling int knobs' behavior).
- Extend `internal/handler/config_test.go`:
  - `TestConfigResponse_IncludesPlanningWindowDays` — assert the value
    appears in the `/api/config` JSON.

## Boundaries

- Do not read this value inside `/api/stats` or `/api/usage` — those
  reuse the `?days=` query param and are the UI's responsibility to
  pass.
- Do not change any existing env var names or defaults.
- Do not build UI elements in this task; the modal-stats and
  usage-stats tasks consume this value.
