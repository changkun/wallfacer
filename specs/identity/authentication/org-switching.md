---
title: Org switching for users belonging to multiple orgs
status: archived
depends_on:
  - specs/identity/authentication/data-model-principal-org.md
  - specs/identity/authentication/jwt-middleware.md
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

## Outcome

Delivered end-to-end across three repos. Users with multiple org
memberships see an inline `<select>` in their sign-in badge;
selecting one refreshes the session with a new token scoped to the
chosen org. The choice survives re-authorizations because the auth
service persists it on the SSO session row.

### What shipped (three-repo change set)

**`latere.ai/auth`** (migration + two handler helpers):
- Migration `000013_sso_active_org.{up,down}.sql`: adds
  `active_org_id UUID REFERENCES orgs(id) ON DELETE SET NULL` to
  `sso_sessions`.
- `activeOrgForSession(ctx, ssoSessionID, principalID)` and
  `setActiveOrgForSession(ctx, ssoSessionID, orgID)` helpers in
  `internal/handler/handler.go`. Membership is re-verified on read
  so a stale selection cannot leak across an org kick.
- `buildSession` takes `ssoSessionID` and reads the session's active
  org instead of always falling back to first-joined.
- `handleAuthorize` accepts an optional `?org_id=<uuid>` query param.
  When the user is a verified member, the choice is persisted on
  the session; non-members are silently ignored.

**`latere.ai/x/pkg/oidc`** (tagged `v0.10.0`):
- `HandleLogin` forwards an allowlisted set of query params (just
  `org_id` today) into the authorize redirect. Narrow allowlist, not
  a blanket pass-through.
- New `Client.AuthCodeURLWithOpts(state, verifier, extra)` exposes
  the `oauth2.SetAuthURLParam` plumbing for callers that need to
  pass extension hints outside `HandleLogin`.

**`wallfacer`**:
- `GET /api/auth/orgs` (`internal/handler/orgs.go`): proxies to the
  auth service's `/me/orgs` using the session's access token;
  returns 200 `{orgs, current_id}` for 2+ orgs, 204 otherwise.
- `POST /api/auth/switch-org`: validates membership via the same
  proxy, clears the wallfacer session cookie, returns
  `{redirect_url: "/login?org_id=<target>"}`. The frontend follows;
  `oidc.HandleLogin` forwards `org_id`; the auth service persists
  the choice and issues a fresh token.
- `Handler.HasAuth()` reused as the cloud-mode signal (same pattern
  as scope-and-superadmin + cloud-forced-login).
- Status-bar badge extended: `.sb-signin__orgs` slot, an inline
  `<select>` with one option per org when 2+, change-handler POSTs
  /api/auth/switch-org and navigates to the returned redirect.
- `go.mod` bumped to `latere.ai/x/pkg v0.10.0`.
- 8 Go tests + 4 vitest cases covering the full decision matrix.

### Implementation notes

1. **Opted for redirect-through-login over a dedicated token swap.**
   The spec sketched `POST /api/auth/switch-org` as a backend call
   that refreshes the token directly via
   `grant_type=refresh_token&org_id=<target>`. Fosite (the auth
   service's OAuth library) does not expose a refresh-with-org_id
   grant, so extending that path would require a custom handler on
   the auth service. The chosen flow reuses the standard
   authorize-code path: wallfacer clears its cookie, browser
   follows to `/login?org_id=<target>`, `pkg/oidc` forwards, the
   auth service honors the hint and persists it. Zero new
   auth-service routes, single round-trip from the user's point of
   view. The spec's original sketch is captured here as the fallback
   design if this ever needs to happen purely server-side (no
   browser round-trip).

2. **The switch endpoint returns a `redirect_url` string, not a 302.**
   The frontend posts via `fetch`, which follows 302s transparently
   — the browser doesn't actually *navigate*, so the user stays on
   the current page and the session cookie never updates. Returning
   the URL in a JSON body and calling `window.location.href = ...`
   in the handler makes the navigation explicit.

3. **Pre-flight membership check is duplicated** between wallfacer
   (via `/me/orgs`) and the auth service (in `handleAuthorize`).
   This is intentional: without the wallfacer check, switching to a
   non-member org would silently no-op (auth service ignores the
   param) and the user would land on /callback still scoped to the
   old org with no error. The wallfacer check gives a clean 403.
   The auth service re-check is the source of truth.

4. **`active_org_id` persistence fixes the "choice forgotten on
   next token" problem.** Without the column, the user would need
   to pass `org_id=` on every re-authorize; with it, once selected
   the choice sticks until they switch again or the SSO session
   expires.

5. **Single-org and local-mode both return 204, not 200+empty.**
   Lets the frontend skip rendering the switcher with one status
   check instead of parsing a response body.

6. **Frontend uses a plain `<select>`, not a custom dropdown.**
   Accessibility + keyboard navigation come for free. Styling can
   upgrade later without changing the behavior.

7. **v0.10.0 pkg bump required an `audience` validator relaxation
   as a side-effect.** While testing the end-to-end flow we found a
   pre-existing config mismatch between fosite's access-token `aud`
   and wallfacer's expected audience that produced a redirect loop.
   The fix landed in a separate commit on `internal/auth/` — see
   the commit message for details. Not in scope for this spec but
   blocking for the deployment verification step.
