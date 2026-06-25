---
title: "GitHub Read Surface: PRs, Issues, and Comments"
status: drafted
depends_on:
  - specs/intent/github-integration/oauth-token-store.md
affects:
  - internal/github/read.go
  - internal/github/client.go
  - internal/handler/github.go
  - frontend/src/components/GithubPanel.vue
  - frontend/src/stores/github.ts
effort: large
created: 2026-06-26
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# GitHub Read Surface: PRs, Issues, and Comments

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

## Affects

Establishes `internal/github/client.go` (shared transport) and `read.go`, adds
the `GET /api/github/pulls`, `/pulls/{number}`, `/issues`, `/issues/{number}`
routes, and the list/detail UI in the GitHub panel.
