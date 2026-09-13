# Service identity

Wallfacer is one member of the latere.ai service family, and this note describes its slice: how a cloud sandbox proves who it is when it calls wallfacer's trust plane, and where wallfacer sits in the family audience scheme that keeps one product's tokens out of another.

The mechanics of the proxy itself (credential substitution, streaming, the 503-until-configured local state) live in [Auth & Identity](auth-and-identity.md). This note covers the identity contract on the edge: the audience wallfacer enforces, the fail-closed posture, and the invariant that ties it to the rest of the family.

## The owner is constant

The family holds one invariant across every service boundary:

> Authority always derives from the owning user (`org_id`, `sub`); a product's own tokens never cross into another product, because every hop presents a token auth issued for the audience being called.

In wallfacer's terms:

- **Authority derives from the owner.** A sandbox JWT presented to wallfacer names the owning user through its `sub`. Wallfacer resolves the owning principal from that claim before it acts (for git, that principal is what auth uses to pick the right installation).
- **The owner is who the token says.** Wallfacer reads the presented token and does not manufacture authority of its own on the inbound path.
- **A product's tokens stay with their issuer.** Wallfacer's own outbound service token targets auth, not a third product. When wallfacer mints a GitHub installation token, it calls auth's installation-token endpoint with its service token and hands back only the scoped result.

## Inbound contract: the sandbox-proxy audience

Every inbound request to the trust-plane routes must carry a service JWT whose audience is:

```
aud = wallfacer-sandbox-proxy
```

A request that validates but is addressed to a different audience is rejected with `403`. On top of the audience, each route requires a per-route scope on the same token:

| Route | Required scope |
|---|---|
| `/internal/sandbox-proxy/llm/anthropic/...` (inference endpoints only) | `llm:proxy` |
| `/internal/sandbox-proxy/llm/openai/...` (inference endpoints only) | `llm:proxy` |
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

The scheme itself, along with how these service JWTs are issued, is defined by the auth service. See the auth service's integration guide for the JWKS endpoint and the family audience scheme. For how a sandbox reaches models through Lux, see the Lux document "Authenticating to Lux".

## Current boundary

The shipped surface is the inbound trust plane described above: the sandbox-proxy audience, its per-route scopes, and the fail-closed validator. Nothing mints the sandbox JWT that plane validates: the proxy validates whatever the sidecar already holds against the contract on this page. Where that token comes from when a cloud executor ships is an open question, and the answer will be a token auth issues for the dispatching user, since auth issues no credential that stands for one.
