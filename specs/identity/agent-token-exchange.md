---
title: Agent Token Exchange
status: archived
depends_on:
  - specs/identity/authentication.md
affects:
  - internal/handler/
  - internal/runner/
  - internal/auth/
  - internal/store/
effort: medium
created: 2026-04-19
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Agent Token Exchange

> **Dormant (demand-gated).** The trust plane this spec needed already shipped
> as the `sandbox_proxy.go` server-side proxy, and the one open decision is
> resolved (extend the proxy). The remaining per-task token-mint path
> (`ExchangeForAgentToken`, `LATERE_AI_TOKEN` env, `AgentDelegationID`) is not
> built and not yet needed: it is consumed only once we call latere.ai backend
> services (fs.latere.ai, telemetry) from inside running tasks, and those
> services don't exist yet. Kept as the record of the resolved design and the
> future need; reopen when the first such backend service lands.

## Problem

Wallfacer launches AI agents as host processes in per-task git worktrees
(the container runtime was removed; see Reality below). As latere.ai grows
out additional services (fs.latere.ai for file storage, telemetry ingestion,
etc.), those agents need credentials to call those services **on behalf of**
the user who dispatched the task, not with the user's own refresh-capable
JWT, and not as anonymous clients.

RFC 8693 token exchange fits: the user's access token is the `subject_token`,
the auth service mints a scoped `agent_token` tied to a delegation record,
and that short-lived token reaches the agent (either injected into the agent
process environment, or held by wallfacer and applied per request, see the
Reality section and the open decision).

This spec was pulled out of `identity/authentication.md` because it is
**orthogonal** to user login / auth / tenant isolation. It unblocks nothing
on the cloud move; it is consumed only when we start calling latere.ai
backend services *from inside running tasks*.

## Reality (shipped since this spec was drafted)

The trust-plane half of this design already shipped, but as a **proxy**, not
as a token injected into the agent. `internal/handler/sandbox_proxy.go`
exposes three routes wired in `internal/cli/server.go`:

- `POST /internal/sandbox-proxy/llm/anthropic/...`
- `POST /internal/sandbox-proxy/llm/openai/...`
- `GET  /internal/sandbox-proxy/github-token?repo=owner/name`

The shipped model already implements the RFC 8693 delegation this spec
describes, just on the server side:

- The agent (sidecar) presents a JWT with `aud=wallfacer-sandbox-proxy` and a
  per-route scope (`scp=llm:proxy` or `scp=github:token`); `requireClaims`
  validates it via `auth.Validator`.
- `delegatorSub(claims)` reads `act.sub` (the delegating user) per RFC 8693,
  falling back to `claims.Sub` when `act` is absent. The GitHub route uses
  that principal to resolve a per-repo installation token.
- The proxy substitutes the upstream credential itself: it swaps the inbound
  placeholder Authorization for `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`
  (v1 shares one org-level key per provider). Secrets never enter the agent
  worktree.

Consequently the `jwtauth.Claims` fields this spec needs are already live and
consumed: `Validation` (`ValidationLocal` / `ValidationStrict`),
`DelegationID`, and `Act.Sub` (see `latere.ai/x/pkg/jwtauth`). The JWT Claim
Reference table below is no longer aspirational.

What is NOT yet built: minting an `agent_token` per task via the
token-exchange grant, and the `internal/auth` exchange call. The proxy
currently trusts whatever sandbox JWT the sidecar already holds; this spec is
about where that JWT comes from.

## Scope

In scope:
- Wire `POST auth.latere.ai/token` (grant `urn:ietf:params:oauth:grant-type:token-exchange`)
  into wallfacer's runner so it can mint an `agent_token` from the dispatching
  user's session token.
- Mint an `agent_token` per task execution, scoped to `validation="local"`
  (`ValidationLocal`) for read-only agent work and `validation="strict"`
  (`ValidationStrict`) for agents that perform writes against latere.ai
  services.
- Decide how the agent reaches latere.ai services (see Open Decisions). Either
  inject the token into the agent process environment via
  `executor.ContainerSpec.Env` (e.g. `LATERE_AI_TOKEN`), or extend the shipped
  sandbox proxy so the sidecar's JWT carries the right `act.sub` and the proxy
  performs the exchange server-side.
- If the env-injection path wins: handle 15-minute TTL (re-mint on turn
  boundary, or surface a recoverable `token_expired` failure that auto-retry
  can replay). The proxy path makes this moot (see Refresh strategy).
- Record the `delegation_id` on the task's execution environment so audit
  logs can trace an agent action back to the delegating user.

Out of scope:
- Agent-principal registration in the auth service (handled by the auth
  service admin API; wallfacer assumes delegations are pre-provisioned or
  created implicitly by the exchange call).
- Per-service authorization policy (fs.latere.ai decides what an
  `agent_token` with scope X can do, not wallfacer).
- User-facing delegation UI (view / revoke active agent sessions).
- `internal/oauth/` (the PKCE OAuth flow for agent *credential* login like
  Claude/Codex, entirely separate from RFC 8693 token exchange).

## JWT Claim Reference

From `identity/authentication.md`'s claim table, the agent-specific fields are
(all live in `latere.ai/x/pkg/jwtauth` and consumed by `sandbox_proxy.go`):

