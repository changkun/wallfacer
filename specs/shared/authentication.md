# Authentication & Identity

**Date:** 2026-03-28

## Problem

Wallfacer's only access control is a shared bearer token (`WALLFACER_SERVER_API_KEY`). There is no concept of user identity — every request is anonymous once the token matches. This blocks two things:

1. **Cloud multi-tenant:** The control plane needs to map authenticated users to per-user instances. The control plane assumes auth exists but doesn't specify how it works.
2. **Single-host access control:** Even a personal VPS deployment benefits from real login (OAuth) over a manually-rotated static token. Teams sharing a single instance today have zero auditability.

Authentication is a prerequisite for multi-tenant, not part of it. It is also independently useful: a single-user deployment can adopt OAuth login without any multi-tenant infrastructure.

---

## Scope

This spec covers identity and session management. It does **not** cover:
- Instance provisioning or traffic routing (multi-tenant spec)
- Role-based access control beyond admin/member distinction
- Fine-grained per-workspace permissions (future spec if needed)

---

## Design

### Authentication Flow

```
Browser → /auth/login → redirect to OAuth provider
                              ↓
                    Provider callback → /auth/callback
                              ↓
                    Validate ID token / exchange code
                              ↓
                    Create or update user record
                              ↓
                    Issue session cookie → redirect to /
```

All subsequent requests carry the session cookie. The auth middleware validates the session before dispatching to handlers.

### Provider Support

Support multiple OAuth2/OIDC providers through a unified interface:

| Provider | Protocol | Notes |
|----------|----------|-------|
| **GitHub** | OAuth2 | Most common for dev tools; use GitHub App or OAuth App |
| **Google** | OIDC | Standard OpenID Connect; ID token contains email + sub |
| **Generic OIDC** | OIDC Discovery | Any provider with `/.well-known/openid-configuration` |

Only one provider needs to be configured for the server to start with auth enabled. Multiple providers can be configured simultaneously (login page shows all).

### Session Management

**Cookie-based sessions** with server-side state:

- Session ID in `HttpOnly`, `Secure`, `SameSite=Lax` cookie
- Server stores session → user mapping (in-memory map + optional persistent backing)
- Session expiry: configurable, default 7 days
- Idle timeout: configurable, default 24 hours of no requests
- Explicit logout (`POST /auth/logout`) clears cookie and server-side session
- Token refresh: for OIDC providers, refresh tokens are stored server-side and used to extend sessions transparently

**Why not JWT-only?** Stateless JWTs cannot be revoked without a blocklist (which reintroduces server state). Cookie + server session is simpler, revocable, and familiar.

### User Identity Model

```go
// internal/auth/user.go

type User struct {
    ID         string    // UUID
    Provider   string    // "github", "google", "oidc"
    ProviderID string    // external user ID from the provider
    Email      string    // from ID token or userinfo endpoint
    Name       string    // display name
    AvatarURL  string    // profile picture URL
    Role       Role      // admin or member
    CreatedAt  time.Time
    LastLogin  time.Time
}

type Role string

const (
    RoleAdmin  Role = "admin"
    RoleMember Role = "member"
)
```

**Storage:** Users and sessions are stored via the existing `StorageBackend` interface. For filesystem backend, this is a JSON file in the data directory. For cloud deployments, PostgreSQL or equivalent.

**First user is admin.** The first user to log in becomes admin. Subsequent users are members. Admins can promote/demote via API.

### Middleware Integration

```go
// internal/auth/middleware.go

func RequireAuth(next http.Handler) http.Handler
func RequireAdmin(next http.Handler) http.Handler
func OptionalAuth(next http.Handler) http.Handler
```

- `RequireAuth`: Returns 401 if no valid session. Sets user in request context.
- `RequireAdmin`: Returns 403 if authenticated user is not admin.
- `OptionalAuth`: Sets user in context if session exists, proceeds regardless.

**Context propagation:**

```go
func UserFromContext(ctx context.Context) (*User, bool)
```

Handlers access the authenticated user via context. No user identity is propagated into task execution or container environments — tasks remain user-agnostic at the runner level.

### Compatibility with Existing Auth

The current `WALLFACER_SERVER_API_KEY` mechanism continues to work:

| Configuration | Behavior |
|---------------|----------|
| No OAuth + no API key | Server is open (current default) |
| API key only | Current behavior, unchanged |
| OAuth only | Login required, API key ignored |
| OAuth + API key | OAuth for browser sessions; API key for programmatic access (CLI, scripts) |

When OAuth is enabled, the `WALLFACER_SERVER_API_KEY` serves as a service account token for non-browser clients (CI, CLI tools, the future control plane). Requests with a valid `Authorization: Bearer <api-key>` header bypass OAuth and are treated as admin.

### Multi-Tenant Integration

When deployed behind the multi-tenant control plane:

- The control plane handles authentication itself and sets `X-Forwarded-User` / `X-Forwarded-Email` headers
- Wallfacer trusts these headers when configured with `WALLFACER_TRUSTED_PROXY` (comma-separated CIDRs)
- In this mode, wallfacer skips its own OAuth flow and creates/updates user records from forwarded headers
- This avoids double-login (control plane + instance)

