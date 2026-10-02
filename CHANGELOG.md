# Changelog

Every tag has a section here, and the section is the body of the GitHub
release. A tag without one is refused at the pre-push and fails the release
workflow. Write under `Unreleased` as work lands; `lateregate release vX.Y.Z`
turns that into the tag's section, commits, tags and pushes.

A section says what changed for whoever uses the release, not what was
committed: the commit log already holds that.

## Unreleased

### Changed

- gRPC-Go v1.83.2, past GO-2026-6443 (GHSA-2v4p-qf9q-27wj), in which a gRPC
  xDS server crashes on a request that carries neither an `:authority` nor a
  Host header; Wallfacer runs no gRPC server and the affected code is not
  reachable from it, so earlier releases were not exposed.

- OpenTelemetry Go v1.46.0, with the log modules at v0.22.0, the slog bridge at
  v0.20.1 and otelhttp at v0.71.0, past GO-2026-6615 and GO-2026-6505, and
  `latere.ai/x/pkg` v0.90.2. pkg v0.90.2 names the service resource with
  semantic conventions v1.43.0, the schema of this SDK; with an older schema
  the two conflict when merged and the server disables telemetry export at start.
  `latere.ai/x/topos` is at v0.7.0, the last release with the adversarial
  review and graph packages Wallfacer uses; the earlier pin does not build
  against pkg v0.90.2.

- The shared footer and product switcher follow latere-ui v1.30.1. The
  product switcher sends Lux to the platform console's Models section and no
  longer offers Drive. The footer puts the lockup, the social profiles and
  icon menus for theme and language beside four link columns (Applications
  with Research, Platform, Company, Legal) under semibold headings in the
  full text tone, and links the platform console in place of the separate
  Topos, Cella, Lux and Drive entries. Wallfacer's own footer rules, which
  overrode the shared layout, are removed.

### Removed

- The GitHub integration: Settings has no GitHub tab, tasks and specs show no
  pull request, the `/api/github/*` and `/api/tasks/{id}/pr` routes and the
  `github` field of `GET /api/config` are gone, and the first start deletes
  the GitHub token cache in `~/.wallfacer/github/`; a branch is still pushed
  with plain `git` and the host's own credentials, and its pull request is
  opened on the git host.

- User-authored agents and fleets: the Agents page, the agent graph picker in
  the task composer and on the Routines page, and the `/api/agents` and
  `/api/flows` routes are gone, and every task and routine runs the built-in
  pipeline; files under `~/.wallfacer/agents/` and `~/.wallfacer/flows/` stay
  on disk unread, with one warning at startup when either directory holds a
  file, and a stored task that names a fleet notes on its timeline that the
  fleet no longer exists.

### Fixed

- A second Wallfacer instance on one machine, which starts on a free port when
  its own is taken, no longer sends the browser to a sign-in that returns to
  the first instance: sign-in by browser redirect and organization switching
  are off there, the account menu says why, and device-code sign-in works as
  before.

- A signed-out browser on the machine Wallfacer runs on reaches sign-in by
  browser redirect, where `/login` and `/callback` answered
  `401 {"error":"unauthorized"}` because a page navigation cannot present the
  server key; a browser on another host still needs the key there, and
  signs in with a device code.

- Switching organization on a local instance ends the session of the previous
  organization before the browser leaves for sign-in, as it does on a hosted
  one, so a switch abandoned at the sign-in page no longer leaves the account
  menu in the old organization.

- A review transcript that cannot be read to its end is marked incomplete in
  the task's verification panel, and the server logs the reason, where the
  rounds before the unreadable part were shown as the whole debate.

- A task on the `topos` harness, or on a delegating fleet, is refused before it
  starts when no model credential is configured: the task fails with the
  `model_credential_missing` category and one sentence that names
  `ANTHROPIC_API_KEY` and where to set it, no worktree is created, nothing runs
  or is committed, and the harness is reported unusable until a key is set.

- A task pinned to the `topos` harness gets a generated title and an oversight
  summary, which run in-process like its commit message, and its test run uses
  the configured default subprocess harness; each of these failed before with
  "unsupported agent".

- A chat round is recorded in the usage log under the harness that ran it, not
  always under Claude; rounds on a harness other than Claude are recorded with
  zero tokens and zero cost, because their usage is not read yet.

