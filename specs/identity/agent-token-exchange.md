---
title: Agent Token Exchange
status: drafted
depends_on:
  - specs/identity/authentication.md
affects:
  - internal/handler/
  - internal/runner/
  - internal/auth/
  - internal/store/
effort: medium
created: 2026-04-19
updated: 2026-07-17
author: changkun
dispatched_task_id: null
---

# Agent Token Exchange

> **Reopened 2026-07-17 (drafted).** The reopening condition is met: the
> identity-fabric epic (`latere-ai/specs products/identity-fabric.md`)
> provides the backend delegation chain. Per-task agent credentials come from
> the registered-agent delegation chain (auth agent principal + delegation row
> + `POST /internal/agent-runner-tokens`; see
> `latere-ai/specs products/identity-fabric/if-05-agent-principal-registration.md`),
> not from exchanging the dispatching user's session token as the parked
> design assumed. Sections marked **historical** below record that parked
> design and are superseded on the credential-source question. This spec
> stays drafted until a cloud executor consumes it; the convergence scope is
> `latere-ai/specs products/identity-fabric/if-09-wallfacer-convergence.md`.

## Problem

Wallfacer launches AI agents as host processes in per-task git worktrees
(the container runtime was removed; see Reality below). As latere.ai grows
out additional services (fs.latere.ai for file storage, telemetry ingestion,
etc.), those agents need credentials to call those services **on behalf of**
the user who dispatched the task, not with the user's own refresh-capable
JWT, and not as anonymous clients.

The credential source is the registered-agent delegation chain (if-05): the
agent is registered as an auth principal, the owning user's consent is a
delegation row, and a per-task token is minted via
`POST /internal/agent-runner-tokens` (RFC 8693 exchange underneath). The
minted token is short-lived (up to 900s, clamped to the delegation) and
carries `sub` = agent with `act.sub`/`grantor_id` = the owning user; revoking
the delegation row kills the chain. That token reaches the agent either
injected into the agent process environment or held by wallfacer and applied
per request through the shipped sandbox proxy (see Reality and the resolved
open decision: extend the proxy).

Historical premise (superseded): the parked design minted the `agent_token`
by exchanging the dispatching user's session token as the `subject_token`.
Sections below that describe minting from the user session are kept as the
record of that design.

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
- Wire `POST {AUTH}/internal/agent-runner-tokens` into wallfacer's runner so
  it can mint a per-task delegated token for the task's registered agent
  principal (historical variant: exchanging the dispatching user's session
  token via `POST auth.latere.ai/token`, grant
  `urn:ietf:params:oauth:grant-type:token-exchange`).
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
- Agent-principal registration in the auth service (owned by if-05: the
  product that creates the agent registers it via `POST {AUTH}/agents` and
  creates the delegation row; wallfacer consumes existing delegations).
- Per-service authorization policy (fs.latere.ai decides what an
  `agent_token` with scope X can do, not wallfacer).
- User-facing delegation UI (view / revoke active agent sessions).
- `internal/oauth/` (the PKCE OAuth flow for agent *credential* login like
  Claude/Codex, entirely separate from RFC 8693 token exchange).

## Family audience scheme (if-04)

The audience and scope names on wallfacer's trust-plane edges are
wallfacer's entries in the family audience scheme defined in the auth
repo's `specs/delegation-claims-contract.md` (identity-fabric if-04):

- `wallfacer-sandbox-proxy`: the audience `requireClaims` enforces on
  inbound sidecar JWTs.
- `llm:proxy` and `github:token`: the per-route scopes those JWTs must
  carry.
- `github:mint-token`: the scope on wallfacer's outbound service JWT
  toward auth's installation-token endpoint.

Consumers enforce their declared audience per that contract. This is
naming alignment only; the values above are already live and no config
change is implied.

## JWT Claim Reference

From `identity/authentication.md`'s claim table, the agent-specific fields are
(all live in `latere.ai/x/pkg/jwtauth` and consumed by `sandbox_proxy.go`):

| Claim | Go field | Description |
|-------|----------|-------------|
| `validation` | `Claims.Validation` | `"local"` (JWKS-only) or `"strict"` (tokeninfo call required for writes) |
| `delegation_id` | `Claims.DelegationID` | Delegation record UUID, for audit + revocation |
| `act.sub` | `Claims.Act.Sub` | Delegator's principal ID (the user who dispatched the task) |

## Design Sketch (historical)

Env-injection variant (the parked sketch; superseded on the credential
source, which is now the runner-token mint from the registered agent's
delegation, not an exchange of the user's session JWT):

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

- Agent principal registration (if-05): the task's agent must exist as an
  auth principal with an active delegation row from the dispatching user
  before wallfacer can mint runner tokens for it.
- `identity/authentication.md` Phase 2, this spec needs a real user
  `*jwtauth.Claims` on the task creator so the delegation can be attributed.
- A cloud executor that consumes the minted credential; until one exists
  this spec stays drafted (if-09 convergence scope).

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
