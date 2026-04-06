---
title: Authentication & Identity
status: drafted
depends_on: [latere.ai/auth]
affects: [internal/auth/, internal/handler/, ui/]
effort: small
created: 2026-03-28
updated: 2026-04-06
author: changkun
dispatched_task_id: null
---

# Authentication & Identity

## Problem

Wallfacer's only access control is a shared bearer token (`WALLFACER_SERVER_API_KEY`). There is no concept of user identity. Every request is anonymous once the token matches. This blocks two things:

1. **Cloud multi-tenant:** The control plane needs to map authenticated users to per-user instances.
2. **Single-host access control:** Even a personal VPS deployment benefits from real login over a manually-rotated static token.

## Scope

Authentication and identity are now handled by the centralized
**latere.ai auth service** (`auth.latere.ai`). This spec covers only
wallfacer's integration as an OIDC Relying Party.

This spec does **not** cover:
- OAuth provider integration (handled by the auth service)
- User model or storage (handled by the auth service)
- User management CRUD (handled by the auth service)
- Session management internals (handled by the auth service)
- Role-based access control definitions (handled by the auth service)
- Login UI with provider buttons (handled by the auth service)

---

## Design

### Overview

Wallfacer registers as an `oauth_client` with the latere.ai auth service
and delegates all authentication to it. Users log in at `auth.latere.ai`,
and wallfacer receives a JWT that identifies the user and their org context.

```
Browser -> Wallfacer -> redirect to auth.latere.ai/authorize
                              |
                    User authenticates (Google, X, email, etc.)
                              |
                    Redirect back to Wallfacer /auth/callback
                              |
                    Exchange code for tokens (access + refresh + ID)
                              |
                    Store tokens in session cookie -> redirect to /
```

### Client Registration

Wallfacer registers as a **confidential** oauth_client with the auth
service:
- `client_type`: confidential
- `redirect_uris`: `["https://wallfacer.latere.ai/auth/callback"]`
- `allowed_scopes`: `["openid", "email", "profile"]`

### Authentication Flow

Standard OAuth 2.0 Authorization Code flow with PKCE:

1. User visits wallfacer, has no session
2. Wallfacer redirects to `auth.latere.ai/authorize` with `client_id`,
   `redirect_uri`, `code_challenge`, `state`
3. User authenticates at the auth service (provider choice is handled there)
4. Auth service redirects back to `/auth/callback` with authorization code
5. Wallfacer exchanges code for tokens via `POST auth.latere.ai/token`
6. Wallfacer stores the access token and refresh token in a server-side
   session, sets a session cookie

### Token Handling

- **Access token**: JWT from the auth service. Contains `sub`
  (principal_id), `org_id`, `principal_type`. Short-lived (15 min).
- **Refresh token**: stored server-side, used to obtain new access tokens
  when they expire.
- **Session cookie**: `HttpOnly`, `Secure`, `SameSite=Lax`. Maps to the
  server-side session containing the tokens.

### JWT Validation

For API requests, wallfacer validates the JWT locally:

1. Fetch JWKS from `auth.latere.ai/.well-known/jwks.json` (cached, 1h TTL)
2. Verify signature, `exp`, `iss`, `aud`
3. Extract `sub` (principal_id) and `org_id`

No round-trip to the auth service on every request.

### Middleware

```go
// internal/auth/middleware.go

func RequireAuth(next http.Handler) http.Handler   // 401 if no valid session/token
func RequireAdmin(next http.Handler) http.Handler  // 403 if not admin (via /tokeninfo)
func OptionalAuth(next http.Handler) http.Handler  // sets user in context if present
```

**Context propagation:**

```go
func PrincipalFromContext(ctx context.Context) (principalID, orgID string, ok bool)
```

Handlers access the authenticated principal via context. No user identity
is propagated into task execution or container environments.

### Authorization

Wallfacer resolves permissions via the auth service's `/tokeninfo`
endpoint, cached for the token's remaining lifetime. For most routes,
a simple "is authenticated" check suffices. Admin-only routes (e.g.
instance management) check for the appropriate role via the cached
tokeninfo response.

### Data Model Changes

