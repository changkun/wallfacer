# Service identity

Wallfacer is one member of the latere.ai identity fabric, and this note describes its slice: how a cloud sandbox proves who it is when it calls wallfacer's trust plane, and where wallfacer sits in the family audience scheme that keeps one product's tokens out of another.

The mechanics of the proxy itself (credential substitution, streaming, the 503-until-configured local state) live in [Auth & Identity](auth-and-identity.md). This note covers the identity contract on the edge: the audience wallfacer enforces, the fail-closed posture, and the invariant that ties it to the rest of the fabric.

## The owner is constant

The fabric holds one invariant across every service boundary:

> Authority always derives from the owning user (`org_id`, `sub`); the acting identity changes at each boundary but the owner is constant; a product's own tokens never cross into another product (cross-product hops carry auth-issued delegated tokens).

In wallfacer's terms:

- **Authority derives from the owner.** A sandbox JWT presented to wallfacer names the owning user through its `sub`, and through `act.sub` when the call is delegated on that user's behalf. Wallfacer resolves the owning principal from those claims before it acts (for git, that principal is what auth uses to pick the right installation).
- **The acting identity changes, the owner does not.** The sidecar is the actor at wallfacer's edge; the owning user stays the authority behind it. Wallfacer does not manufacture authority of its own on the inbound path.
- **A product's tokens stay with their issuer.** Wallfacer's own outbound service token targets auth, not a third product. When wallfacer mints a GitHub installation token, it calls auth's installation-token endpoint with its service token and hands back only the scoped result.

## Inbound contract: the sandbox-proxy audience

Every inbound request to the trust-plane routes must carry a service JWT whose audience is:

```
aud = wallfacer-sandbox-proxy
```

A request that validates but is addressed to a different audience is rejected with `403`. On top of the audience, each route requires a per-route scope on the same token:

| Route | Required scope |
|---|---|
| `POST /internal/sandbox-proxy/llm/anthropic/...` | `llm:proxy` |
| `POST /internal/sandbox-proxy/llm/openai/...` | `llm:proxy` |
| `GET /internal/sandbox-proxy/github-token?repo=owner/name` | `github:token` |

A token that clears the audience but lacks the route's scope is rejected with `403`. Missing or unparseable bearer tokens are `401`.

Do not confuse this inbound `github:token` scope with `github:mint-token`. The latter is the scope on wallfacer's *outbound* service token (`SANDBOX_PROXY_AUTH_SERVICE_TOKEN`) that it presents to auth when minting an installation token. They sit on opposite edges of the proxy.

## Fail closed

The validator is fail closed. If the JWT validator is not configured, the trust plane cannot establish who is calling, so it rejects the request with `503` rather than admitting it as anonymous-but-authorized. An unconfigured validator never becomes an open door.

This is the floor beneath the whole edge: the proxy only forwards to a real upstream (an LLM provider, or auth's installation-token endpoint) once a caller has proven identity against the declared audience and carries the route's scope. Absent the configuration to check that, nothing is forwarded.

The trust-plane routes are also gated on the proxy being enabled at all: without the upstream credentials wired (`SANDBOX_PROXY_AUTH_INSTALLATION_URL`, `SANDBOX_PROXY_AUTH_SERVICE_TOKEN`, and at least one provider key) the routes answer `503` before any JWT check, which is the permanent state of a local, single-user run.

## The family audience scheme

`wallfacer-sandbox-proxy` is wallfacer's slot in the family audience naming shared across the latere.ai services. Audiences are per family member: a token minted for one member's audience is not valid at another. A sandbox JWT scoped to `wallfacer-sandbox-proxy` is meaningful only at wallfacer's trust plane, and a token addressed to a sibling service's audience is rejected here on the audience check.

That per-member separation is what makes the invariant enforceable at wallfacer's edge. The owning user is the constant authority, but a credential minted for a different destination cannot be replayed against wallfacer, because its audience does not match.

The scheme itself, along with how these service JWTs are issued and delegated, is defined by the auth service. See the auth document "Identity, delegation, and token exchange" for the JWKS endpoint and the family audience scheme. For how a sandbox reaches models through Lux, see the Lux document "Authenticating to Lux".

## Current boundary

The shipped surface is the inbound trust plane described above: the sandbox-proxy audience, its per-route scopes, and the fail-closed validator. Cloud-executor adoption of the fabric, where an executor mints per-task delegated credentials for a registered agent principal, is a separate upcoming feature and is not part of the shipped trust plane. Until an executor consumes it, the proxy validates whatever sandbox JWT the sidecar already holds against the contract on this page.
