---
title: envconfig exposes host-binary path overrides
status: archived
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


# envconfig exposes host-binary path overrides

## Goal

Add two optional path-override env vars used by `HostBackend` when resolving the `claude` / `codex` CLIs. Backend selection itself is a CLI flag on `wallfacer run` (see `runner-host-switch.md`) — not an env var — so this task is strictly about binary lookup overrides, not mode selection.

## What to do

1. In `internal/envconfig/envconfig.go`:
   - Add two new fields to the parsed config struct:
     - `HostClaudeBinary string` — from `WALLFACER_HOST_CLAUDE_BINARY`.
     - `HostCodexBinary  string` — from `WALLFACER_HOST_CODEX_BINARY`.
   - Update `Parse` to populate both fields (follow the pattern of other optional string env vars).
   - Update the Windows-specific validation / update paths in the same file to round-trip the two new keys so `PUT /api/env` preserves them when masking tokens.

2. Do **not** add or modify any field related to `WALLFACER_SANDBOX_BACKEND`. Backend selection is a CLI concern; `envconfig` does not read it in this task.

3. In `internal/envconfig/envconfig_test.go`:
   - Add cases for `WALLFACER_HOST_CLAUDE_BINARY=/usr/local/bin/claude` and `WALLFACER_HOST_CODEX_BINARY=/opt/codex/bin/codex` populating the new fields.
   - Add an Update round-trip test that edits an env file containing both new keys and asserts they are preserved (use the existing Update test pattern).

## Tests

- `TestParse_HostBinaryOverrides` — both override vars set; both fields populated.
- `TestParse_HostBinaryOverrides_Empty` — neither set; fields are the zero value.
- `TestUpdate_PreservesHostBinaryOverrides` — Update writes unrelated keys; existing `WALLFACER_HOST_*` values survive.

## Boundaries

- Do **not** validate the binary exists here; that is `HostBackend`'s job at construction time.
- Do **not** add, change, or reference `WALLFACER_SANDBOX_BACKEND` — it is no longer a user-facing env var.
- Do **not** add runner-side wiring — a separate task (`runner-host-switch.md`) reads these fields.