| Claim | Go field | Description |
|-------|----------|-------------|
| `validation` | `Claims.Validation` | `"local"` (JWKS-only) or `"strict"` (tokeninfo call required for writes) |
| `delegation_id` | `Claims.DelegationID` | Delegation record UUID, for audit + revocation |
| `act.sub` | `Claims.Act.Sub` | Delegator's principal ID (the user who dispatched the task) |

## Design Sketch

Env-injection variant (the original sketch, now one of two options):

```
┌────────────────────────────────────────────┐
│ runner starts task for user U              │
│ ├─ load U's access token (session)         │
│ ├─ POST auth.latere.ai/token               │
│ │   grant_type=token-exchange              │
│ │   subject_token=<user JWT>               │
│ │   agent_id=<task UUID>                   │
│ └─ receive agent_token (15m TTL)           │
│                                            │
│ buildBaseContainerSpec sets                │
│   spec.Env["LATERE_AI_TOKEN"]=<agent_token>│
│   spec.Env["LATERE_AI_DELEGATION"]=<uuid>  │
│ then HostBackend launches the agent in     │
│ the task worktree with that env            │
└────────────────────────────────────────────┘
```

Proxy variant (matches what shipped): the agent never holds a token; it calls
`/internal/sandbox-proxy/*` and wallfacer applies the credential after
checking `act.sub`. See Open Decisions.

### Refresh strategy

This section applies **only to the env-injection variant**. Agent tokens have
no refresh capability (per the auth service's design). Options:
1. **Re-mint on turn boundary**, before each agent launch in a multi-turn
   task, re-exchange the user's (refreshed) session token. Simple, adds one
   auth call per turn (~20 ms).
2. **Fail-fast + auto-retry**, let the token expire mid-turn; when the agent
   hits a 401 from fs.latere.ai, bubble up `token_expired` as a recoverable
   failure category and let the existing retry engine re-mint on the next run.
   Cheaper, but the mid-turn failure is user-visible.

Prefer (1) for quality; fall back to (2) if the per-turn call cost is
measurable in the trace spans.

Under the **proxy variant** this section dissolves entirely: wallfacer holds
the user session and performs the exchange (or credential substitution)
per request, server-side, so the agent never holds a token that can expire.
That is a real point in the proxy's favor.

## Integration Points

- `internal/handler/sandbox_proxy.go`, the shipped trust plane; the proxy
  variant extends this rather than adding env injection.
- `internal/auth/`, add `ExchangeForAgentToken(ctx, userToken, agentID, scopes) (agentToken, delegationID, error)`.
- `internal/runner/container.go`, the agent process env is assembled in
  `buildBaseContainerSpec` (sets `spec.Env`) and the surrounding
  `spec.Env[...]` writes in `container.go`. For the env-injection variant,
  call the exchange there and set `spec.Env["LATERE_AI_TOKEN"]` /
  `spec.Env["LATERE_AI_DELEGATION"]`. (There is no `ContainerSpec.Build()`;
  the host backend reinterprets `ContainerSpec.Env` directly. The stale
  reference in `internal/executor/doc.go` should be dropped.)
- `internal/store/`, the task `ExecutionEnvironment`
  (`internal/store/models.go`) does not yet carry a delegation field; add
  `AgentDelegationID string` (persisted via `Store.UpdateTaskEnvironment`) so
  the trace survives. Note `ExecutionEnvironment` still has stale
  `ContainerImage` / `ContainerDigest` fields from the container era.
- Agent process env contract, document `LATERE_AI_TOKEN` +
  `LATERE_AI_DELEGATION` as the env vars agents read when calling latere.ai
  services (env-injection variant only).

## Dependencies

- `identity/authentication.md` Phase 2 must be complete, this spec needs a
  real user `*jwtauth.Claims` on the task creator and a populated session
  access token on the request path.
- The auth service must expose the `token-exchange` grant and accept an
  `agent_id` parameter (currently documented but not yet end-to-end tested
  from wallfacer's side).

## Open Decisions

- **Proxy vs env-injection - RESOLVED: extend the proxy.** The trust plane
  already shipped as a server-side proxy (`sandbox_proxy.go`) that performs
  `act.sub` delegation and credential substitution per request, so secrets
  never enter the worktree and there is no token TTL to manage. The chosen
  design extends that proxy (route fs.latere.ai and telemetry through it,
  carrying `act.sub`) rather than injecting a 15-minute `agent_token` into
  the agent environment: the proxy model is already proven for LLM and
  GitHub credentials and removes the refresh problem. Env injection is kept
  only as a documented fallback for services the proxy genuinely cannot
  front (direct agent-to-service calls outside wallfacer's network path);
  the env-injection sections below apply only to that fallback.
- **Where to store the minted agent token** (env-injection variant only),
  in-memory on the running task versus persisted to
  `store.ExecutionEnvironment`. In-memory keeps the secret out of disk;
  persisted helps debugging. Default: in-memory, log only `delegation_id`.
- **Handling tasks dispatched by the built-in API key**, no user principal,
  no subject token (and no `act.sub`). Either skip agent-token provisioning
  (agents can't call latere.ai services) or mint a service-principal token.
  Out of scope for v1; documented as a limitation. Note the shipped proxy's
  `delegatorSub` already falls back to `claims.Sub` when `act` is absent.
