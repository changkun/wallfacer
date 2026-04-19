# Cloud Mode

Wallfacer is built to run locally by default. Cloud mode is an opt-in feature flag that unlocks integrations with the [latere.ai](https://latere.ai) platform — starting with identity (sign-in, avatar, username) and expanding in later phases to tenant filesystem, K8s-backed sandboxes, and multi-tenant deployment.

This directory documents every cloud surface. Local deployments do not need to read any of it.

## Status: Phase 1 — Sign-in badge

The only shipping cloud feature today is a latere.ai sign-in badge in the status bar. It does **not** gate any existing functionality. Anonymous usage remains fully supported; the cloud flag only adds identity rendering and the routes needed to support it.

What Phase 1 ships:

- `WALLFACER_CLOUD` feature flag with fail-fast startup validation.
- `/login`, `/callback`, `/logout`, `/logout/notify`, `/api/auth/me` HTTP routes (mounted unconditionally; handlers self-gate to 503/204 when the OIDC client is nil).
- Status-bar badge: avatar + username when signed in, "Sign in" link otherwise. Hidden entirely in local mode.
- Front-channel logout iframe so signing out at `auth.latere.ai` clears the wallfacer session cookie cross-tab.

What Phase 1 explicitly does **not** ship:

- No JWT middleware on API routes.
- No `org_id` / `principal_id` on workspace or task records.
- No authorization checks; nothing becomes sign-in-required.
- No agent token exchange.
- No forced login redirect for unauthenticated browsers.

The full long-range design lives in [`specs/shared/authentication.md`](../../specs/shared/authentication.md); later phases are tracked there.

## Enabling cloud mode

Cloud mode is toggled by one environment variable, set in the shell (not in `~/.wallfacer/.env`):

```bash
WALLFACER_CLOUD=true \
AUTH_CLIENT_ID=… \
AUTH_CLIENT_SECRET=… \
AUTH_REDIRECT_URL=https://your-host/callback \
wallfacer run
```

When `WALLFACER_CLOUD=true` but any of `AUTH_CLIENT_ID`, `AUTH_CLIENT_SECRET`, or `AUTH_REDIRECT_URL` is missing, the server logs a fatal error and exits. Misconfigured cloud deployments fail loudly instead of silently running without sign-in.

`WALLFACER_CLOUD` accepts `true`, `1`, `yes` (case-insensitive); everything else — including `false`, `0`, `no`, empty, and typos like `tru` — is false. Cloud mode fails closed on ambiguous values.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_CLOUD` | `false` | Enable cloud-gated UI surfaces and the sign-in routes. |
| `AUTH_URL` | `https://auth.latere.ai` | Auth service base URL |
| `AUTH_CLIENT_ID` | (required when cloud is on) | OAuth client ID registered with the auth service |
| `AUTH_CLIENT_SECRET` | (required when cloud is on) | OAuth client secret |
| `AUTH_REDIRECT_URL` | (required when cloud is on) | OAuth callback URL, e.g. `https://your-host/callback` |
| `AUTH_COOKIE_KEY` | (derived from client secret) | AES-GCM key for encrypted session cookies; set an explicit value in production so session cookies survive a rotation of the client secret |
| `AUTH_JWKS_URL` | `{AUTH_URL}/.well-known/jwks.json` | JWKS endpoint used to validate `Authorization: Bearer <jwt>` headers on API routes. Override only when the auth service publishes JWKS on a non-standard path. |
| `AUTH_ISSUER` | `AUTH_URL` | Expected `iss` claim on incoming JWTs. Override only when the auth service issues tokens with an issuer that differs from its base URL. |

Client registration with the auth service is a prerequisite. Wallfacer must be registered as a **confidential** `oauth_client` with `redirect_uris` containing `AUTH_REDIRECT_URL` and `allowed_scopes` including `openid`, `email`, `profile`.

## Cloud vs local partition

`WALLFACER_CLOUD` is the single gate that separates local-only functionality from cloud surfaces. Two invariants hold regardless of cloud state:

1. **Task execution is identical.** Container launch, worktree management, commit pipelines, automation, oversight — none of this changes in cloud mode. Cloud adds identity only.
2. **No feature regression in local mode.** If cloud mode is off, the UI renders exactly what it did before cloud support landed. No placeholders, no stub affordances, no disabled buttons.

`WALLFACER_CLOUD` and `WALLFACER_SERVER_API_KEY` are orthogonal: the API key remains the auth mechanism for programmatic CLI/script access; browser sign-in via OIDC is added on top when cloud mode is on. Both can coexist.

## OIDC specifics

The OIDC integration uses [`latere.ai/x/pkg/oidc`](https://github.com/latere-ai/pkg), which is specific to the latere.ai auth service:

- Encrypted cookie format (`__Host-latere-flow`, `__Host-latere-session`).
- Userinfo shape (`{sub, email, name, picture}`).
- Token refresh behavior and front-channel logout protocol.

Generic third-party OIDC (Keycloak, Entra ID, etc.) is **deferred**. Self-hosted deployments without latere.ai auth continue using the `WALLFACER_SERVER_API_KEY` static bearer.

## Front-channel logout

When a signed-in user signs out centrally at `auth.latere.ai`, the auth service loads `/logout/notify` on every wallfacer origin the user is authenticated to, via a hidden iframe. Wallfacer responds `200 OK` and clears the local session cookie. The frontend embeds a matching iframe pointing at `${AUTH_URL}/logout` whenever a signed-in session is displayed, so the cross-tab broadcast reaches this origin.

The endpoint is safe to load unauthenticated: it only clears a cookie and returns `200`.

## Roadmap

Later cloud phases (design-level today, no ETA):

- **JWT middleware on `/api/*`** (shipped in Phase 2's first child spec). When `WALLFACER_CLOUD=true`, requests carrying `Authorization: Bearer <jwt>` have the token validated against the auth service's JWKS; valid tokens surface as `*auth.Claims` in handler context via `auth.PrincipalFromContext(r.Context())`. Today this is `OptionalAuth` only (no route requires a token; claims just surface when present); strict 401 enforcement on admin routes and forced login for unauthenticated browsers land in later Phase 2 specs.
- **Authorization primitives.** `org_id` routing for multi-tenant, superadmin gating, scope-based route guards.
- **Tenant filesystem** — fs.latere.ai integration for cloud-hosted workspace storage.
- **K8s sandbox** — dispatch task containers as K8s Jobs instead of local podman/docker.
- **Multi-user collaboration** — presence, audit log, RBAC.
- **Remote control** — latere.ai web UI can reach a user's locally-running wallfacer instance without requiring inbound network.

See the [cloud track in `specs/README.md`](../../specs/README.md#cloud-platform) for the dependency graph and status.
