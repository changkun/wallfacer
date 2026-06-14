---
title: Third-party OIDC providers for self-hosted deployments
status: vague
depends_on:
  - specs/identity/authentication.md
affects:
  - internal/auth/
  - internal/handler/login.go
  - internal/envconfig/
effort: large
created: 2026-04-19
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Third-Party OIDC

## Problem

Wallfacer's login flow is composed from the platform package
`latere.ai/x/pkg/oidc` (re-exported through `internal/auth`). That RP
is wired for latere.ai: the session and flow cookie names
(`__Host-latere-session`, `__Host-latere-flow`), the `/userinfo`
shape, and the claim layout are all tied to `auth.latere.ai`.
Self-hosted wallfacer deployments that want real login against a
non-latere.ai identity provider (Keycloak, Entra ID, Okta, Authelia,
Dex, Google Workspace, GitHub Enterprise) have to keep using
`WALLFACER_SERVER_API_KEY`, a single static bearer token, because
nothing else works today.

## What this spec does

Make any RFC-6749 / OIDC-core provider able to terminate wallfacer's
login flow, without wallfacer owning a parallel OIDC implementation.
The platform `pkg/oidc` RP stays the only relying party; the work is
to configure it (and, where it falls short, extend it upstream) so it
can point at a non-latere.ai issuer. Latere.ai mode
(`WALLFACER_CLOUD=true`, `AUTH_URL=https://auth.latere.ai`) stays the
default.

## Design space

Wallfacer no longer vendors an OIDC package; `pkg/oidc`,
`pkg/authkit`, and `pkg/jwtauth` are platform-owned
(`latere.ai/x/pkg`). So the design is not "fork a local package", it is
"how far does configuring the existing platform RP get us, and what has
to move upstream". The shape to weigh during the drafted to validated
transition:

1. **Configure the platform RP at a third-party issuer.** Point
   `AUTH_URL` / `AUTH_JWKS_URL` / `AUTH_ISSUER` at the foreign provider
   and let `oidc.New` plus the `authkit` authenticators
   (`NewJWT`, `NewSessionAuthenticator`, composed in
   `internal/auth/middleware.go`) do the rest. This works for any
   provider whose discovery document, claim names, and cookie semantics
   already match what the platform RP expects. Cheapest path; bounded by
   whatever the platform RP hard-codes.
2. **Extend the platform RP upstream for the gaps.** Where the foreign
   provider diverges (claim mapping, cookie naming, optional `org_id`),
   the change lands in `latere.ai/x/pkg/oidc` so every consumer
   benefits, and wallfacer only consumes the new configuration surface.
   Wallfacer-local code stays a thin handler layer
   (`internal/handler/login.go`, the `AuthProvider` interface) plus env
   plumbing.

The real gaps either path has to close, wherever the config surface
ends up living:

- A claim mapping from provider-native claims to wallfacer's internal
  principal (`sub`, `email`, `name`, `picture`, `org_id`, `scp`,
  `is_superadmin`). Some providers don't surface `org_id`; deployments
  that need org scoping will have to map a group / role claim instead.
- Cookie naming. The session and flow cookie names
  (`__Host-latere-session`, `__Host-latere-flow`) and the `__Host-`
  prefix handling are owned by the platform `pkg/oidc`, so any
  per-deployment renaming (to keep multiple deployments behind a shared
  domain from colliding) is an upstream concern, not a wallfacer-local
  knob.
- JWKS URL + issuer configuration (already landed for latere.ai: see
  `auth.BuildValidator`, `AUTH_JWKS_URL`, `AUTH_ISSUER`).
- Discovery document support (`.well-known/openid-configuration`) so
  most providers configure with a single URL.

## Out of scope

- SAML. OIDC-only here; SAML is a separate spec if ever needed.
- Social login (raw OAuth2 against GitHub / Google without OIDC).
  Providers that speak OIDC are covered; bare OAuth2 is not.
- Reshaping the latere.ai-hosted flavour. `auth.latere.ai` keeps the
  same platform RP, its cookie semantics, and its front-channel logout
  iframe.
- Third-party provider *registration* (client ID / secret rotation,
  redirect-URI management). Operators handle that in their identity
  provider's admin UI before pointing wallfacer at it.

## Deployment posture

Until this ships, self-hosted non-latere.ai deployments continue to
use `WALLFACER_SERVER_API_KEY` as a single shared bearer token.
`WALLFACER_CLOUD=false` remains the unauthenticated local mode. Once
this ships, a third combination opens up:

| `WALLFACER_CLOUD` | `AUTH_URL` | Behavior |
|-------------------|------------|----------|
| `false` | (unset) | Local / API key path (unchanged) |
| `true` | `https://auth.latere.ai` | latere.ai flow (default) |
| `true` | any OIDC issuer | Third-party OIDC flow |

## What this spec does NOT answer

- How much of the gap is configuration on the existing platform RP vs.
  an upstream change to `latere.ai/x/pkg/oidc`. Needs a prototype round
  against at least one real provider (likely Authelia or Dex for local
  dev) before committing.
- How to surface the claim-mapping config: env vars vs. a YAML file
  under `~/.wallfacer/` vs. a UI screen.
- Whether per-provider refresh semantics differ enough to require
  provider-specific refresh code.

## Dependencies

- Authentication Phase 2 is complete: JWT middleware, principal
  context (`auth.Identity`, resolved from a Bearer JWT or the session
  cookie), org-scoped data, and `WALLFACER_CLOUD` are all in place.
- No cloud-track spec depends on this; cloud always uses latere.ai.
- Unblocks: credible self-hosting story for non-latere.ai operators;
  org-scoped multi-user deployments outside latere.ai infra.
