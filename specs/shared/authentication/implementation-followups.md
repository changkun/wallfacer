---
title: Phase 2 implementation follow-ups
status: complete
depends_on:
  - specs/shared/authentication/jwt-middleware.md
  - specs/shared/authentication/principal-context.md
  - specs/shared/authentication/data-model-principal-org.md
  - specs/shared/authentication/cloud-forced-login.md
  - specs/shared/authentication/scope-and-superadmin.md
  - specs/shared/authentication/org-switching.md
  - specs/shared/authentication/task-event-actor-sub.md
affects:
  - internal/auth/
  - internal/handler/
  - internal/store/
  - internal/workspace/
  - ui/js/status-bar.js
  - ui/css/header.css
  - scripts/e2e-auth-flow.sh
  - scripts/e2e-switch-org.sh
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Phase 2 Implementation Follow-ups

Retrospective record of the bug fixes, design corrections, and cross-repo
releases that landed after every Phase 2 child spec was already marked
`complete`. The parent [`authentication.md`](../authentication.md) lists
the shape of what shipped; this document keeps the sequence of *changes
to what shipped* so the history is auditable.

Scope is strictly Phase 2 post-merge fixes. Phase 3 work (third-party
OIDC, remote control) is unaffected.

## Summary table

| # | Area | Change | Trigger |
|---|------|--------|---------|
| 1 | `internal/auth/middleware.go` | Drop audience check in `BuildValidator` | fosite access tokens carry no `aud` |
| 2 | `internal/auth/cookie_principal.go` | Stop clearing session on validation failure | `/callback → / → /login → /callback` redirect loop |
| 3 | auth service (`v0.5.4`→`v0.5.5`) | Populate JWT `iss` on access tokens | Middleware rejected auth service's own tokens |
| 4 | auth service migration `000013` | Fix FK target from `orgs(id)` → `organizations(id)`, add `active_org_id` | Dirty migration blocked beta deploy |
| 5 | auth service (`v0.5.6`) | Cast `joined_at::text` on `/me/orgs` | pgx cannot scan `TIMESTAMPTZ` into `*string` |
| 6 | auth service (`v0.5.7`) + wallfacer | Return `name` + `picture` from `/userinfo` + consume them in badge | Badge rendered empty name/avatar |
| 7 | `pkg/oidc v0.10.1` | `UserFromRequest` now fetches `/userinfo` after decode | JWT carries no `name`/`picture` |
| 8 | `pkg/oidc v0.10.2` + auth service | Forward empty `org_id` through `AuthCodeURLWithOpts` + `HandleLogin` | "Switch to personal" was a silent no-op |
| 9 | `internal/store/principal.go` + `internal/workspace/groups.go` | Final three-shape filter matrix (legacy / personal / org) | Tasks disappeared after signing in; later, personal data leaked into org view |
| 10 | `ui/js/status-bar.js`, `ui/css/header.css` | Two-line badge trigger; `position: fixed` menu anchored via `getBoundingClientRect` | Name truncation ("Changk..."); menu clipped by collapsed sidebar's `overflow: hidden` |
| 11 | `scripts/e2e-auth-flow.sh`, `scripts/e2e-switch-org.sh` | Email-OTP walkthrough + personal↔org switch verification | Reproducible regression coverage |
| 12 | `internal/workspace/groups.go` + `internal/handler/config.go` | Per-group automation toggle persistence | Autopilot leaked across workspace groups after the `OrgID` filter landed |

## Details

### 1. JWT audience not required

`jwtauth.Validator` initially required `aud == client_id`. fosite does
not set the audience claim on its access tokens, so every authenticated
request was rejected with `invalid audience`. `BuildValidator` now skips
audience verification and relies on `iss` + signature. Audience checks
remain available but are not wired for wallfacer's client.

### 2. CookiePrincipal no longer clears session on validation failure

The spec implied the cookie path would clear a bad session. In
production this compounded with bug #3 to loop the browser through
`/callback → / → /login → /callback` — every redirect landed on a
validation failure, the session was cleared, the force-login middleware
then bounced back to `/login`. `CookiePrincipal` now attaches no
principal on failure and leaves the cookie intact; explicit sign-out is
the only path that clears it.

### 3. JWT `iss` populated by auth service

fosite's default `JWTClaims` did not set `Issuer`. The auth service's
own `jwtMiddleware` rejected its own tokens with "wrong issuer". Fixed
in `buildSession` + `enrichClientCredentials` by setting
`sess.JWTClaims.Issuer = h.cfg.BaseURL`. Tagged `v0.5.5`.

### 4. `sso_sessions.active_org_id` migration

Migration `000013_sso_active_org.up.sql` initially referenced `orgs(id)`
in its foreign-key clause; the actual table is `organizations`. The
failed migration left `schema_migrations.dirty=true`, blocking startup.
Recovery: `UPDATE schema_migrations SET dirty=false` via `kubectl psql`,
then retag. The corrected FK is `REFERENCES organizations(id) ON DELETE
SET NULL`. `NULL` = personal view; UUID = org-scoped view.

### 5. `/me/orgs` TIMESTAMPTZ cast

