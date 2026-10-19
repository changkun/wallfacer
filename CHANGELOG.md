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
  `latere.ai/x/pkg` v0.90.2. Wallfacer exports no telemetry; these modules
  come in only as dependencies of `latere.ai/x/pkg`.

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

- The Artifacts gallery: the console has no Artifacts page, and the
  `/api/artifacts` and `/artifact/{path...}` routes are gone; files under a
  workspace's `artifacts/` directory stay where they are.

- Routines: the console has no Routines page, routine card controls or
  Schedule toggle in the composer, and the `/api/routines` routes are gone. A
  saved routine card stays on disk unread, with one warning at startup naming
  how many there are; tasks a routine already spawned stay on the board as
  ordinary tasks.

- Sign-in to latere.ai and cloud mode: the console has no sign-in button,
  account menu or organization switcher, `wallfacer auth` and the device
  sign-in are gone, and the `/login`, `/callback`, `/logout`, `/api/me` and
  `/api/auth/{device,orgs,me}` routes are gone, as are `WALLFACER_CLOUD` and the
  `AUTH_*` settings. Wallfacer runs on your machine only, and its one
  credential is the per-machine server key. Tasks, events and workspaces no
  longer record who created them: `created_by`, `org_id`, `actor_sub` and
  `actor_type` drop from a record the next time it is saved. Files earlier
  releases wrote for sign-in and coordination (`~/.wallfacer/cookie-key`,
  `coordination-opt-in`, `instance-id`, and the shared `latere/token.json`
  under the user config directory) stay on disk unread.

- Spec comments and the coordination connection to wf.latere.ai: specs show
  no comment layer, the `/api/spec-comments` and `/api/coordination/*` routes
  are gone, and `WALLFACER_COORDINATION`, `WALLFACER_COORDINATION_URL` and the
  `WALLFACER_DATABASE_*` settings are ignored.

- The hosted server: `wallfacer web` is gone, and a release publishes the
  binaries only, with no container image or deployment.

- The Topos harness: the harness pickers no longer offer it, the task trace
  view and `GET /api/tasks/{id}/trace` are gone, and a task or setting that
  still names `topos` runs on the default harness.

- Browser telemetry, the language setting, and the shared footer and product
  switcher, which only the sign-in and product pages used; the console is in
  English.

- Automation and review: the board header has no automation menu, and no task
  moves on its own. The auto-promote, auto-test, auto-submit, catch-up and
  auto-push watchers, automatic retry, the launch circuit breaker, the
  scheduled start, critical-path ordering and review on a second model are
  gone, as are `POST /api/tasks/{id}/review`,
  `GET /api/tasks/{id}/review/transcript`, the `autoimplement`, `autotest`,
  `autosubmit`, `autosync`, `autopush`, `review` and `watcher_health` fields of
  `GET /api/config`, and `WALLFACER_REVIEW_MODEL`, `WALLFACER_REVIEW_ROUNDS`,
  `WALLFACER_REVIEW_COST_CAP`, `WALLFACER_REVIEW_FORKS`, `WALLFACER_AUTO_PUSH`,
  `WALLFACER_AUTO_PUSH_THRESHOLD`, `WALLFACER_MAX_TEST_PARALLEL`,
  `WALLFACER_CONTAINER_CB_THRESHOLD` and `WALLFACER_CONTAINER_CB_OPEN_SECONDS`.
  A task's `scheduled_at`, `review_*`, `auto_retry_*`, `test_fail_count`,
  `pending_test_feedback` and `last_fetch_error` fields, and a workspace's
  automation flags and `max_test_parallel`, drop from a record the next time
  it is saved; a line for a removed setting left in `~/.wallfacer/.env` is
  ignored.

- Whiteboard, Mission Control, Analytics and the terminal: the console has no
  `/whiteboard`, `/mission`, `/map` or `/analytics` page and no terminal dock,
  and `GET` and `PUT /api/whiteboard`, `GET /api/graph`, `GET /api/stats`,
  `GET /api/usage`, `GET /api/debug/spans` and `GET /api/terminal/ws` are
  gone, as are `WALLFACER_TERMINAL_ENABLED`,
  `WALLFACER_AGENT_SESSION_WINDOW_DAYS` and its alias
  `WALLFACER_PLANNING_WINDOW_DAYS`, and the `terminal_enabled` and
  `agent_session_window_days` fields of `GET /api/config`. A task's own usage,
  cost and timeline stay in its detail view, and saved whiteboard scenes stay
  on disk unread.

- The `/chat` page: conversations live in the chat popup, on every page but
  Plan, and in Plan's chat, which opens full width when a workspace has no
  specs.

- Keyboard shortcuts: no global key bindings, shortcuts dialog, letter keys on
  cards or in Plan, or editor tab hotkeys. The command palette opens from the
  rail; in chat, Enter sends and Shift+Enter starts a new line.

- Signing the Claude and Codex CLIs in from Settings: the Harness tab has no
  **Sign in with Claude**, **Sign in with OpenAI** or **Sign in again**
  button, and the `/api/auth/{provider}/start`, `/api/auth/{provider}/status`
  and `/api/auth/{provider}/cancel` routes and the `reauth_available` field of
  `POST /api/env/test` are gone. Agents use the sign-in each CLI keeps on the
  machine, or `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY` and
  `OPENAI_API_KEY` in `~/.wallfacer/.env`, which are unchanged; a token an
  earlier sign-in saved there keeps working.

