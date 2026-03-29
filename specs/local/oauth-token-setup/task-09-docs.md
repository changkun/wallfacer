---
title: Documentation
status: complete
track: local
depends_on:
  - specs/local/oauth-token-setup/task-04-auth-handler-routes.md
  - specs/local/oauth-token-setup/task-05-ui-sign-in.md
  - specs/local/oauth-token-setup/task-07-reauth-flag.md
  - specs/local/oauth-token-setup/task-08-first-launch-reauth-ui.md
affects: []
effort: small
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 9: Documentation

## Goal

Document the OAuth sign-in feature in the user guide, update API docs, and update CLAUDE.md with the new routes and env behavior.

## What to do

1. Update `docs/guide/configuration.md`:
   - Add a section on OAuth sign-in (e.g., under "API Configuration" or as a new subsection).
   - Explain: click "Sign in" in Settings, browser opens, authenticate, token stored automatically.
   - Mention that manual token paste remains as a fallback.
   - Note that sign-in buttons are hidden when custom base URLs are set.
   - Document first-launch behavior (hints and toast).
   - Document re-auth flow (test detects invalid token, "Sign in again" button).

2. Update `docs/internals/api-and-transport.md`:
   - Add the three new auth routes: `POST /api/auth/{provider}/start`, `GET /api/auth/{provider}/status`, `POST /api/auth/{provider}/cancel`.
   - Document request/response shapes.

3. Update `CLAUDE.md`:
   - Add the new auth routes to the API Routes section.
   - Mention `internal/oauth/` in the key server files list.
   - Mention `internal/handler/auth.go` in the handler files list.

4. Update `docs/guide/getting-started.md` if it references manual token setup — add a note that OAuth sign-in is now available as an alternative.

## Tests

- No code tests. Verify docs render correctly (no broken links or formatting).

## Boundaries

- Do NOT make any code changes in this task.
- Only document what was actually implemented in tasks 1-8.
