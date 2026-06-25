---
title: Documentation
status: archived
depends_on:
  - specs/local/terminal-sessions/task-05-frontend-session-wiring.md
affects: []
effort: small
created: 2026-03-28
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 6: Documentation

## Goal

Update user-facing and internal documentation to cover multi-session terminal tabs.

## What to do

1. Update `docs/guide/configuration.md` if any new env vars are added (unlikely but check).

2. Update the terminal section in the appropriate user guide (likely `docs/guide/usage.md` or whichever guide covers the terminal) to describe:
   - How to open additional terminal sessions (click "+").
   - How to switch between sessions (click tab).
   - How to close a session (click × on tab).
   - Behavior when the last session is closed (terminal disconnects).

3. Update `docs/internals/internals.md` or the relevant internals doc with:
   - The session registry architecture.
   - New WebSocket message types (`create_session`, `switch_session`, `close_session`, `session_created`, `session_switched`, `session_closed`, `session_exited`, `sessions`).

4. Update `CLAUDE.md` if the terminal section needs revision (e.g., if the WebSocket protocol description changes).

## Tests

- Run `make test` to verify nothing is broken.
- Manually verify doc links are valid.

## Boundaries

- Do NOT change code in this task — documentation only.
- Do NOT document container exec features (separate spec).
