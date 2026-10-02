---
title: Remove the GitHub Integration
status: archived
depends_on:
  - specs/shared/platform-native.md
affects:
  - internal/github/
  - internal/handler/
  - internal/apicontract/
  - internal/cli/server.go
  - frontend/src/
  - docs/
effort: medium
created: 2026-10-02
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Remove the GitHub Integration

Decision 4 of [platform-native](../../../shared/platform-native.md). A removal, not a
rewrite.

## Goal

Wallfacer makes no call to GitHub and holds no GitHub credential. The token
broker, its cache, the pull-request surface on tasks, and the Settings tab
are deleted. Pushing a branch to a remote stays what it always was: plain
`git` with the user's own credentials.

## Why

- **It belongs to the platform.** A GitHub connection is an account-level
  capability. When the platform has a connector for it, an application
  reaches GitHub through that connector like any other external system.
  Holding a second, application-level path now means maintaining and
  securing something scheduled for replacement.
- **What is built is thin and uneven.** A read of the code on 2026-10-02
  found that creating a pull request cannot succeed without manual git work
  (nothing pushes the task branch), that a merged or closed pull request
  disappears from its task, that connect and disconnect have no caller in the
  interface, and several error-handling defects. The repository picker and
  the PR and issue browser were built and removed within days in July.
- **Removing it retires those defects** instead of fixing code that is going
  away.

## Decisions

1. **Everything GitHub-specific goes**, API and interface together. No route
   is kept "for automation": none has a caller.
2. **Plain git is untouched.** `internal/handler/git.go` (status, push, sync,
   branch operations) and the commit pipeline's optional auto-push use the
   user's git configuration and never used the brokered token.
3. **The identity token file stays.** The session bridge that writes the
   signed-in user's identity token to the shared file serves the coordination
   connector too. Only its GitHub consumer is removed.
4. **Cached tokens are deleted, not abandoned.** See "User data".
5. **Links to wallfacer's own repository stay**: the source link in the site
   navigation, the install and product pages, the About tab, release URLs.
   They are links, not an integration.

## Surface removed

**HTTP**

- `GET /api/github/auth/status`, `POST /api/github/auth/connect`,
  `POST /api/github/auth/disconnect`
- `POST /api/github/pulls`, `POST /api/github/comments`
- `GET /api/tasks/{id}/pr`, `POST /api/tasks/{id}/pr`,
  `POST /api/tasks/{id}/pr/comment`
- The `github` object in `GET /api/config`.

**Backend**

- `internal/github`, whole package: the broker, the provider, the token
  type, the file store, the API client, the read and write helpers.
- `internal/handler/github.go`, `github_auth.go`, `github_write.go`,
  `tasks_pr.go`, their tests, their route registrations and contract entries,
  and the `SetGitHub` / `SetGitHubBroker` wiring on the handler.
- The token store and broker construction in `internal/cli/server.go`.

**Frontend**

- `SettingsTabGithub.vue` and its tab in `SettingsPage.vue`.
- `TaskPrPanel.vue` and where the task sheet mounts it; the pull-request
  state on `TaskCard.vue`.
- `stores/github.ts`, `stores/githubPr.ts`, their tests, the related types in
  `api/types.ts`, and the strings in both language files.
- Any Settings scene in the screenshot checks that asserts the GitHub tab.

**Docs**

- The GitHub sections of `docs/guide/workspaces.md`, `docs/guide/board.md`,
  `docs/guide/configuration.md` and `docs/guide/getting-started.md`.
- The GitHub parts of `docs/internals/auth-and-identity.md`,
  `api-and-transport.md`, `architecture.md`, `internals.md`,
  `service-identity.md` and `development.md`, and the regenerated
  `docs/internals/api-contract.json`.
- Any claim in the root `README.md` that wallfacer opens pull requests.

## User data

| What | After |
|---|---|
| `<config dir>/github/` | Deleted on the first start of the upgraded binary, with one log line. It holds only token cache files that wallfacer wrote, each a short-lived GitHub access token. A credential nothing reads should not stay on disk |
| The GitHub connection on the user's Latere account | Untouched. It is the account's, and wallfacer only ever asked for a token through it |
| Pull requests already opened from a task | Untouched on GitHub. The task no longer shows them |
| Task records | No field to migrate: pull-request state was read live and never stored |

## Removal order

Each step is one commit that builds and passes the Go and frontend suites.

1. **Frontend.** Remove the Settings tab, the panel, the card state, the two
   stores, the types and strings.