---

## Configuration

New environment variables (all in `~/.wallfacer/.env`):

| Variable | Description | Default |
|----------|-------------|---------|
| `WALLFACER_AUTH_PROVIDER` | Comma-separated list: `github`, `google`, `oidc` | (unset = auth disabled) |
| `WALLFACER_AUTH_SESSION_TTL` | Session lifetime | `168h` (7 days) |
| `WALLFACER_AUTH_SESSION_IDLE` | Idle timeout before session expires | `24h` |
| `WALLFACER_AUTH_ALLOWED_EMAILS` | Comma-separated allow-list of email addresses or domains (e.g., `*@example.com`) | (unset = any authenticated user) |
| `WALLFACER_TRUSTED_PROXY` | CIDRs to trust for `X-Forwarded-User` | (unset = disabled) |

**Per-provider variables:**

| Variable | Provider | Description |
|----------|----------|-------------|
| `WALLFACER_GITHUB_CLIENT_ID` | GitHub | OAuth App client ID |
| `WALLFACER_GITHUB_CLIENT_SECRET` | GitHub | OAuth App client secret |
| `WALLFACER_GOOGLE_CLIENT_ID` | Google | OAuth 2.0 client ID |
| `WALLFACER_GOOGLE_CLIENT_SECRET` | Google | OAuth 2.0 client secret |
| `WALLFACER_OIDC_ISSUER_URL` | Generic OIDC | Issuer URL (must serve `/.well-known/openid-configuration`) |
| `WALLFACER_OIDC_CLIENT_ID` | Generic OIDC | Client ID |
| `WALLFACER_OIDC_CLIENT_SECRET` | Generic OIDC | Client secret |

---

## API Routes

### Auth Endpoints (unauthenticated)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/auth/login` | Show login page (lists enabled providers) |
| `GET` | `/auth/login/{provider}` | Initiate OAuth flow for provider |
| `GET` | `/auth/callback/{provider}` | OAuth callback; creates session, redirects to `/` |
| `POST` | `/auth/logout` | Destroy session, clear cookie |

### User Management (authenticated)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/auth/me` | Current user profile |
| `GET` | `/api/auth/users` | List all users (admin only) |
| `PATCH` | `/api/auth/users/{id}` | Update user role (admin only) |
| `DELETE` | `/api/auth/users/{id}` | Remove user (admin only) |

### Session Management

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/auth/sessions` | List active sessions for current user |
| `DELETE` | `/api/auth/sessions/{id}` | Revoke a specific session |

---

## UI Changes

### Login Page

When auth is enabled and the user has no valid session, all routes redirect to `/auth/login`. The login page shows:

- Wallfacer logo and name
- One button per configured provider ("Sign in with GitHub", "Sign in with Google", etc.)
- No username/password form (OAuth only)

### Authenticated UI

- **Header:** Show user avatar + name in the top bar; dropdown with "Sign out"
- **Settings → Users** (admin only): List users, change roles, remove users
- **Settings → Sessions:** List own active sessions, revoke individual sessions

### Unauthenticated Fallback

When auth is not configured, the UI behaves exactly as today — no login page, no user avatar, no user management.

---

## Security Considerations

- **CSRF protection:** All state-changing endpoints require the session cookie (SameSite=Lax blocks cross-origin POST). OAuth state parameter prevents login CSRF.
- **Session fixation:** New session ID generated on every login, never reuse pre-auth session IDs.
- **Secret storage:** Client secrets stored in `.env`, never logged or exposed via API. Refresh tokens encrypted at rest.
- **Email allow-list:** Optional `WALLFACER_AUTH_ALLOWED_EMAILS` restricts who can log in. Supports domain wildcards (`*@company.com`).
- **Open redirect:** Callback handler validates redirect URLs against the server's own origin.

---

## Implementation Order

1. **Auth package scaffold** — `internal/auth/`: User model, session store (in-memory), middleware, context helpers
2. **GitHub OAuth provider** — Most common; proves the pattern end-to-end
3. **Session management** — Cookie handling, expiry, idle timeout, logout
4. **Login UI** — Login page, header integration, redirect-on-401
5. **User management API** — CRUD, role assignment, admin bootstrap
6. **Google OIDC provider** — Second provider validates the multi-provider abstraction
7. **Generic OIDC provider** — Covers corporate IdPs (Okta, Auth0, Keycloak, etc.)
8. **Trusted proxy mode** — `X-Forwarded-User` support for multi-tenant control plane integration
9. **Email allow-list** — Domain/address filtering

### Dependencies

- **Storage Backend Interface:** User and session persistence uses `StorageBackend`. Filesystem backend is sufficient for single-host; cloud backends needed for multi-tenant.
- **Multi-Tenant:** Consumes this spec's trusted proxy mode. Multi-tenant does not need to implement its own auth — it delegates to this.

### What Multi-Tenant No Longer Needs to Cover

With this spec implemented, Multi-tenant can remove:
- Section "1. Authentication Gateway" (fully covered here)
- The `users` table from "Data Model Changes" (moved here)
- OAuth/OIDC library selection (decided here)
- Session management design (decided here)
