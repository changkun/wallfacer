---
title: "GitHub Read Surface: PRs, Issues, and Comments"
status: archived
depends_on:
  - specs/intent/github-integration/oauth-token-store.md
affects:
  - internal/github/read.go
  - internal/github/client.go
  - internal/handler/github.go
  - frontend/src/views/GithubPage.vue
  - frontend/src/components/github/
  - frontend/src/components/Sidebar.vue
  - frontend/src/router.ts
  - frontend/src/stores/github.ts
effort: large
created: 2026-06-26
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# GitHub Read Surface: PRs, Issues, and Comments

> **Archived 2026-10-02. Retired as: GitHub is metadata on a task, not a
> destination to browse.** This spec was built and then removed. The
> backend landed in `fc3b9a91`: `ListPulls`, `ListIssues`, `GetPull` and
> `GetIssue` in `internal/github/read.go`, and
> `GET /api/github/pulls`, `/pulls/{number}`, `/issues` and
> `/issues/{number}` in `internal/handler/github_read.go`. It was REST
> only, with conversation comments attached to the detail; line-anchored
> review comments, a cache and a GraphQL client were never written. The
> `/github` page, its route and its Sidebar entry landed in `1a8ebf9e`,
> on the store from `2501105b`.
>
> The task-centric redesign of 2026-06-30, recorded in the
> [umbrella](../github-integration.md), removed all of it one
> day later: the page, route, Sidebar entry and browse state in
> `22407df2`; the four read functions and `github_read.go` in `bd5740cc`;
> the routes in `df9df847`. The last two commits carry unrelated
> subjects; the teardown rode along with them.
>
> What replaced each part:
>
> - **Pull request list and detail.** One lookup per task:
>   `GET /api/tasks/{id}/pr` (`internal/handler/tasks_pr.go`, `ca8104b3`)
>   returns the open pull request whose head is the task's branch, through
>   `PullForBranch` in `internal/github/write.go`. It is shown in the task
>   detail panel, as a badge on the board card and as a pill on a spec
>   whose dispatched task has one (`b58ae5e4`). The design lives in
>   [pull-request](pull-request.md).
> - **Issues.** Dropped. No list, detail or read endpoint remains.
> - **Comment threads.** Not read anywhere. A comment can be posted to a
>   task's pull request; the thread itself is read on GitHub.
> - **The shared transport.** Kept. `internal/github/client.go` (`d41e1b51`)
>   is what the write surface calls. It still parses rate-limit headers and
>   the `Link` next-page cursor, and no caller reads either: a rate-limit
>   rejection reaches the browser only as a 429 from `mapGitHubAPIError`.
> - **`internal/github/read.go`.** Kept as a file, holding only the
>   `PullRequest` and `Comment` models the write surface returns.
> - **Rate-limit readout, cache, pagination.** Dropped with the list views
>   that needed them.
>
> The text below is kept as written for the record. The umbrella's "UI
> Architecture" section it refers to was removed from the umbrella on
> 2026-10-02; its last version is in the umbrella at commit `acc2872a`.

Child of [github-integration](../github-integration.md).

## Design Problem

Read the selected repo's collaboration state into Wallfacer: list pull requests
and issues, and open PR/issue detail with its comment threads. This is plain
authenticated HTTP, no runner or sandbox. The open decisions are about shape and
cost: REST vs GraphQL per view, how pagination is exposed to the UI, how the
GitHub rate limit is respected and surfaced, and how aggressively list/detail
responses are cached so polling does not exhaust the budget.

## Context

- The token comes from [oauth-token-store](oauth-token-store.md); the repo from
  [repo-selection](repo-selection.md).
- No GitHub API client exists yet; this child establishes
  `internal/github/client.go` (the shared authenticated transport: base URL,
  auth header, rate-limit header parsing, pagination helper) that the write
  surface and repo-selection also build on.
- The existing git status SSE stream (`/api/git/stream`,
  `internal/handler/git.go`) polls every 5s; a similar poll for PR/issue lists
  would hit the rate limit without caching.

## Options

### REST vs GraphQL

- **REST**: simple, one resource per call. A PR with its review comments and
  conversation comments needs several calls (`pulls/{n}`, `pulls/{n}/comments`,
  `issues/{n}/comments`).
- **GraphQL**: one round trip for a PR + all its comment kinds; fewer
  rate-limit hits for the detail composite; more complex client. Lists are fine
  in REST; detail is where GraphQL pays off.

Lean: REST for lists, GraphQL (or a batched REST fan-out) for detail composites;
decide per view.

