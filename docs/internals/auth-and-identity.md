# Auth & Identity

Wallfacer runs anonymously by default and stays fully functional that way. Sign-in against the latere.ai auth service is nevertheless wired on every `wallfacer run`: `resolveAuthConfig` (`internal/cli/server.go`) fills the public, secret-less `wallfacer` OIDC client against `https://auth.latere.ai` whenever no `AUTH_*` overrides are present, so a plain local server presents a working login button with zero configuration. `WALLFACER_CLOUD` does not control whether sign-in is available; it controls whether sign-in is *forced* (see [Cloud mode](#cloud-mode) below).

Signing in is what lights up the cross-machine surfaces: the coordination connector, the GitHub broker, actor attribution on tasks and events, and org switching.

## Three Sign-In Paths, One Token Store

All paths persist the token via `authkit.FileTokenStore` at `<UserConfigDir>/latere/token.json` (`~/Library/Application Support/latere/token.json` on macOS, `~/.config/latere/token.json` on Linux). This is the same file the `latere` CLI uses, so one login carries across the latere CLI, `wallfacer auth`, and wallfacer's own web UI and background connectors.

### Browser OIDC (authorization code + PKCE)

`/login`, `/callback`, `/logout`, and `/logout/notify` are mounted unconditionally through the apicontract route table and handled by `internal/handler/login.go`. The handlers delegate to `latere.ai/x/pkg/oidc` (re-exported as `internal/auth`) and self-gate: with no auth client wired, `/login` and `/callback` answer 503 so a broken deployment fails loudly, while `/logout` falls back to a bare cookie clear.

Configuration resolution (`resolveAuthConfig`):

- `AUTH_URL` defaults to `https://auth.latere.ai`, `AUTH_CLIENT_ID` to the seeded public client `wallfacer`, scopes to `openid email profile offline_access`.
- `AUTH_REDIRECT_URL` derives from the listen address: a loopback or wildcard host yields `http://localhost:<port>/callback` (matching the redirect registered for the public client); any other host is assumed to terminate TLS and uses `https`.
- Every `AUTH_*` value resolves shell environment first, then `~/.wallfacer/.env`, then the default, so `AUTH_CLIENT_ID=other wallfacer run` is a clean one-shot override.

**Session cookie and the cookie-key file.** A confidential client derives the AES-GCM session-cookie key from `AUTH_CLIENT_SECRET`. The default public client has no secret, so `loadOrCreateCookieKey` generates a 32-byte hex key once and persists it at `<configDir>/cookie-key` (mode 0600); sessions survive restarts, and an explicit `AUTH_COOKIE_KEY` takes precedence. On a loopback `http://` redirect URL the client sets `InsecureCookies`: browsers reject `Secure` cookies over plain HTTP, so `pkg/oidc` drops the `__Host-` cookie-name prefix and the Secure attribute for local serving.

**Principal endpoint.** `GET /api/me` (`Handler.AuthMe`) returns 204 for no session, or the latere-ui Principal shape (identity, avatar, active org, org list) assembled by `oidc.BuildMe` off a single up-front token refresh, plus `principal_id` and `auth_url` so the shared AccountMenu renders the avatar and org switcher.

**Front-channel logout.** When a user signs out centrally at the auth service, it loads `/logout/notify` on every signed-in origin via a hidden iframe; wallfacer clears the local session cookie and returns 200. The endpoint is safe to load unauthenticated. Both `Logout` and `LogoutNotify` also clear the coordination token first, so signing out stops the connector (see below).

### Device flow (RFC 8628)

Two consumers drive the device-authorization grant:

**The in-UI modal.** `POST /api/auth/device/start`, `GET /api/auth/device/poll`, `POST /api/auth/device/cancel` (`internal/handler/device_auth.go`) back the account-menu sign-in modal in local mode. `start` requests a device code (optionally org-scoped via `org_id`), returns the user code and verification URI, and launches a background goroutine that polls the token endpoint; calling `start` again cancels the in-flight flow, so at most one flow exists at a time. `poll` reports `idle | pending | done | denied | expired`. The poll request that observes completion mints the session cookie on its own response (`oidc.SetSession`), because the start goroutine's ResponseWriter is long gone by then; this is what makes `/api/me` reflect the sign-in immediately. The token is also saved to the shared file store, so the coordination connector and GitHub broker light up from the same login. A cookie-write failure is non-fatal: the file-store consumers still work and the SPA's `fetchMe` falls back to 204.

The driver is wired only in local mode (`!cloudMode` in `internal/cli/server.go`). Cloud deployments force the browser `/login` flow and have no writable per-user token store, so the endpoints answer 503 there, which is also the signal the SPA uses to fall back to the `/login` redirect instead of showing the device modal.

**The CLI.** `wallfacer auth login|logout|whoami` (`internal/cli/auth.go`) runs the same grant from a terminal via `authkit.NewDeviceCodeClient`, defaulting to the `wallfacer-cli` public client. Flags: `-auth-url`, `-client-id`, `-scopes`, `-org=<uuid>` / `-personal` (org-scoped login), `-no-browser`. `whoami` prints token expiry only; the access token is intentionally never echoed (`latere auth print-token` is the supported retrieval path).

## Middleware Chain

The server handler stack, outermost first (`internal/cli/server.go`, `startServerComponents`):

```
logging -> CSRF -> session token bridge -> CookieAuth -> OptionalAuth (JWT) -> BearerAuth -> [ForceLogin, cloud only] -> mux
```

- **CSRF** (`handler.CSRFMiddleware`): unsafe methods must present an `Origin` or `Referer` matching the server's known host:port or the request's own `Host` header (covering remote/IP access). Requests carrying neither header (curl, scripts, the CLI) pass through, so CSRF protection targets browsers without breaking programmatic clients.
- **Session token bridge** (`sessionTokenBridge.wrap`, `internal/cli/coordination.go`): mirrors the cookie session's access/refresh token into the shared file token store on every request, deduplicated on the access token so the file write happens once per token. This is why signing in via the board enables the coordination connector and GitHub broker without a separate `wallfacer auth login`.
- **CookieAuth** (`internal/auth/middleware.go`): decodes the AES-GCM-authenticated session cookie via `authkit.SessionAuthenticator` and injects an `*auth.Identity` into context. Decode failure passes through anonymous.
- **OptionalAuth**: validates an `Authorization: Bearer <jwt>` against the auth service's JWKS and injects the identity on success; missing, malformed, or expired tokens pass through anonymous rather than 401. It runs downstream of CookieAuth and overwrites the context identity, so a JWT wins when both are present. `BuildValidator` accepts both the client id and the issuer as audiences (the auth server stamps the issuer into every access token's `aud`); `AUTH_JWKS_URL` and `AUTH_ISSUER` override the derived defaults, and an unset issuer skips the `iss` check.
- **BearerAuth** (`handler.BearerAuthMiddleware`): the `WALLFACER_SERVER_API_KEY` static gate. With no key configured it is a no-op. With a key, every request must present `Authorization: Bearer <key>`, except: requests already carrying an identity from the cookie or JWT path bypass the check (so a cookie-only browser works in a deployment that also sets the key for scripts), SSE/WebSocket paths accept `?token=<key>` because `EventSource` cannot set headers, and `GET /` passes so the SPA shell loads.
- **ForceLogin** (`handler/force_login.go`, wrapped only when `WALLFACER_CLOUD` is on): anonymous browser GETs for HTML routes are redirected to `/login?next=<path>`. Only `GET` with `text/html` in `Accept` is considered, an allowlist (`/login`, `/callback`, `/logout`, `/logout/notify`, `/api/config`, `/api/me`, `/favicon.ico`, static asset prefixes) passes through so the bootstrap works, JSON API calls keep their clean 401, and `next` is validated to be path-only to close the open-redirect class of bug.

Both identity paths converge on the same context key: `auth.PrincipalFromContext(ctx)` returns the resolved `*auth.Identity` (`authkit.Identity`) or `(nil, false)` for anonymous.

## Principal Context and Actor Attribution

`principalFromRequest` (`internal/handler/principal.go`) is the single translation from context claims to the domain layer: it returns a `*store.Principal{Sub, OrgID}` or nil for anonymous callers. Nil means "unfiltered", which preserves local-mode behavior on every read path (`TasksForPrincipal` in `internal/store/principal.go`).

Attribution on records (`internal/store/models.go`):

- `Task.CreatedBy` holds the JWT `sub` of the user who dispatched the task; `Task.OrgID` the owning organization. Both are populated by the handler layer, empty for anonymous creations; the store never resolves claims itself.
- `TaskEvent.ActorSub` and `TaskEvent.ActorType` attribute every timeline event. `ActorType` is one of `user` (a signed-in human), `service` (a service-account JWT), `apikey` (a request gated only by `WALLFACER_SERVER_API_KEY`), `system` (the runner or a background goroutine with no request context), or empty (legacy/anonymous).

A small set of routes additionally *requires* a principal when auth is configured (`requiresPrincipal` in `internal/cli/server.go`): the spec-comment surface (`ListSpecComments`, `SubmitSpecComment`, `StreamSpecComments`) and `SubmitFeedback`. Spec comments read and write the coordination relay, which serves the connector's cached threads regardless of the browser session, so a logged-out browser must be rejected at the data layer, not just hidden in the SPA. Local mode with no auth remains permissive.

## What Sign-In Enables

**Coordination connector.** A signed-in instance can hold one outbound WebSocket to the cloud coordinator (`wss://wf.latere.ai/api/coordination/ws`, overridable with `WALLFACER_COORDINATION_URL`) for cross-machine spec-comment collaboration. The connector reads its token from the shared file store; the session token bridge writes the board login's token into that same store, so a UI sign-in enables coordination automatically. A runtime opt-in gate (`coordinationGate`, persisted to a flag file under the config dir) governs whether anything dials: the default is on for a signed-in instance (a deliberate product decision, collaboration on by default once signed in), overridable with `WALLFACER_COORDINATION=0` or the in-app toggle (`SetCoordinationOptIn`, body `{"enabled": bool}`). An anonymous instance dials nothing regardless; that is the data-boundary floor. Signing out (`Logout`, `LogoutNotify`, and the account-menu path) clears the coordination token, which drops the live connection and stops re-dialing.

**GitHub broker.** `github.HTTPBroker` is wired whenever an auth URL is configured, which since auth-by-default is effectively always. Its `TokenSource` loads the shared store's OIDC token, and with it wallfacer *borrows the signed-in latere.ai account's GitHub connection* from the auth service: there is no wallfacer-side GitHub OAuth dance, and connect returns the central install URL (`<authURL>/me/integrations/github/install/start`). The effective gate for PR creation and comments is therefore: signed in, plus a GitHub connection on the account.

**Identity on work.** Tasks and events created while signed in carry `CreatedBy`/`OrgID`/`ActorSub` as described above.

**Org switching.** `GET /api/auth/orgs` lists the user's organizations (204 when single-org or unauthenticated); `PATCH /api/auth/me` and `POST /api/me/switch-org` (`internal/handler/orgs.go`) validate membership, clear the session, and redirect to `/login?org_id=<target>` so the new session is minted in the target org context. The CLI equivalent is `wallfacer auth login -org=<uuid>`.

## Cloud Mode

`WALLFACER_CLOUD` gates the multi-tenant, hosted posture. It parses via `envconfig.ParseBoolFlag`: `true`, `1`, `yes` (case-insensitive) enable it; everything else, including typos, is false, so cloud mode fails closed. Two invariants hold regardless of the flag:

1. **Task execution is identical.** Host-process agent execution, worktrees, commit pipelines, automation, oversight: cloud mode adds identity and tenancy, not a different execution path.
2. **No regression in local mode.** With the flag off, the UI and API behave as a single-user anonymous instance unless the user chooses to sign in.

`WALLFACER_CLOUD` and `WALLFACER_SERVER_API_KEY` are orthogonal: the static key remains the auth mechanism for programmatic access, and the bearer middleware bypasses it once a cookie or JWT identity is present.

What the flag changes:

- **Forced login.** The `ForceLogin` wrapper described above is installed only in cloud mode.
- **Org-scoped isolation.** `SetCloudMode(true)` makes workspace visibility and task listing principal-scoped: only cloud deployments hide workspaces and tasks a principal's org cannot see. A local run keeps every workspace visible regardless of session org.
- **Device endpoints disabled.** `/api/auth/device/*` answers 503; browser `/login` is the only sign-in path.
- **Superadmin gating.** `POST /api/admin/rebuild-index` is wrapped by `auth.RequireSuperadmin` whenever an OIDC client is wired (`adminOnly` in `BuildMux`, keyed on `h.HasAuth()`, which auth-by-default makes true on every run): anonymous callers get 401, signed-in non-superadmins 403, and only a session whose `is_superadmin` claim is true reaches the handler. `auth.RequireScope(name)` is scaffolded alongside it; no route applies it yet.
- **Sandbox trust-plane proxy.** `/internal/sandbox-proxy/llm/anthropic/*`, `/internal/sandbox-proxy/llm/openai/*`, and `/internal/sandbox-proxy/github-token` (`internal/handler/sandbox_proxy.go`) let cloud sandboxes reach LLM providers and GitHub without holding real credentials. Configuration comes from `SANDBOX_PROXY_AUTH_INSTALLATION_URL` (auth's installation-token endpoint), `SANDBOX_PROXY_AUTH_SERVICE_TOKEN` (wallfacer's long-lived service JWT, scope `github:mint-token`), and the provider keys; the routes answer 503 until every required field is set, which is the permanent local-mode state. Inbound requests carry a sandbox JWT with `aud=wallfacer-sandbox-proxy` and per-route scopes (`llm:proxy`, `github:token`); the proxy substitutes the real provider key (`x-api-key` for Anthropic, `Authorization: Bearer` for OpenAI) and, for git, mints a per-repo GitHub App installation token via auth.
- **wallfacerd.** `wallfacer web` (`internal/cli/web.go`) runs the hosted control plane: OIDC sign-in, the coordination WebSocket acceptor (`GET /api/coordination/ws`) that local instances dial into, a spec-comment store backed by Postgres (`WALLFACER_DATABASE_URL`, falling back to memory), a RUM telemetry proxy, and the SPA in cloud mode (`window.__WALLFACER__.mode` selects the cloud route table).

### OIDC specifics

The integration is specific to the latere.ai auth service via `latere.ai/x/pkg/oidc`: the encrypted cookie format (`__Host-latere-flow`, `__Host-latere-session`), the userinfo shape, token refresh, and the front-channel logout protocol. Generic third-party OIDC (Keycloak, Entra ID) is deferred; self-hosted deployments without latere.ai auth use the `WALLFACER_SERVER_API_KEY` static bearer.

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_CLOUD` | `false` | Force sign-in and enable the tenant-aware surfaces |
| `AUTH_URL` | `https://auth.latere.ai` | Auth service base URL |
| `AUTH_CLIENT_ID` | `wallfacer` | OAuth client id (public, secret-less default) |
| `AUTH_CLIENT_SECRET` | unset | Set for a confidential client; also becomes the cookie-key source |
| `AUTH_REDIRECT_URL` | derived from listen address | OAuth callback URL |
| `AUTH_COOKIE_KEY` | generated at `<configDir>/cookie-key` | Session-cookie encryption key; set explicitly in production so sessions survive secret rotation |
| `AUTH_JWKS_URL` | `{AUTH_URL}/.well-known/jwks.json` | JWKS endpoint for Bearer-JWT validation |
| `AUTH_ISSUER` | unset (skip `iss` check) | Expected `iss` claim on incoming JWTs |
| `WALLFACER_SERVER_API_KEY` | unset (gate off) | Static bearer for programmatic access |
| `WALLFACER_COORDINATION` | on when signed in | Set `0` to default the coordination opt-in off |
| `WALLFACER_COORDINATION_URL` | `wss://wf.latere.ai/api/coordination/ws` | Coordinator endpoint the connector dials |
| `SANDBOX_PROXY_AUTH_INSTALLATION_URL` | unset | Auth's `/internal/github/installation-token` endpoint (cloud trust plane) |
| `SANDBOX_PROXY_AUTH_SERVICE_TOKEN` | unset | Wallfacer's service JWT for minting installation tokens |
| `WALLFACERD_ADDR` | `:8080` | Listen address for `wallfacer web` |
