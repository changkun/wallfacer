---
title: Agent Token Exchange
status: drafted
depends_on:
  - specs/shared/authentication.md
affects:
  - internal/runner/
  - internal/auth/
  - internal/store/
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Agent Token Exchange

## Problem

Wallfacer launches AI agents inside sandbox containers. As latere.ai grows
out additional services (fs.latere.ai for file storage, telemetry ingestion,
etc.), those agents need credentials to call those services **on behalf of**
the user who dispatched the task, not with the user's own refresh-capable
JWT, and not as anonymous clients.

RFC 8693 token exchange fits: the user's access token is the `subject_token`,
the auth service mints a scoped `agent_token` tied to a delegation record,
and that short-lived token travels into the container.

This spec was pulled out of `shared/authentication.md` because it is
**orthogonal** to user login / auth / tenant isolation. It unblocks nothing
on the cloud move; it is consumed only when we start calling latere.ai
backend services *from inside task containers*.

## Scope

In scope:
- Wire `POST auth.latere.ai/token` (grant `urn:ietf:params:oauth:grant-type:token-exchange`) into wallfacer's runner.
- Mint an `agent_token` per task execution, scoped to `validation="local"` for
  read-only agent work and `validation="strict"` for agents that perform
  writes against latere.ai services.
- Inject the token into the container at launch time via an env var that the
  sandbox images already read (or introduce one, e.g. `LATERE_AI_TOKEN`).
- Handle 15-minute TTL: re-mint on turn boundary if the task outlives the
  token, or surface a recoverable `token_expired` failure that auto-retry
  can replay.
- Record the `delegation_id` on the task's execution environment so audit
  logs can trace an agent action back to the delegating user.

Out of scope:
- Agent-principal registration in the auth service (handled by the auth
  service admin API; wallfacer assumes delegations are pre-provisioned or
  created implicitly by the exchange call).
- Per-service authorization policy (fs.latere.ai decides what an
  `agent_token` with scope X can do, not wallfacer).
- User-facing delegation UI (view / revoke active agent sessions).
- Migration of `internal/oauth/` (that handles sandbox *credential* OAuth
  like Claude/Codex API keys, entirely different system).

## JWT Claim Reference

From `shared/authentication.md`'s claim table, the agent-specific fields are:

| Claim | Description |
|-------|-------------|
| `validation` | `"local"` (JWKS-only) or `"strict"` (tokeninfo call required for writes) |
| `delegation_id` | Delegation record UUID, for audit + revocation |
| `act.sub` | Delegator's principal ID (the user who dispatched the task) |

## Design Sketch

```
┌────────────────────────────────────┐
│ runner starts task for user U      │
│ ├─ load U's access token (session) │
│ ├─ POST auth.latere.ai/token       │
│ │   grant_type=token-exchange      │
│ │   subject_token=<user JWT>       │
│ │   agent_id=<task UUID>           │
│ └─ receive agent_token (15m TTL)   │
│                                    │
│ launch container with              │
│   LATERE_AI_TOKEN=<agent_token>    │
│   LATERE_AI_DELEGATION=<uuid>      │
└────────────────────────────────────┘
```

### Refresh strategy

Agent tokens have no refresh capability (per the auth service's design).
Options:
1. **Re-mint on turn boundary**, before each sandbox agent launch in a
   multi-turn task, re-exchange the user's (refreshed) session token.
   Simple, adds one auth call per turn (~20 ms).
2. **Fail-fast + auto-retry**, let the token expire mid-turn; when the
   container hits a 401 from fs.latere.ai, bubble up `token_expired` as a
   recoverable failure category and let the existing retry engine re-mint
   on the next run. Cheaper, but the mid-turn failure is user-visible.

Prefer (1) for quality; fall back to (2) if the per-turn call cost is
measurable in the trace spans.

## Integration Points

- `internal/auth/`, add `ExchangeForAgentToken(ctx, userToken, agentID, scopes) (agentToken, delegationID, error)`.
- `internal/runner/`, call the exchange before `ContainerSpec.Build()`;
  inject env vars into the launch command.
- `internal/store/`, add `AgentDelegationID string` to task execution
  environment so the trace is persisted.
- Sandbox image contract, document `LATERE_AI_TOKEN` + `LATERE_AI_DELEGATION`
  as the env vars agents should read when calling latere.ai services.

## Dependencies

- `shared/authentication.md` Phase 2 must be complete, this spec needs a
  real user `*jwtauth.Claims` on the task creator and a populated session
  access token on the request path.
- The auth service must expose the `token-exchange` grant and accept an
  `agent_id` parameter (currently documented but not yet end-to-end tested
  from wallfacer's side).

## Deferred Decisions

- **Where to store the minted agent token**, in-memory on the running task
  versus persisted to `store.Task.ExecutionEnv`. In-memory keeps the secret
  out of disk; persisted helps debugging. Default: in-memory, log only
  `delegation_id`.
- **Handling tasks dispatched by the built-in API key**, no user principal,
  no subject token. Either skip agent-token provisioning (agents can't call
  latere.ai services) or mint a service-principal token. Out of scope for
  v1; documented as a limitation.
