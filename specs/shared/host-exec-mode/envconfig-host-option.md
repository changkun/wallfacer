---
title: envconfig accepts "host" backend and binary overrides
status: validated
depends_on: []
affects:
  - internal/envconfig/envconfig.go
  - internal/envconfig/envconfig_test.go
effort: small
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# envconfig accepts "host" backend and binary overrides

## Goal

Make `envconfig` parse `WALLFACER_SANDBOX_BACKEND=host` without warning, and add two new optional binary-override keys used by `HostBackend`.

## What to do

1. In `internal/envconfig/envconfig.go`:
   - Locate the field for `SandboxBackend` (string value). Confirm it already stores the raw string verbatim.
   - Add two new fields to the parsed config struct:
     - `HostClaudeBinary string` — from `WALLFACER_HOST_CLAUDE_BINARY`.
     - `HostCodexBinary  string` — from `WALLFACER_HOST_CODEX_BINARY`.
   - Update `Parse` to populate both fields (follow the same pattern as other optional string env vars in the file).
   - Update the Windows-specific validation / update paths in the same file to round-trip the two new keys (so `PUT /api/env` preserves them when masking tokens).

2. Add a constant or comment documenting the valid values for `SandboxBackend`: `"local"` (default) or `"host"`. Any other value continues to fall back to `local` with a warning in the runner — this task does not add new enforcement in envconfig.

3. In `internal/envconfig/envconfig_test.go`:
   - Add a case to the existing Parse table test covering `WALLFACER_SANDBOX_BACKEND=host`.
   - Add cases for `WALLFACER_HOST_CLAUDE_BINARY=/usr/local/bin/claude` and `WALLFACER_HOST_CODEX_BINARY=/opt/codex/bin/codex` populating the new fields.
   - Add an Update round-trip test that edits an env file containing both new keys and asserts they are preserved (use the existing Update test pattern).

## Tests

- `TestParse_SandboxBackendHost` — input contains `WALLFACER_SANDBOX_BACKEND=host`; `cfg.SandboxBackend == "host"`.
- `TestParse_HostBinaryOverrides` — both override vars set; both fields populated.
- `TestUpdate_PreservesHostBinaryOverrides` — Update writes unrelated keys; existing `WALLFACER_HOST_*` values survive.

## Boundaries

- Do **not** validate the binary exists here; that is `HostBackend`'s job at construction time.
- Do **not** add runner-side wiring — a separate task (`runner-host-switch.md`) reads these fields.
- Do **not** rename or deprecate existing `SandboxBackend` values.
