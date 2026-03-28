# Task 2: Add TerminalEnabled to Envconfig and Config Response

**Status:** Todo
**Depends on:** None
**Phase:** Phase 1 — Single Terminal Session
**Effort:** Small

## Goal

Add the `WALLFACER_TERMINAL_ENABLED` opt-in flag so the backend can gate terminal access and the frontend can hide/show the Terminal button.

## What to do

1. **`internal/envconfig/envconfig.go`** — Add `TerminalEnabled bool` field to the `Config` struct. In the `Parse` function's switch statement, add:
   ```go
   case "WALLFACER_TERMINAL_ENABLED":
       cfg.TerminalEnabled = v == "true"
   ```
   This defaults to `false` (opt-in), matching the pattern used by `AutoPushEnabled` and `DependencyCaches`.

2. **`internal/handler/config.go`** — In `buildConfigResponse()`, add `"terminalEnabled": cfg.TerminalEnabled` to the response map (alongside existing flags like `"autopilot"`, `"autotest"`, etc.). When `cfg == nil`, default to `false`.

3. **`internal/handler/env.go`** — In the env update handler (`UpdateEnv`), ensure `WALLFACER_TERMINAL_ENABLED` is included in the passthrough fields so the Settings UI can toggle it. Follow the pattern used by `WALLFACER_AUTO_PUSH`.

4. **`internal/envconfig/envconfig_test.go`** — Add a test case that writes `WALLFACER_TERMINAL_ENABLED=true` to a temp `.env` file, calls `Parse`, and asserts `cfg.TerminalEnabled == true`. Add a second case with the field absent to confirm the default is `false`.

5. **`internal/handler/config_test.go`** — Add a test that calls `GetConfig` and verifies the response includes `"terminalEnabled"` as a boolean.

## Tests

- `TestParseTerminalEnabled` — parses `.env` with `WALLFACER_TERMINAL_ENABLED=true`, asserts `cfg.TerminalEnabled == true`
- `TestParseTerminalEnabledDefault` — parses empty `.env`, asserts `cfg.TerminalEnabled == false`
- `TestGetConfigIncludesTerminalEnabled` — HTTP GET `/api/config`, decode JSON, assert `terminalEnabled` key exists and is `false`

## Boundaries

- Do NOT add the terminal WebSocket handler yet
- Do NOT modify the frontend settings UI (the env update passthrough is enough for the existing generic settings form)
- Do NOT add documentation yet (Task 7)
