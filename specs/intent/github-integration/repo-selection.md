---
title: "GitHub Repo Selection"
status: stale
depends_on:
  - specs/intent/github-integration/oauth-token-store.md
  - specs/cloud/latere-integration/coordination-plane/repo-identity.md
affects:
  - internal/github/repos.go
  - internal/handler/github.go
  - internal/coordinator/identity.go
  - internal/workspace/groups.go
  - frontend/src/views/GithubPage.vue
  - frontend/src/components/github/RepoPicker.vue
  - frontend/src/stores/github.ts
effort: medium
created: 2026-06-26
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# GitHub Repo Selection

Child of [github-integration](../github-integration.md).

## Design Problem

"Select a GitHub repo based on the user's choice." With a GitHub token in hand
(from [oauth-token-store](oauth-token-store.md)), list the repos the identity
can access and let the user pick one, resolving the choice to canonical
`host/owner/repo`. The open decisions: which repos to list and how (user repos,
org repos, installation repos, pagination/search at scale), how a picked repo
associates with local workspace state, and how the org boundary is enforced.

## Context

- Repo identity is owned by
  [repo-identity.md](../../cloud/latere-integration/coordination-plane/repo-identity.md):
  canonical `host/owner/repo` via `NormalizeRemoteURL`
  (`internal/coordinator/identity.go`), durable org->repos registry, org
  boundary as the security perimeter. This child **resolves a selection into**
  that identity; it does not redefine it.
- Workspaces today are local folder paths in a `workspace.Group`
  (`internal/workspace/groups.go`); there is no clone path. A picked repo in
  local mode associates with an existing group member by matching its `origin`.
- Actual clone-into-a-new-folder belongs to the gated
  [cloud-remote-fix](cloud-remote-fix.md) phase, not here.

## Options

### Listing source (default: installation repos)

The umbrella resolved the app type to a **GitHub App**, so the default listing
source is **installation repos**:

- **GitHub App installation repos** (`GET /installation/repositories` or
  `GET /user/installations/{id}/repositories`): only what the install was
  granted. This is the default -- the install grant *is* the org-boundary
  enforcement, so the list cannot leak repos outside what the org admin
  approved, and there is no client-side org filtering to get wrong.
- **Org-scoped** (`GET /orgs/{org}/repos`): only relevant under the OAuth-App
  fallback; superseded by installation repos under the GitHub App path.
- **User-affiliated** (`GET /user/repos`): not used; too broad for the
  install-scoped model.

When the install grants no repos (or the user lands before installing), the
picker shows the install affordance rather than an empty list.

### Selection -> workspace association

- **Match existing group member**: resolve picked repo to `host/owner/repo`,
  find the workspace folder whose `origin` normalizes to the same key, select
  it. No clone. Errors clearly when no local clone exists (pointing at the gated
  remote phase).
- **Persist selection independent of folders**: store "current repo" as a
  GitHub-context selection decoupled from `workspace.Group`, so the read/write
  surfaces work even before a local clone is associated.

## Open Questions

1. ~~Default listing scope?~~ **Resolved: installation repos** (GitHub App,
   `GET /installation/repositories`). The install grant is the boundary; no
   client-side org filtering.
2. Pagination + search for users/orgs with hundreds of repos: server-side
   search param, incremental fetch, or cache the full list?
3. Is "selected repo" a property of the workspace group, a separate GitHub
   selection state, or both? What does the read surface key on?
4. When the user picks a repo with no local clone (local mode), what is the UX:
   disabled with a "clone via remote phase" hint, or an inline error?
5. ~~How is the org boundary enforced on the list call?~~ **Mostly resolved**:
   under the GitHub App, the installation grant scopes the list, so a repo
   outside what was granted cannot appear. Remaining: confirm the
   `host/owner/repo` of each listed repo still passes repo-identity's perimeter
   check before selection (defense in depth), and that org-boundary still maps
   to the signed-in org when one install spans repos.
6. Does local v1 actually need repo-identity's **server registry** (org->repos
   Postgres, part of the unbuilt coordination plane), or only the already
   shipped `NormalizeRemoteURL` plus repo-identity's **local-credential-proof
   tier** (`git ls-remote`)? If the latter, this child ships without the cloud
   plane, and the `repo-identity` dependency is "consume the identity model and
   local tier", not "the server registry must exist first". Resolve this so the
   umbrella's "components 1-4 ship independently of remote execution" claim
   holds.

## UI

Contributes the **repo selector** to the `/github` page (the umbrella's
[UI Architecture](../github-integration.md#ui-architecture)): a `RepoPicker.vue`
under `components/github/`, surfaced both as the page header's repo dropdown and,
when no repo is chosen, as the page's centered first-run picker. Selection state
lives in `stores/github.ts` and keys the read/write surfaces.

States this child owns from the shared matrix: **No repo selected**,
**Org-boundary blocked**, **No local clone**. (**Disconnected** defers to
component 1.)

```
No repo selected (centered first-run picker)
+--------------------------------------------------------------+
|  Choose a repository                                         |
|  Installed on: latere      [ Manage installation ↗ ]         |
|  [ search granted repos... ]                                 |
|  ----------------------------------------------------------- |
|  ▸ latere/wallfacer      default · last push 2h ago   ● local|
|  ▸ latere/agents         default · last push 1d ago   ○ no   |
|  ▸ latere/terraform      default · last push 5d ago   ○ no   |
|  ... (paginated / incremental)                              |
|  + Install on another org or grant more repos ↗             |
+--------------------------------------------------------------+

Selected (collapses into the page header dropdown)
  [ latere/wallfacer ▾ ]   ● local checkout
```

- Each row shows `owner/repo`, the default branch, last-push recency, and a
  **local-clone indicator** (`● local` when a `workspace.Group` member's
  `origin` normalizes to the repo's `host/owner/repo`; `○ no` otherwise).
- **Selecting a repo with a local clone** resolves to that workspace and enables
  the full surface.
- **No local clone** (`○ no`): selectable for read (PR/issue browse works
  without a checkout), but a banner notes "no local checkout" and points at the
  gated [cloud-remote-fix](cloud-remote-fix.md) phase for clone-and-fix; write
  actions that need a working tree are disabled with that hint. Not an error.
- **Install affordance** (`+ Install on another org or grant more repos`) deep
  links to the GitHub App installation page; on return, the list refetches.
- **Org-boundary blocked**: a repo whose identity fails the perimeter check is
  omitted from the list; a direct selection attempt returns 403 surfaced as
  "outside your organization".
- **Search + pagination** follow open question 2 (server-side search vs cached
  list); the UI assumes incremental load with a search box for large installs.

## Affects

Adds repo-list + repo-select endpoints under `/api/github/repos`, a
`internal/github` repo-list client, and the `RepoPicker.vue` UI (see UI above).
Consumes
`NormalizeRemoteURL` to key selections; touches `workspace.Group` only if the
selection is stored on the group.
