---
title: Org switching for users belonging to multiple orgs
status: validated
depends_on:
  - specs/shared/authentication/data-model-principal-org.md
  - specs/shared/authentication/jwt-middleware.md
affects:
  - internal/handler/
  - internal/auth/
  - ui/js/
  - ui/partials/status-bar.html
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Org switching for users belonging to multiple orgs

## Goal

When a signed-in user belongs to more than one org, the status-bar dropdown
lists their orgs; selecting one refreshes the session cookie with a token
scoped to the chosen org. Subsequent data reads (tasks, workspaces) are
filtered by the new `org_id`. Users with a single org see no chooser.

## What to do

1. **Org list endpoint**, `GET /api/auth/orgs`:
   - Calls `auth.latere.ai/userinfo` (or the dedicated org-list endpoint
     if the platform exposes one, check `~/dev/latere.ai/pkg/oidc/`
     before adding a new HTTP call).
   - Returns JSON `{orgs: [{id, name, current}], current_id}`.
   - 204 when the user has only one org.
   - 401 when unauthenticated.
2. **Switch endpoint**, `POST /api/auth/switch-org` with body `{org_id}`:
   - Uses the session's refresh token to call
     `auth.latere.ai/token` with `grant_type=refresh_token&org_id=<target>`.
     The auth service mints a new access token scoped to the requested org;
     refresh the session cookie to carry it.
   - Validates the user actually belongs to the target org (the auth
     service enforces this, but surface a 403 with a clear error body on
     rejection rather than propagating the raw OAuth error).
   - Returns 200 with the updated `{org_id, org_name}`; the frontend
     reloads to pick up org-scoped data.
3. **Frontend**, extend the status-bar badge (from Phase 1):
   - When `/api/auth/orgs` returns 200 with multiple orgs, the signed-in
     dropdown grows an "Organizations" submenu listing each org with a
     check mark next to the current one.
   - Selecting an org POSTs `/api/auth/switch-org`; on success, reload
     the page.
   - When 204, render the dropdown as today (Sign out only).
4. **Cookie refresh**, `pkg/oidc` exposes a session-mutation helper;
   reuse it rather than writing cookies directly. Preserve `HttpOnly`,
   `Secure`, `SameSite=Lax`, `__Host-` prefix.

## Tests

- `internal/handler/orgs_test.go`:
  - multi-org user → `/api/auth/orgs` 200 with list.
  - single-org user → 204.
  - unauthenticated → 401.
- `internal/handler/switch_org_test.go`:
  - valid switch → 200, cookie `Set-Cookie` header carries a new session.
  - switching to an org the user does not belong to → 403 from the
    platform's refresh call, surfaced as 403 to the client.
  - unauthenticated → 401.
- `ui/js/tests/status-bar.test.js`:
  - multi-org response renders a submenu with correct current-marker.
  - single-org response omits the submenu.
  - clicking an org fires a POST and, on success, triggers `location.reload`.

## Boundaries

- Do not introduce an in-app org admin UI (invite users, change names).
  The auth service owns that.
- Do not migrate existing tasks on org switch. Switching changes the
  *lens*; data written under org A stays under org A.
- Do not cache the org list beyond the request. The platform's session TTL
  is the right boundary, if the membership list changes, the user sees
  it on next login.
- Do not add deep-linked org switching (`/switch-org?id=...`). Only POST.