The `joined_at` column is `TIMESTAMPTZ`. The handler scanned it into
`*string`, which pgx rejects. Fixed by casting at the SQL level:
`SELECT ... joined_at::text ...`. Tagged `v0.5.6`.

### 6. `/userinfo` returns `name` + `picture`

The sign-in badge needed display name + avatar, which are database
fields, not JWT claims. `/userinfo` now selects them from the users
table and serializes them in the JSON response. Tagged `v0.5.7` on the
auth service. Without this, the badge rendered only an email string.

### 7. `UserFromRequest` fetches `/userinfo`

`pkg/oidc.UserFromRequest` previously decoded only the JWT's `sub` and
`email`. After fix #6 the auth service had the data, but wallfacer
still wasn't asking for it. `UserFromRequest` now calls `FetchUserInfo`
after token validation and merges the response into the returned user
struct. Tagged `v0.10.1`.

### 8. Empty `org_id` forwarding

Switching back to personal view required the caller to send
`?org_id=` with an empty string — the single-bit "clear active org"
signal to the auth service. Both `AuthCodeURLWithOpts` and
`HandleLogin` dropped empty values, so the signal never left the
client. Fix: check presence, not value, when forwarding the parameter.
Tagged `pkg/oidc v0.10.2`.

### 9. Three-shape filter matrix

The original spec modelled records as scoped (`OrgID != ""`) or
unscoped (`OrgID == ""`). Production had a third shape that behaved
differently: `OrgID == ""` authored by a known user (personal) vs.
`OrgID == "" && CreatedBy == ""` (legacy). Final matrix:

```go
func principalSeesTask(p Principal, t Task) bool {
    if p.OrgID != "" {
        return t.OrgID == p.OrgID        // org view: strict
    }
    if t.OrgID != "" {
        return false                     // personal view hides all orgs
    }
    return t.CreatedBy == "" || t.CreatedBy == p.Sub
}
```

The same shape is applied to `GroupsForPrincipal` in
`internal/workspace/groups.go`. Local mode (`claims == nil`) bypasses
the filter entirely and returns everything.

### 10. Badge redesign

Two regressions against the Phase 2 badge:

- **Name truncation.** Display names like "Changkun Ou" were truncated
  to "Changk..." because an inline org pill competed for horizontal
  space with the name. The trigger is now a two-line stack: name on
  top, scope subtitle (`"Personal"` or org name) below.
- **Collapsed-sidebar clipping.** The popup menu used
  `position: absolute`, which the sidebar's `overflow: hidden`
  clipped when the sidebar was narrow. The menu now uses
  `position: fixed` and is anchored via
  `getBoundingClientRect()` on click; a `resize` listener repositions
  it if the layout changes while open. This escapes the clip in both
  the expanded and collapsed sidebar layouts.

Tracked in commits `e6ff070c` (CSS) and `1cc4a4e2` (JS).

### 11. E2E scripts

Two reproducible coverage scripts now sit in `scripts/`:

- `e2e-auth-flow.sh` — walks the full email-OTP sign-in flow via curl,
  asserts the session cookie and `/api/auth/me` response.
- `e2e-switch-org.sh` — signs in, lists workspace groups, switches
  personal ↔ org, and asserts the workspace-group count differs
  between views.

Script bugs caught during write-up: BSD awk doesn't honor
`IGNORECASE=1`; use `tolower($1) == "location:"` instead. Double-POST
to `/login/email` invalidates the OTP — capture body + status in a
single curl.

### 12. Per-group automation toggles

After the `OrgID` filter landed, users reported that enabling Autopilot
in workspace group A carried into group B after a switch. The
automation toggles (autopilot, autorefine, autotest, autosubmit,
autosync) were global `atomic.Bool` values. Fix: store each toggle as
an optional pointer on `workspace.Group`, apply on snapshot
(`applyGroupToggles`), persist on change (`persistCurrentGroupToggles`).
Autopush stays global because push credentials are server-level.
Landed as commit `f2085fae`.

## Release tag inventory

Final tags that make up the shipped Phase 2 surface:

| Repo | Tag | Landed |
|------|-----|--------|
| `latere.ai/auth` | `v0.5.8` | Migration 13 + `active_org_id`, JWT `iss`, `/me/orgs` cast, `/userinfo` name/picture, `/authorize` `org_id` forward |
| `latere.ai/x/pkg` | `v0.10.2` | `AuthCodeURLWithOpts` + `HandleLogin` empty-value forward, `UserFromRequest` `/userinfo` fetch |
| `changkun.de/x/wallfacer` | `main` (`3537cd83`) | Middleware chain, principal filter matrix, badge, e2e scripts, per-group toggles |

## What did *not* change

- The spec lifecycle for the seven Phase 2 child specs. They stayed
  `complete` throughout; this document is a supplement, not a reopen.
- Local-mode behavior. Every fix above is gated on cloud mode or on a
  code path that only runs under cloud mode.
- The JWT claim shape or cookie format. No on-disk migrations for
  existing task / workspace records (both new fields are `omitempty`).

## Outcome

Phase 2 is stable in production as of 2026-04-19. The parent
[`authentication.md`](../authentication.md) captures the end-state
design; this document captures the path taken to get there.
