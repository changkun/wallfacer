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
- Internal/background callers (`persistPlanningRoundUsage`, group-toggle/limit
  helpers, the spec-completion callback) keep `currentWorkspaces()` — they run
  without a request principal and act on the active group regardless of viewer.

## OAuth redirect URL uses the requested port, not the bound port

`internal/cli/server.go` resolves the OAuth `redirect_uri` from the *requested*
address (`cfg.Addr`) via `defaultRedirectURL`, before `net.Listen` may fall back
to a random port when `cfg.Addr` is taken (e.g. a second wallfacer instance). The
redirect then points at a port the server is not listening on.

Deriving the redirect from the *bound* port does NOT fix sign-in: the auth
service (`../auth`) uses ory/fosite with exact `redirect_uri` matching, and
fosite's RFC 8252 dynamic-port loopback exception applies only to IP literals
(`127.0.0.1`/`[::1]`), not the `localhost` hostname that `defaultRedirectURL`
emits (`isLoopbackAddress` = `net.ParseIP(host).IsLoopback()`). So the registered
redirect must match the port exactly; a bound-port redirect just fails earlier at
`/authorize` instead of at the callback.

Real fix (only if port-fallback sign-in matters): emit
`http://127.0.0.1:<port>/callback` (loopback IP literal) AND register a
`http://127.0.0.1/callback` redirect for the public `wallfacer` client in the
auth DB. Then fosite accepts any dynamic port and the bound-vs-requested port
distinction is moot. Requires an auth-service registration change; not done here.