- A client on another host needs the server key to reach the API of a local
  instance whatever account it is signed in with; a sign-in stands in for the
  key only on the machine Wallfacer runs on, and on a cloud-mode deployment.

- The chat harness picker no longer offers the `topos` harness, which runs
  tasks but has no chat runtime, and a chat message sent with it through the
  API is refused with `harness_unavailable_in_chat` instead of failing after
  it was accepted with "unsupported agent".

- A chat thread gets a generated title when the default harness is `topos`:
  the title runs in-process, as a `topos` task's title does, where it failed
  before with "unsupported agent".

- A request to a path under `/api` that no endpoint serves answers 404 with
  the JSON error envelope, `not_found`, for every method, where a `GET` got
  the web app's page with 200 and other methods 405; a path an endpoint serves
  under other methods answers 405 `method_not_allowed`, and a page of the web
  app still loads when its address is opened directly.

- `POST /api/admin/rebuild-index` works on a local instance with the server
  key, where it answered 401 unless the caller was signed in as a platform
  administrator; a cloud-mode deployment still requires that role.

- A cloud-mode deployment refuses the terminal with 403
  `terminal_unavailable` and reports it off in `GET /api/config`, whatever
  `WALLFACER_TERMINAL_ENABLED` says, where it would have opened a shell on the
  server host for any signed-in account; a local instance is unchanged.

## v0.6.1 - 2026-09-26

### Fixed

- An agent-graph run through Lux reaches a gateway served under a base path.
  The model leg reduced `ANTHROPIC_BASE_URL` to its origin, so
  `https://api.latere.ai/v1/models/anthropic`, the Lux core's Anthropic door,
  sent the lux-native call to `https://api.latere.ai/lux/v1/generate`, where
  nothing answers. It now drops only the trailing `/anthropic` segment and
  keeps the base path. A gateway at the root of its host is unaffected.

- A spec comment's anchor is written as text, which is correct on any database
  connection. A json value handed to Postgres as bytes is sent as a binary
  string and arrives as a hex literal, which a json column refuses whenever
  parameters travel in the text format. Connections that ask the server to
  describe a statement first, which is how the coordination plane connects
  today, accept it, so nothing was failing in production. The build refuses a
  json value bound as bytes, so this cannot return unnoticed.

## v0.6.0 - 2026-09-19

### Changed

- The coordination plane serves through the family's database pooler.
  Serving traffic opens `WALLFACER_DATABASE_POOL_URL` when the deployment
  carries it and falls back to `WALLFACER_DATABASE_URL`, so a deployment
  whose secret predates the pool starts unchanged. Migrations keep the direct
  endpoint, because they hold a lock across statements that a transaction
  pooler cannot keep on one connection. The pool's own size, and no longer
  the replica count, is the plane's claim on the shared database.

## v0.5.0 - 2026-09-18

### Fixed

- A workspace that is a git repository keeps its history when wallfacer writes
  a task's work back to it on a machine without `rsync`, such as Windows.
  Without rsync the write-back deleted the workspace's `.git` directory right
  after copying the files in, so the repository lost its commits, branches and
  remotes. The write-back now leaves everything named `.git` in the workspace
  untouched and copies none out of the snapshot, at any depth, matching what
  the rsync path always did.

### Changed

- A release is cut only from a green build. The release command reads CI
  before it runs the quality bar and refuses while the repository is red,
  so a version whose build failed, or whose tag published no notes, does
  not reach you as a release. The same bump puts this deployment's
  audience back under check: the gate reads `AUTH_AUDIENCE` off the
  deployment again, which it had stopped doing when the command moved to
  the module root, so the audience wallfacer verifies is proved to be the
  one it runs with.

- The identity gate now also reads the frontend for the retired admin flag.
  It read the Go, the documents and the deploy manifests, so a page that
  still branched on the flag passed while the Go beside it was clean; the
  flag is now looked for in the repository's `.ts`, `.tsx`, `.jsx`, `.vue`,
  `.svelte`, `.js`, `.mjs` and `.cjs` too. Nothing changes for a user of
  wallfacer.

## v0.4.0 - 2026-09-14

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