### Caching + freshness

- **Short-TTL cache** (per repo+view, ~30-60s) keyed so list polls are served
  from cache; bypass on explicit refresh. Combine with ETag/conditional requests
  (GitHub returns 304s that do not count against the rate limit).
- **No cache, on-demand only** (fetch when the panel opens): simplest, but
  refetches on every navigation.

## Open Questions

1. Which views use REST and which use GraphQL? Is the PR-detail composite worth
   the GraphQL client, or is a batched REST fan-out enough for v1?
2. Cache TTL and invalidation: ETag conditional requests + short TTL, or a
   simpler open-on-demand fetch? Is there a background poll at all, or only
   user-triggered loads?
3. How is the rate-limit budget surfaced to the UI (remaining/reset), and what
   is the behavior on a secondary-limit 403 (backoff, disable refresh, queue)?
4. Pagination UX: infinite scroll, explicit pages, or a capped first-page with
   "open on GitHub" for the long tail?
5. What is the canonical internal model for a PR/issue/comment that both the
   read views and the write surface (reply/comment) share?

## UI

Owns the **`/github` page shell** (the umbrella's
[UI Architecture](../github-integration.md)): the new
`views/GithubPage.vue`, its route in `router.ts` (`localRoutes`, with
`meta: { needsWorkspace: true }`), and a Sidebar entry under the **Workspace**
group in `Sidebar.vue` (`{ id: 'github', label: 'GitHub', to: '/github',
icon: 'github' }` plus the inline SVG). The page hosts the repo selector
(component 2), the PRs/Issues tabs, and the master-detail list/detail; the write
affordances (comment box, Create PR) are component 4 slotted in.

States this child owns from the shared matrix: **Loading**, **Empty**,
**Error**, **Rate-limited**, plus the page-level **Disconnected** call-to-action
that links into component 1's Settings tab.

```
/github  (page = header + tabs + master-detail)
+--------------------------------------------------------------+
| [ latere/wallfacer ▾ ]            rate: 4980/5000   [ ↻ ]    |
+--------------------------------------------------------------+
|  ( Pull Requests )  ( Issues )                               |
+----------------------+---------------------------------------+
| open  closed  all    |  #42  Add task revert ...             |
| [ filter labels ]    |  @author · opened 3d ago · ✓ 2 / ✗ 0 |
| [ search ]           |  -------------------------------------|
|----------------------|  Summary / body (markdown)           |
| ▸ #42 Add task ...   |  -------------------------------------|
| ▸ #41 Fix flaky test |  ▸ review comment (file:line)         |
| ▸ #39 Bump deps      |  ▸ conversation comment               |
| · loading more...    |  ▸ ...                                |
+----------------------+---------------------------------------+
     list pane (master)          detail pane (lazy-loaded)
```

- **Tabs**: Pull Requests / Issues switch the list pane; each has open/closed/all
  state filters (issues also label filter). Switching tabs preserves the
  selected repo and the per-tab filter.
- **List pane** (master): paginated rows; selecting a row loads the detail pane.
  Pagination UX follows open question 4 -- default to incremental "load more" /
  infinite scroll with a capped window and an "open on GitHub ↗" escape for the
  long tail.
- **Detail pane**: lazy-loads on selection (PR or issue) with its merged comment
  thread (review comments line-anchored, conversation comments inline); built
  from the REST-vs-GraphQL decision in open question 1.
- **Loading**: skeleton rows in the list pane, a skeleton block in the detail
  pane; chrome (repo selector, tabs, refresh) stays interactive.
- **Empty**: "No open pull requests" / "No open issues" echoing the active
  filter; not a blank pane.
- **Error**: inline error with a retry in the affected pane; the rest of the
  page keeps working.
- **Rate-limited**: the header shows remaining/reset (open question 3); on a
  secondary-limit 403 the manual refresh is disabled with a "resets in N min"
  hint and lists fall back to cache. The header rate readout is shared chrome
  other components read.
- **Refresh** (`↻`) bypasses the short-TTL cache for the active view; normal
  navigation is served from cache (open question 2).

## Affects

Establishes `internal/github/client.go` (shared transport) and `read.go`, adds
the `GET /api/github/pulls`, `/pulls/{number}`, `/issues`, `/issues/{number}`
routes, and the `/github` page + list/detail UI (see UI above), including the
route and Sidebar entry that make the page reachable.

Later, 2026-10-02: the whole GitHub integration was retired from wallfacer, this part included. See [remove-github-integration](../../shared/platform-native/remove-github-integration.md).
