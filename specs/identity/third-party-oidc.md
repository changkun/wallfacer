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
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Third-Party OIDC

## Problem

The `pkg/oidc` package is latere.ai-specific: cookie names
(`__Host-latere-session`, `__Host-latere-flow`), the `/userinfo` shape,
and the claim layout are all tied to `auth.latere.ai`. Self-hosted
wallfacer deployments that want real login against a non-latere.ai
identity provider (Keycloak, Entra ID, Okta, Authelia, Dex, Google
Workspace, GitHub Enterprise) have to keep using
`WALLFACER_SERVER_API_KEY` — a single static bearer token — because
nothing else works.

## What this spec does

Add pluggable OIDC so any RFC-6749 / OIDC-core provider can terminate
wallfacer's login flow. Keeps `pkg/oidc` as the default when
`WALLFACER_CLOUD=true` (latere.ai mode), adds a generic code path when
pointed at another provider.

## Design space

Two shapes worth weighing during the drafted → validated transition:

1. **Pluggable RP behind the existing `AuthProvider` interface.** A
   second implementation of the `pkg/oidc` surface area parameterised
   by discovery document, cookie names, and a claim mapping. Minimal
   change to `internal/handler/login.go`. Cost: `pkg/oidc` becomes two
   providers inside one package, or a new `pkg/oidc-generic` sibling.
2. **Second, parallel code path.** `internal/auth/oidc_generic.go`
   sits next to the latere.ai path; the handler picks which to wire at
   startup based on env. Faster to land, easier to reason about, less
   DRY. Cost: two flows to maintain.

Either path needs:

- A claim mapping from provider-native claims to wallfacer's internal
  principal (`sub`, `email`, `name`, `picture`, `org_id`, `scp`,
  `is_superadmin`). Some providers don't surface `org_id`; deployments
  that need org scoping will have to map a group / role claim instead.
- Cookie name + prefix configuration so multiple deployments behind a
  shared domain don't collide.
- JWKS URL + issuer configuration (already landed for latere.ai in
  Phase 2).
- Discovery document support (`.well-known/openid-configuration`) so
  most providers configure with a single URL.

## Out of scope

- SAML. OIDC-only here; SAML is a separate spec if ever needed.
- Social login (raw OAuth2 against GitHub / Google without OIDC).
  Providers that speak OIDC are covered; bare OAuth2 is not.
- Switching the latere.ai-hosted flavour to the generic code path.
  `auth.latere.ai` stays on `pkg/oidc` for its cookie semantics and
  front-channel logout iframe.
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
| `false` | — | Local / API key path (unchanged) |
| `true` | `https://auth.latere.ai` | Phase 1+2 latere.ai flow |
| `true` | any OIDC issuer | Generic OIDC flow |

## What this spec does NOT answer

- Which of the two designs wins. Needs a prototype round against at
  least one real provider (likely Authelia or Dex for local dev) before
  committing.
- How to surface the claim-mapping config — env vars vs. a YAML file
  under `~/.wallfacer/` vs. a UI screen.
- Whether per-provider refresh semantics differ enough to require
  provider-specific refresh code.

## Dependencies

- Authentication Phase 2 is complete: JWT middleware, principal
  context, org-scoped data, and `WALLFACER_CLOUD` are all in place.
- No cloud-track spec depends on this — cloud always uses latere.ai.
- Unblocks: credible self-hosting story for non-latere.ai operators;
  org-scoped multi-user deployments outside latere.ai infra.
