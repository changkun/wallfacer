---
title: "Documentation"
status: complete
depends_on:
  - specs/foundations/host-terminal/task-04-backend-terminal-handler.md
  - specs/foundations/host-terminal/task-06-statusbar-integration.md
affects:
  - docs/guide/configuration.md
effort: small
created: 2026-03-22
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 7: Documentation

## Goal

Document the new terminal feature in the configuration guide, CLAUDE.md, and internals docs.

## What to do

1. **`docs/guide/configuration.md`** — Add `WALLFACER_TERMINAL_ENABLED` to the environment variables table. Description: "Enable the integrated host terminal panel (`true`/`false`, default: `false`). When enabled, the Terminal button in the status bar opens an interactive shell running on the host machine." Place it in the appropriate section (likely near other feature toggles like `WALLFACER_AUTO_PUSH`).

2. **`CLAUDE.md`** — Under the "Configuration" section's optional variables list, add:
   ```
   - `WALLFACER_TERMINAL_ENABLED` — enable integrated host terminal (`true`/`false`, default `false`)
   ```
   Under the API Routes section, add a Terminal subsection:
   ```
   ### Terminal
   - `GET /api/terminal/ws` — WebSocket: interactive host shell (not in apicontract; requires `?token=` auth)
   ```

3. **`docs/internals/api-and-transport.md`** (if it exists) — Add a note about the WebSocket endpoint being the project's first, explaining why it's registered directly in `BuildMux` instead of via `apicontract/routes.go`.

## Tests

- No automated tests for docs.
- Verify `make build` still succeeds (docs may be embedded).

## Boundaries

- Do NOT write a new guide page for the terminal — it's a small feature toggle documented in the configuration guide
- Do NOT add user guide content about how to use the terminal — it's self-explanatory (open panel, type commands)
- Keep additions minimal and factual
