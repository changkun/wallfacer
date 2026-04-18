---
title: Makefile skips pull-images when host backend is selected
status: validated
depends_on: []
affects:
  - Makefile
  - docs/guide/configuration.md
effort: small
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# Makefile skips pull-images when host backend is selected

## Goal

Let users opt out of image pulling at build time by setting `WALLFACER_SANDBOX_BACKEND=host` in their `.env`. `make build` should skip the `pull-images` dependency in that case, so host-mode users never touch GHCR.

## What to do

1. In `Makefile`, replace the single `build: fmt lint ui-ts build-binary pull-images` line with a conditional:

   ```makefile
   ifeq ($(WALLFACER_SANDBOX_BACKEND),host)
   build: fmt lint ui-ts build-binary
   else
   build: fmt lint ui-ts build-binary pull-images
   endif
   ```

   The existing `-include .env` + `export` at lines 27–28 already makes `WALLFACER_SANDBOX_BACKEND` available to Make, so the `ifeq` resolves correctly when the user has set it in `~/.wallfacer/.env` or shell env.

2. Leave `pull-images` and `pull-images-force` targets unchanged — users can still invoke them manually when they flip back to container mode.

3. Add a short note in the `pull-images` target comment block explaining that host mode bypasses this target entirely.

4. Update `docs/guide/configuration.md` in the Sandbox section: add a one-line mention under the `WALLFACER_SANDBOX_BACKEND` row that `host` causes `make build` to skip image pull.

## Tests

No automated test — Makefile logic is verified manually. For the commit, verify both paths by hand:

- `WALLFACER_SANDBOX_BACKEND=host make -n build` — output should **not** include `pull-images`.
- `unset WALLFACER_SANDBOX_BACKEND; make -n build` — output should include `pull-images`.

Record the two `-n` outputs in the commit message body as evidence.

## Boundaries

- Do **not** change the CI workflow files — CI continues to build with container mode.
- Do **not** touch `pull-images` / `pull-images-force` internals; the existing retag-from-latest fallback is orthogonal.
- Do **not** add a Makefile target that invokes wallfacer with backend=host — that remains a runtime choice.
