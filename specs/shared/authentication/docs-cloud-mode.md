---
title: Cloud mode documentation
status: validated
depends_on:
  - specs/shared/authentication/status-bar-sign-in-badge.md
affects:
  - docs/guide/configuration.md
  - AGENTS.md
  - CLAUDE.md
effort: small
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Cloud mode documentation

## Goal

Document the new `WALLFACER_CLOUD` flag, the five `AUTH_*` env vars, the
sign-in badge, and the cloud-vs-local partition so users and future agents
understand where the line sits.

## What to do

1. **`docs/guide/configuration.md`** — add a new `## Cloud mode` subsection
   under the appropriate parent (likely the existing env-var reference).
   Cover:
   - What `WALLFACER_CLOUD=true` enables (sign-in badge, cloud routes).
   - Required env: `AUTH_CLIENT_ID`, `AUTH_CLIENT_SECRET`, `AUTH_REDIRECT_URL`.
   - Optional: `AUTH_URL` (default `https://auth.latere.ai`), `AUTH_COOKIE_KEY`.
   - Fail-fast startup behavior when cloud mode is on but config is missing.
   - Note: OIDC is latere.ai-specific; third-party OIDC is deferred.
   - Note: `WALLFACER_SERVER_API_KEY` remains orthogonal.
2. **`AGENTS.md` / `CLAUDE.md`** — append a short section or bullet under
   Configuration describing the cloud/local partition and pointing at the
   configuration guide.
3. **Env var table** — if there's a central env-var table (`docs/internals/*`
   or the Configuration section of `CLAUDE.md`), add `WALLFACER_CLOUD` and
   the `AUTH_*` vars with one-line descriptions.

## Tests

No automated tests. Verify:

- `make build` still passes (docs-only changes should not break the gate).
- `docs/guide/configuration.md` renders correctly in whatever doc server
  path the project uses (spot-check the new section).

## Boundaries

- Do not document the later phases (JWT middleware, `org_id`, agent token
  exchange) as shipping features — they remain "planned" in the spec.
- Do not add a CHANGELOG entry unless the project keeps one (it doesn't,
  as of this spec).
- Do not touch marketing / external docs.
