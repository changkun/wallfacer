# Changelog

Every tag has a section here, and the section is the body of the GitHub
release. A tag without one is refused at the pre-push and fails the release
workflow. Write under `Unreleased` as work lands; `lateregate release vX.Y.Z`
turns that into the tag's section, commits, tags and pushes.

A section says what changed for whoever uses the release, not what was
committed: the commit log already holds that.

## Unreleased

### Changed

- Access is by role, not the retired flag (identity id-09, rule R9).
  RequireSuperadmin admits the `platform_admin` role in the verified
  principal where it read the installation flag before, and the
  sandbox-proxy reads its required scope from the token's `scp` claim into a
  local slice rather than off the family Identity, which no longer carries a
  scope. `GET /api/me` marshals the family principal, which now carries the
  `roles` claim instead of `is_superadmin`; the console derives the account
  role (`platform_admin`) from it. Pins pkg v0.64.0. Requires an issuer
  that mints `platform_admin` (identity auth release); a token minted
  before it refreshes within 15 minutes.

## v0.3.0 - 2026-09-13

### Removed

- The sandbox proxy's `GET /internal/sandbox-proxy/github-token` route, the
  service token it presented to auth, and `SANDBOX_PROXY_AUTH_INSTALLATION_URL`,
  `SANDBOX_PROXY_AUTH_URL`, `SANDBOX_PROXY_CLIENT_ID` and
  `SANDBOX_PROXY_CLIENT_SECRET`. auth v0.25.0 removed the installation-token
  brokering the route called, so it had nothing left to call. The proxy is
  enabled by a provider key alone and serves the two LLM routes.

## v0.2.0 - 2026-09-13

### Changed

- The sandbox proxy presents wallfacer's own service token at the issuer's
  installation-token endpoint, minted with the client_credentials grant
  from `SANDBOX_PROXY_CLIENT_ID` and `SANDBOX_PROXY_CLIENT_SECRET` against
  `SANDBOX_PROXY_AUTH_URL` and re-minted before expiry.
  `SANDBOX_PROXY_AUTH_SERVICE_TOKEN`, a long-lived static JWT, is gone.
- The runner reads a token's expiry through the shared `jwt.DecodePayload`
  instead of splitting and decoding it by hand, and the verifier runs the
  family's `authkit/conformance` suite in the tests of internal/auth.

## v0.1.0 - 2026-09-13

- Signing in requests no audience any more: the session token belongs to
  the identity provider, and the coordination connector presents a
  five-minute actor token minted for wallfacer instead of the login token.
  `AUTH_AUDIENCE` now names only what the API verifies. Anyone signed in
  before this release signs in once more.
