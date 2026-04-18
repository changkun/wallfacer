---
title: Document `make build-binary` as the host-mode build
status: validated
depends_on: []
affects:
  - Makefile
  - docs/guide/configuration.md
  - CLAUDE.md
  - AGENTS.md
effort: small
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# Document `make build-binary` as the host-mode build

## Goal

Since backend selection is now a runtime CLI flag (`wallfacer run --backend host`) and no longer an env var, the Makefile cannot predict whether the user will run container or host mode — so it should not branch on a phantom env var. Instead, make it discoverable that `make build-binary` is the right target for host-mode users (skips `pull-images` and all container machinery). Add a short Makefile comment and a docs pointer.

## What to do

1. In `Makefile`, update the comment block above `build` (around line 32) to explicitly name the host-mode path:

   ```makefile
   # Build the wallfacer binary and pull sandbox images (container mode).
   # For host mode (wallfacer run --backend host) use `make build-binary`
   # instead — it skips image pull and still runs fmt + lint + ts build.
   ```

   Do **not** change the target body. `build` keeps `pull-images` unconditionally. The only behavioral change is that users who explicitly choose host mode run `make build-binary`.

2. If `build-binary` currently skips lint / fmt / ts-build (per line 45, it does — it only compiles), introduce a new convenience target:

   ```makefile
   # Build for host mode: full fmt + lint + ts build + binary, no image pull.
   .PHONY: build-host
   build-host: fmt lint ui-ts build-binary
   ```

   and list it in `.PHONY`. This keeps parity with `build`'s validation pipeline for host-mode users without forcing them into a pull.

3. Update `docs/guide/configuration.md` (Sandbox section):
   - Remove any references to `WALLFACER_SANDBOX_BACKEND` (there should be one row in the env-var table — delete it).
   - Add a new "Host mode" subsection noting:
     - Activate via `wallfacer run --backend host`.
     - Requires host-installed `claude` / `codex`.
     - Build with `make build-host` (or `make build-binary` for a minimal build).
     - No filesystem isolation — see *Write containment* warning.

4. In `CLAUDE.md` and `AGENTS.md`:
   - Remove the `WALLFACER_SANDBOX_BACKEND` row from the env-var table.
   - Add `build-host` to the `make` target list.
   - Add `--backend` flag to the `wallfacer run` usage examples.

## Tests

No automated test — Makefile logic is verified manually. For the commit, verify both paths by hand:

- `make -n build` — output includes `pull-images`.
- `make -n build-host` — output does **not** include `pull-images` but does include `fmt`, `lint`, `ui-ts`, `build-binary`.

Record the two `-n` outputs in the commit message body as evidence.

## Boundaries

- Do **not** gate `pull-images` on a shell env var. Backend selection is a runtime CLI concern, not a build-time concern.
- Do **not** remove the existing retag-from-latest fallback inside `pull-images`.
- Do **not** add CI wiring to run `build-host` — CI continues to run `make build` with containers.
