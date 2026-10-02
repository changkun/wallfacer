# Known bugs / follow-ups

## Workspace isolation: data-layer gating

The visibility fix (`visibleWorkspaces`/`isAllowedWorkspace`/`requireVisibleWorkspace`)
covers the browser-facing surfaces: spec tree, file list, file explorer, git
status, terminal cwd, planning reads (thread list, messages, SSE stream),
planning + spec mutations (send/clear/interrupt/start, thread create/patch,
undo, archive/unarchive, dispatch/undispatch), and the stats planning
aggregation. A session that cannot see the active org-stamped workspace gets
empty reads and 403 mutations, matching `/api/config`.

Remaining, intentionally not gated:

- **`env.go` TestSandbox** seeds its probe runner with `currentWorkspaces()`.
  It is a Settings connectivity probe (reached from the always-available
  Settings page), not a workspace-data surface, and gating it would block
  sandbox testing whenever an org workspace is active. Left ungated by design;
  revisit if a probe must never run against a workspace the caller can't see.
- Internal/background callers (`persistAgentRoundUsage`, group-toggle/limit
  helpers, the spec-completion callback) keep `currentWorkspaces()` — they run
  without a request principal and act on the active group regardless of viewer.

## Redirect sign-in is unavailable on a fallback port

`internal/cli/server.go` resolves the default OAuth `redirect_uri` from the
*requested* address (`cfg.Addr`) via `defaultRedirectURL`, before `net.Listen`
may fall back to an OS-assigned port when `cfg.Addr` is taken (e.g. a second
wallfacer instance). The redirect then names a port this server is not
listening on. In practice that port belongs to the first instance, so the
issuer would deliver the authorization code there.

Fixed: the flow is no longer started where it cannot return. After binding,
`initServer` compares the port in the default redirect URL with the bound port
(`redirectPortMismatch`); the two also differ when port 0 is requested. On a
mismatch it logs a warning and calls `Handler.SetRedirectSignInOff`. From then
on:

- `GET /login` answers 503 with the error `redirect_sign_in_unavailable`
  instead of redirecting to the issuer.
- The org switch (`POST /api/me/switch-org`, `PATCH /api/auth/me`), which
  completes through `/login`, answers the same error before it clears the
  session.
- `/api/config` reports `auth_redirect_enabled: false`, and the account menu
  shows the reason in the device sign-in modal instead of navigating to
  `/login`, for a sign-in that cannot start by device code and for an org
  switch.

An operator-set `AUTH_REDIRECT_URL` is exempt: it names the address the
deployment is reached on, which need not be the bound port. The device-code
flow needs no redirect and works on any port. On a local instance `GET /login`
and `GET /callback` pass the server-key check for a browser on the same
machine, signed in or not, so both a first sign-in and an org switch reach
these answers. A peer on another host reaches them only with a session or the
server key.

Still open: sign-in by browser redirect, and with it the in-app org switch,
does not work on a fallback port. Deriving the redirect from the *bound* port
does NOT fix it: the auth service (auth.latere.ai) uses ory/fosite with exact
`redirect_uri` matching, and fosite's RFC 8252 dynamic-port loopback exception
applies only to IP literals (`127.0.0.1`/`[::1]`), not the `localhost` hostname
that `defaultRedirectURL` emits (`isLoopbackAddress` =
`net.ParseIP(host).IsLoopback()`). So the registered redirect must match the
port exactly; a bound-port redirect fails at `/authorize`.

Full fix (only if port-fallback sign-in matters): emit
`http://127.0.0.1:<port>/callback` (loopback IP literal) AND register a
`http://127.0.0.1/callback` redirect for the public `wallfacer` client in the
auth DB. Then fosite accepts any dynamic port, the redirect can be derived
from the bound port, and the gate above has nothing left to switch off.
Requires an auth-service registration change; not done here.
