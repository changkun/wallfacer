---
title: E2E lifecycle test covers host backend
status: complete
depends_on:
  - specs/shared/host-exec-mode/host-backend.md
  - specs/shared/host-exec-mode/container-spec-host-mode.md
  - specs/shared/host-exec-mode/host-parallel-cap.md
affects:
  - scripts/e2e-lifecycle.sh
  - Makefile
  - CLAUDE.md
effort: small
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# E2E lifecycle test covers host backend

## Goal

Extend the existing `make e2e-lifecycle` harness so a developer can run the full task-lifecycle assertions against the host backend. This is the acceptance gate — if the whole host-mode path works, host mode ships.

## What to do

1. In `scripts/e2e-lifecycle.sh`, add support for a `BACKEND` env var (`container` default, `host` alternate):
   - Before the preflight checks, if `BACKEND=host`, verify `command -v claude && command -v codex` are both available; exit with a helpful message if not.
   - The harness does not start its own server — it connects to one already running. When `BACKEND=host`, the script's preflight banner must remind the operator that the server must have been started with `wallfacer run --backend host` for the checks to be meaningful. A machine-readable check: hit `GET /api/config`, assert the response's `host_mode` field is `true` (this relies on the flag introduced by `ui-host-banner.md`).
   - Swap container-cleanup assertions: the existing `podman ps` check for "wallfacer-*" containers becomes a skip when `BACKEND=host`; add an equivalent assertion that no child processes of wallfacer named `claude` or `codex` remain after archive (use `pgrep -P <wallfacer-pid> -a` with a ~5 s grace).

2. In `Makefile`, extend the `e2e-lifecycle` target to accept `BACKEND=host`:

   ```makefile
   BACKEND ?=
   e2e-lifecycle:
   	BACKEND=$(BACKEND) sh scripts/e2e-lifecycle.sh $(SANDBOX)
   ```

   (If `BACKEND` is already a variable in another target, reuse the same name.)

3. Update the script's header comment block and `CLAUDE.md` (in the E2E section around line 650) with the new invocations:

   ```
   make e2e-lifecycle BACKEND=host SANDBOX=claude
   make e2e-lifecycle BACKEND=host           # both sandboxes, host backend
   ```

4. Keep the existing check count intact for local mode (30 assertions per the current spec). For host mode, the image-presence preflight check is skipped; subtract it from the total and document the new count in the script output.

## Tests

This is itself a test script — no nested tests. Verification steps:

- Run `make build-host`, start `wallfacer run --backend host` (optionally set `WALLFACER_HOST_CLAUDE_BINARY=$(which claude)` / `WALLFACER_HOST_CODEX_BINARY=$(which codex)` if they are not on `$PATH`).
- In another shell: `make e2e-lifecycle BACKEND=host SANDBOX=claude` — assert all checks pass.
- Repeat with `SANDBOX=codex`.
- Run the matrix: `make e2e-lifecycle` (local, default), `make e2e-lifecycle BACKEND=host` — assert both succeed.

Record the assertion counts and durations in the commit message body.

## Boundaries

- Do **not** modify `scripts/e2e-dependency-dag.sh` in this task — it exercises conflict resolution across parallel workers, which host mode's cap-to-1 default fights against. A follow-up spec can decide whether to run the DAG test in host mode with explicit parallelism.
- Do **not** add CI wiring to run host-mode E2E automatically — GitHub Actions runners do not have `claude`/`codex` preinstalled. Document that it's a local-dev convenience.
- Do **not** change the 30-check count for local mode; only adjust for host mode.
