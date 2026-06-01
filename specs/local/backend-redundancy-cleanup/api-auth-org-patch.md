---
title: Move switch-org behind PATCH /api/auth/me
status: drafted
depends_on:
  - specs/local/backend-redundancy-cleanup.md
  - specs/local/vue-frontend-migration.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/orgs.go
  - internal/cli/server.go
  - ui/js/generated/routes.js
  - frontend/src/
  - ui/js/
effort: small
created: 2026-06-01
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Move switch-org behind PATCH /api/auth/me

Today:

- `GET /api/auth/me`
- `GET /api/auth/orgs`
- `POST /api/auth/switch-org`

`switch-org` is a mutation of the "me" resource (it picks the active
org for the signed-in principal). Folding it into `PATCH /api/auth/me`
brings the surface in line with REST conventions and saves one route.

## Target shape

```
PATCH /api/auth/me
Body: {"org_id": "<uuid>"} or {"org_id": ""} for personal
Response: { "redirect_url": "/login?org_id=<uuid>" }
```

The redirect-url response stays so the frontend follows the same
re-login flow.

## Backend changes

1. Remove `POST /api/auth/switch-org` from
   `internal/apicontract/routes.go`. Add `PATCH /api/auth/me`.
2. Rename `AuthSwitchOrg` handler to `PatchAuthMe`. Body decode stays
   the same; consider switching from `json.NewDecoder` to
   `httpjson.DecodeBody` (small win — could also be done in the
   sibling `handler-helpers-dedup.md` spec; do whichever lands first).
3. Wiring update in `internal/cli/server.go`.

## Frontend changes

Search for `/api/auth/switch-org` in both `ui/js/` and `frontend/src/`
and migrate to the new PATCH shape.

## Acceptance

- One route removed; one added.
- Existing `OrgSwitcher` UI still works (same body, same response,
  different method/URL).

## Out of scope

- The GET endpoints (`/api/auth/me`, `/api/auth/orgs`) stay
  unchanged.
- The cloud-side `auth.latere.ai/me/orgs` proxy logic in `orgs.go` is
  unaffected.
