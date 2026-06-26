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
  - frontend/src/components/GithubPanel.vue
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

### Listing source

- **User-affiliated** (`GET /user/repos`): everything the user can see, simple,
  but noisy for users in many orgs.
- **Org-scoped** (`GET /orgs/{org}/repos`, filtered to the signed-in org):
  matches repo-identity's org boundary; the natural default for team mode.
- **GitHub App installation repos** (`GET /installation/repositories`): only
  what the app was granted; tightest, depends on the App-type decision in the
  oauth child.

### Selection -> workspace association

- **Match existing group member**: resolve picked repo to `host/owner/repo`,
  find the workspace folder whose `origin` normalizes to the same key, select
  it. No clone. Errors clearly when no local clone exists (pointing at the gated
  remote phase).
- **Persist selection independent of folders**: store "current repo" as a
  GitHub-context selection decoupled from `workspace.Group`, so the read/write
  surfaces work even before a local clone is associated.

## Open Questions

1. Default listing scope: org-only (enforcing the boundary) or user-wide with
   client-side filtering? How does this interact with the App-installation
   model if the oauth child picks GitHub App?
2. Pagination + search for users/orgs with hundreds of repos: server-side
   search param, incremental fetch, or cache the full list?
3. Is "selected repo" a property of the workspace group, a separate GitHub
   selection state, or both? What does the read surface key on?
4. When the user picks a repo with no local clone (local mode), what is the UX:
   disabled with a "clone via remote phase" hint, or an inline error?
5. How is the org boundary enforced on the list call so a repo outside the
   signed-in org never appears (filter server-side, reuse repo-identity's
   perimeter check)?
6. Does local v1 actually need repo-identity's **server registry** (org->repos
   Postgres, part of the unbuilt coordination plane), or only the already
   shipped `NormalizeRemoteURL` plus repo-identity's **local-credential-proof
   tier** (`git ls-remote`)? If the latter, this child ships without the cloud
   plane, and the `repo-identity` dependency is "consume the identity model and
   local tier", not "the server registry must exist first". Resolve this so the
   umbrella's "components 1-4 ship independently of remote execution" claim
   holds.

## Affects

Adds repo-list + repo-select endpoints under `/api/github/repos`, a
`internal/github` repo-list client, and the picker UI. Consumes
`NormalizeRemoteURL` to key selections; touches `workspace.Group` only if the
selection is stored on the group.