Wallfacer keys all user-specific and tenant-specific data on the auth
service's identifiers:

```go
type Workspace struct {
    ID          string    // workspace UUID
    OrgID       string    // from JWT org_id, tenant isolation
    CreatedBy   string    // from JWT sub, principal_id
    Name        string
    // ...
}
```

All queries filter by `org_id`. Ownership and attribution use
`principal_id` (from `sub`). Wallfacer never stores user profiles
locally; display info (name, avatar) is fetched from `/userinfo` and
cached with a short TTL.

### User Profile Resolution

When wallfacer needs to display user info (e.g. "workspace created by
Alice"):

```
GET auth.latere.ai/userinfo
Authorization: Bearer <token>
```

Cached locally for 5-15 minutes.

### Org Switching

If a user belongs to multiple orgs, wallfacer triggers a token refresh
with a new `org_id` parameter. The auth service issues a new access token
scoped to the target org.

---

## Compatibility with Standalone Mode

The `WALLFACER_SERVER_API_KEY` mechanism continues to work for self-hosted
deployments where the auth service is not available.

| Configuration | Behavior |
|---------------|----------|
| No auth service + no API key | Server is open (current default) |
| API key only | Current behavior, unchanged |
| Auth service configured | Login via auth service, full identity |
| Auth service + API key | Auth service for browser; API key for CLI/scripts |

When the auth service is not configured, wallfacer operates exactly as
it does today.

---

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `WALLFACER_AUTH_ISSUER_URL` | Auth service issuer URL | (unset = auth disabled) |
| `WALLFACER_AUTH_CLIENT_ID` | OAuth client ID | (required if issuer set) |
| `WALLFACER_AUTH_CLIENT_SECRET` | OAuth client secret | (required if issuer set) |
| `WALLFACER_SERVER_API_KEY` | Static API key for standalone mode | (unset = disabled) |

When `WALLFACER_AUTH_ISSUER_URL` is set, wallfacer fetches the OIDC
discovery document and configures itself automatically (authorization
endpoint, token endpoint, JWKS URI).

---

## API Routes

### Auth Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/auth/login` | Redirect to auth service |
| `GET` | `/auth/callback` | Handle auth service callback, create session |
| `POST` | `/auth/logout` | Destroy session, clear cookie |

### Authenticated Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/auth/me` | Current principal info (from cached /userinfo) |

User management (list users, change roles, remove users) is handled
entirely by the auth service's admin API, not by wallfacer.

---

## UI Changes

### Login

When auth is configured and the user has no valid session, all routes
redirect to `/auth/login`, which immediately redirects to the auth
service. The auth service presents the login UI (provider selection,
email code, etc.).

### Authenticated UI

- **Header:** Show user avatar + name (from cached /userinfo); dropdown
  with "Sign out"
- No user management UI in wallfacer. Users and roles are managed via
  the auth service.

### Unauthenticated Fallback

When auth is not configured, the UI behaves exactly as today.

---

## Implementation Order

1. **OIDC client integration** - Add `coreos/go-oidc` dependency, implement
   login redirect, callback handler, token exchange
2. **JWT middleware** - Validate tokens via JWKS, extract principal_id and
   org_id, set in request context
3. **Data model migration** - Add `org_id` and `principal_id` (created_by)
   columns to workspace/task tables, add org_id filtering
4. **UI integration** - Login redirect, header with user info, sign-out

### Dependencies

- **latere.ai auth service**: must be deployed and accessible. Wallfacer
  must be registered as an oauth_client before auth can be enabled.

### What Moved to the Auth Service

The following items from the original spec are now handled by the
latere.ai auth service and removed from wallfacer's scope:

- OAuth provider integration (GitHub, Google, Generic OIDC)
- User model and storage (User struct, StorageBackend persistence)
- Session store implementation (in-memory map, expiry, idle timeout)
- User management API (`/api/auth/users/*`)
- Session management API (`/api/auth/sessions/*`)
- Login UI with provider buttons
- First-user-is-admin bootstrap
- Trusted proxy mode (`X-Forwarded-User`)
- Email allow-list
- CSRF/session fixation handling (handled by auth service)
- Refresh token encryption (handled by auth service)