2. **HTTP.** Remove the eight routes, their handlers and tests, the contract
   entries, and the `github` object from the config response; regenerate the
   contract file.
3. **Package and wiring.** Delete `internal/github`; remove the construction
   and setters in `internal/cli/server.go` and the handler; add the one-time
   deletion of the cache directory.
4. **Docs and index.** Guide, internals, README, changelog (`### Removed`),
   and the spec bookkeeping below.

## Acceptance

1. `go build ./...` has no reference to `internal/github`, and
   `grep -ri "api.github.com" internal/` finds nothing.
2. Every route listed above answers 404.
3. `GET /api/config` has no `github` key, and the Settings page has no GitHub
   tab.
4. A task sheet shows no pull-request panel; a card shows no pull-request
   state.
5. With files present in `<config dir>/github/`, the first start removes the
   directory and logs it; a second start logs nothing.
6. The coordination connector still signs in and connects (the identity
   token file path is unchanged).
7. Pushing a branch from the git panel still works with the user's own git
   credentials.
8. The Go suite, the frontend suite, `bunx vue-tsc --noEmit` and
   `make ui-test` pass.

## Accepted loss

- Opening a pull request from a task, seeing its state on the card, and
  commenting on it from the task sheet.
- The connection status in Settings.

A user opens the pull request with their own tools after pushing the branch.

## What comes back, and how

Not through this code. When the platform offers a GitHub connector, an agent
or the application reaches GitHub through it, under the platform's
authorization, and wallfacer holds no token. That is specified when the
connector exists.

## Bookkeeping

- [github-integration](../../intent/github-integration.md),
  [pull-request](../../intent/github-integration/pull-request.md) and
  [cloud-remote-fix](../../intent/github-integration/cloud-remote-fix.md):
  retired and archived with this spec. The already archived token-store,
  repository-selection and read-surface specs each get one line pointing
  here.
- [repo-identity](../../../cloud/latere-integration/coordination-plane/repo-identity.md):
  its upgrade tier (a server-authoritative check through the account's GitHub
  connection) loses its input and is struck. The default tier (proof with the
  user's own git credentials) never depended on it.
- [platform integration](../../../cloud/latere-integration.md) and the
  [hosted executor](../../../cloud/latere-integration/topos-remote-executor.md):
  where they point at cloud-remote-fix for repositories outside the
  platform's git host, they say instead that such repositories are out of
  scope until a platform connector exists.
- The Git Workflow track keeps [task-revert](../../../intent/task-revert.md) and
  the archived intent-commits. It no longer has a GitHub half.

## Out of scope

- Any change to plain git operations.
- The Latere account's GitHub connection, which is the identity service's.
- A connector design.

## Outcome

Archived 2026-10-02 as complete. Shipped in four commits:

- `ea6bcbd8` frontend: no Settings tab, and no pull-request state on a task
  card, the task sheet, or a spec's focused view.
- `ec6323ee` handler: the eight routes, their handlers and tests, the contract
  entries and the `github` key in `/api/config` are gone, and so is the store
  and broker construction in `internal/cli/server.go`.
- `631273da` cli: `internal/github` is deleted, and the first start after
  upgrade removes `<config dir>/github/` with one log line.
- `08d57a9a` docs: guide, internals, README and changelog.

Where it differed from the text above:

- **Order.** Steps 2 and 3 could not compile separately: the handler setters
  lived in a file step 2 deletes, so step 2 also removed their construction
  in `internal/cli/server.go`, and step 3 deleted the package.
- **Surface the spec missed:** the pull-request pill in
  `plan/SpecFocusedView.vue`, an orphaned rule in `styles/modal.css`, two
  paths in `tests/designSystem.test.ts`, the Settings tab loop in the
  screenshot checks, and the generated docs index.
- **Surface that did not exist:** no GitHub types in `api/types.ts` (they
  lived in the two stores) and no strings in the language files.
- **Acceptance criterion 2** holds on the API mux. On the full server, an
  unknown `GET /api/...` path is answered by the single-page app's catch-all
  with its index page, and other methods with 405. That is true of every
  unknown API path, not only these, and is recorded as a separate defect.
- **Criterion 6** gained a test it did not have: the session bridge writes the
  shared identity token file and a fresh connector store reads it back.
- `make ui-test` was not run: it builds the frontend, and its fixed paths and
  port collide with parallel runs. The edited scene was checked by reading,
  and no committed screenshot shows a removed surface.