- Prompt template overrides: the `GET /api/system-prompts` and
  `GET`, `PUT` and `DELETE /api/system-prompts/{name}` routes and the
  `prompts_dir` field of `GET /api/config` are gone, `wallfacer doctor` no
  longer prints a prompts directory, and every agent runs on the templates
  built into the binary. Files under `~/.wallfacer/prompts/` stay on disk
  unread.

- The docs reader in the console: the sidebar has no **Docs** entry, the
  command palette no longer searches the guides, the `/docs` page and the
  `GET /api/docs`, `/api/docs/{slug...}`, `/api/docs-asset/{path...}` and
  `/api/docs-search` routes are gone, and the binary no longer embeds the
  `docs/` tree. The guides stay in the repository's `docs/` directory.

- Metrics and telemetry export: `GET /metrics` and its `wallfacer_*`
  Prometheus series are gone, as is the OpenTelemetry export of traces,
  metrics and logs, so `OTEL_EXPORTER_OTLP_ENDPOINT` and the other `OTEL_*`
  settings are ignored and responses no longer carry an `X-Trace-Id` header.
  Logs still go to the terminal in the format `-log-format` (or `LOG_FORMAT`)
  selects.

- Workspaces with several folders: a workspace is one folder, and
  `POST /api/workspaces` and `PUT /api/workspaces/{id}` refuse more than one
  with 400. A workspace saved with several folders keeps its name and history
  and shows in the picker as needing a folder; it opens once it is pointed at
  one, and its tasks come back. A task stored with worktrees in several
  folders still commits into each, and its timeline names the folder the agent
  works in. Agents no longer see other tasks' worktrees:
  `WALLFACER_SIBLING_WORKTREES_JSON`, the `mount_worktrees` task field (now
  refused with 400) and the `worktree_path` of board entries are gone.
  `WALLFACER_WORKSPACES`, which the server wrote and never read, is ignored,
  and `wallfacer run` refuses folder arguments, which it used to ignore.

- Batch create and task dependencies: the composer makes one task per
  submit, `POST /api/tasks/batch` is gone, and a task has no **Depends on**
  picker, blocked or ready badge, or Blocked by list. A spec's `depends_on`
  stays in the spec tree, and dispatching a spec no longer copies it onto the
  task. `depends_on` on `PATCH /api/tasks/{id}` is refused with 400, and a
  stored task's `depends_on` drops the next time it is saved.

- Title generation: a new task's title is the first line of its prompt,
  without a leading `#`, cut at a word boundary to 60 characters, and a chat
  thread is named the same way from its first message; `POST /api/tasks`
  takes an optional `title` kept as given. `POST /api/tasks/generate-titles`,
  `CLAUDE_TITLE_MODEL`, `CODEX_TITLE_MODEL` and `WALLFACER_SANDBOX_TITLE` are
  gone; existing titles stay as they are. Oversight summaries run on the
  default model, where they used the title model.

- The test run: a waiting task has no **Test** button, verification tab or
  acceptance criteria, and `POST /api/tasks/{id}/test`, the `?phase=test`
  oversight and logs views and `WALLFACER_SANDBOX_TESTING` are gone. `criteria`,
  `custom_pass_patterns` and `custom_fail_patterns` on task create and update
  are refused with 400, and a stored task's test result fields drop the next
  time it is saved. The credential check in Settings (`POST /api/env/test`)
  answers `passed` in place of `last_test_result`.

### Fixed

- A task resumed after its workspace was pointed at another folder runs in
  its own worktree, where it ran in the workspace's folder itself, and a
  rebase conflict in a task's second repository is resolved in that
  repository's worktree.

- A chat round is recorded in the usage log under the harness that ran it, not
  always under Claude; rounds on a harness other than Claude are recorded with
  zero tokens and zero cost, because their usage is not read yet.

- A request to a path under `/api` that no endpoint serves answers 404 with
  the JSON error envelope, `not_found`, for every method, where a `GET` got
  the web app's page with 200 and other methods 405; a path an endpoint serves
  under other methods answers 405 `method_not_allowed`, and a page of the web
  app still loads when its address is opened directly.

- `POST /api/admin/rebuild-index` works on a local instance with the server
  key, where it answered 401 unless the caller was signed in as a platform
  administrator.

- The Events tab of a task shows the sentence of a system event, such as a
  commit phase, a recovery note or the notice that a stored fleet no longer
  exists, where it showed only the event's kind or the word "system".

- `wallfacer run -addr` with an address that has no port, such as
  `-addr 127.0.0.1`, stops at startup with an error naming the address, where
  the server listened on every network interface at a random port.

### Security

- Built with Go 1.27.2 and golang.org/x/net v0.60.0, which fix GO-2026-6611, GO-2026-6612, GO-2026-6613 and GO-2026-6617.

- An agent no longer inherits Wallfacer's own `WALLFACER_*` settings from the
  server's environment or from `~/.wallfacer/.env`, among them
  `WALLFACER_SERVER_API_KEY`, the key to the board's API, so the commands an
  agent runs and the output it prints do not carry it. The runner passes the
  variables an agent needs, such as `WALLFACER_BOARD_JSON`, explicitly;
  provider credentials in the env file still reach the agent.

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
