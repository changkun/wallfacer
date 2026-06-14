# Cloud Mode

Wallfacer is built to run locally by default. Cloud mode is an opt-in feature flag that unlocks integrations with the [latere.ai](https://latere.ai) platform, starting with identity (sign-in, avatar, username, organizations) and expanding in later phases to metadata sync, tenant filesystem, cloud execution, and multi-tenant deployment.

This directory documents every cloud surface. Local deployments do not need to read any of it. For the gap analysis that reconciles these surfaces against the latere.ai components that already exist, see [`integration-plan.md`](integration-plan.md).

## Status: Identity Phase 1+2 shipped

Identity is fully landed. Sign-in, JWT validation, the principal/org model, superadmin gating, and forced login are all in the shipping runtime. Anonymous usage remains fully supported in local mode; the cloud flag only forces sign-in and adds the tenant-aware surfaces.

What is shipped:

- `WALLFACER_CLOUD` feature flag with fail-fast startup validation.
- **Account control** in the sidebar: a "Sign in" chip when signed out, the account menu (avatar, username, org switcher, sign out) when signed in. Since auth-by-default this surface is present in local mode too; cloud mode does not add it, it makes sign-in *required* (see [`auth-by-default.md`](../../specs/identity/auth-by-default.md)).
- **OIDC sign-in routes:** `/login`, `/callback`, `/logout`, `/logout/notify` (mounted unconditionally; handlers self-gate to 503/204 when the OIDC client is nil).
- **Principal endpoints:** `GET /api/me` returns the current signed-in user, or 204 when unauthenticated.
- **Org-switch routes:** `GET /api/auth/orgs` lists the user's organizations (204 when single-org or unauthenticated); `PATCH /api/auth/me` and `POST /api/me/switch-org` both validate membership, clear the session, and return a redirect to `/login?org_id=<target>`.
- **JWT validation on `/api/*`** via `OptionalAuth`. When `WALLFACER_CLOUD=true`, requests carrying `Authorization: Bearer <jwt>` have the token validated against the auth service's JWKS; valid tokens surface as `*auth.Claims` in handler context. Claims also flow in from the session cookie via `CookieAuth`, so browser callers present the same shape as Bearer callers. Both middlewares are nil-safe in local mode (`internal/cli/server.go:399-400`).
- **Principal/org model:** `store.Principal{Sub, OrgID}`, `Task.CreatedBy` (JWT `sub`) and `Task.OrgID` (JWT `org_id`) on records (`internal/store/models.go:293-304`), and `TasksForPrincipal` org-scoped filtering (`internal/store/principal.go`).
- **Superadmin gating** via `RequireSuperadmin`. In cloud mode, `POST /api/admin/rebuild-index` requires a JWT (or cookie session) whose `is_superadmin` claim is `true`; regular users get `403`, anonymous requests get `401`. Local mode reaches the handler with no claim, unchanged (`internal/cli/server.go:917-923`).
- **Forced login** via `ForceLogin` (`internal/cli/server.go:395-397`). In cloud mode, an anonymous browser GET for any HTML route is redirected to `/login?next=<original-path>`. `/login`, `/callback`, `/logout`, `/logout/notify`, `/api/config`, `/api/me`, `/favicon.ico`, and static asset paths (`/css/*`, `/js/*`, `/assets/*`, `/static/*`) pass through so the bootstrap works. API calls with `Accept: application/json` are not redirected; they still get a clean `401` from upstream. The `next=` target is validated to be path-only to close the open-redirect class of bug.

The authentication design that drove this work is archived/complete; see [`specs/shared/authentication.md`](../../specs/shared/authentication.md) for the historical record.

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

`WALLFACER_CLOUD` accepts `true`, `1`, `yes` (case-insensitive); everything else, including `false`, `0`, `no`, empty, and typos like `tru`, is false. Cloud mode fails closed on ambiguous values.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_CLOUD` | `false` | Enable cloud-gated UI surfaces, forced sign-in, and the tenant-aware routes. |
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

1. **Task execution is identical.** Host-process agent execution, worktree management, commit pipelines, automation, oversight: none of this changes in cloud mode. The runner execs the selected CLI as a host process with the task's git worktree as CWD; cloud mode adds identity and tenancy only, not a different execution path.
2. **No feature regression in local mode.** If cloud mode is off, the UI renders exactly what it did before cloud support landed. No placeholders, no stub affordances, no disabled buttons.

`WALLFACER_CLOUD` and `WALLFACER_SERVER_API_KEY` are orthogonal: the API key remains the auth mechanism for programmatic CLI/script access; browser sign-in via OIDC is added on top when cloud mode is on. Both can coexist. The bearer middleware bypasses its static-key check once a JWT or cookie identity is populated, so a cookie-only browser request succeeds even in a deployment that also sets `WALLFACER_SERVER_API_KEY` for scripts.

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

Later cloud work is design-level today, no ETA. It is reconciled against existing latere.ai components in [`integration-plan.md`](integration-plan.md); the headline is that wallfacer **consumes** platform services in cloud mode rather than rebuilding them.

- **Metadata-sync adapter (Cloud v1, proposed, NOT built).** The one genuinely new piece: a `MetadataSink` seam (proposed at `internal/cloudsync`, which does not exist yet) that replicates allowlisted, redacted task/spec metadata to a cloud metadata service over an authenticated HTTPS transport, with a bounded offline queue so a cloud outage never blocks local execution. Code never leaves the machine. See `integration-plan.md` Part 3 and the phasing in Part 5.
- `auth.RequireScope(name)` is scaffolded for routes to opt into as scopes are assigned; no route applies it today.
- **Tenant filesystem.** A thin `FSClient` over the fs.latere.ai Workspace API for cloud-hosted workspace storage. Blocked externally on FS Phase 5 (not built, not ours to unblock).
- **Cloud execution backend.** A future `executor.Backend` implementation (e.g. a Cella/K8s backend over `/v1/sandboxes`) selectable by config, replacing the host-process `HostBackend` only in cloud-execution deployments. Gated by demand; do not build wallfacer-owned K8s logic.
- **Agent token exchange.** RFC 8693 token minting so agents call FS/telemetry from cloud sandboxes. Needed only at the cloud-execution phase; the endpoint already exists in auth.
- **Multi-user collaboration.** Presence, audit log, RBAC. Team feature, post-v1.
- **Remote control.** latere.ai web UI reaching a user's locally-running wallfacer instance without inbound network. Exploratory.

See the [cloud track in `specs/README.md`](../../specs/README.md#cloud-platform) for the dependency graph and status, and [`integration-plan.md`](integration-plan.md) for the spec-by-spec verdicts.
