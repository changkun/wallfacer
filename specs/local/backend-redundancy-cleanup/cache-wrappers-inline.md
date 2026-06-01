---
title: Inline diffcache and commitsbehind_cache thin wrappers
status: drafted
depends_on:
  - specs/local/backend-redundancy-cleanup.md
affects:
  - internal/handler/diffcache.go
  - internal/handler/diffcache_test.go
  - internal/handler/commitsbehind_cache.go
  - internal/handler/commitsbehind_cache_test.go
  - internal/handler/git.go
  - internal/handler/execute.go
  - internal/handler/tasks.go
  - internal/handler/tasks_autopilot.go
effort: small
created: 2026-06-01
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Inline diffcache and commitsbehind_cache thin wrappers

`internal/handler/diffcache.go` (67 LOC) and
`internal/handler/commitsbehind_cache.go` (68 LOC) are thin wrappers
over `internal/pkg/cache.TTLCache`. They survive today because they
provide typed APIs (`get(uuid.UUID)`, `cachedCommitsBehind(repo,
worktree)`) over the generic primitive.

Pass-1 cleanup deliberately left them alone because the wrappers
encapsulated some real behavior (immutable-vs-volatile split for
diffs, read-through for commits-behind). On a second look the
encapsulation is narrow:

- `diffcache.set` chooses between `SetPermanent` and `Set` based on
  `entry.immutable`. Two callers (in git.go) — they could pick
  directly.
- `commitsbehindCache.cachedCommitsBehind` is read-through over
  `gitutil.CommitsBehind` with a NUL-joined key. The read-through is
  reusable, but the key shape is per-callsite.

## Scope

Two options, pick during implementation:

### Option A — keep both wrappers, narrow their surface

Delete unused convenience methods (e.g. the separate `get`/`set` on
`commitsbehindCache` if only `cachedCommitsBehind` and `invalidate`
have external callers) so the wrappers stay typed but minimal.

### Option B — inline into call sites

Delete both wrappers and their tests. The five-or-so call sites in
git.go / execute.go / tasks.go / tasks_autopilot.go construct a
`cache.New[K, V]` directly with the typed key (uuid.UUID for diffs,
the NUL-joined string for commits-behind). The immutable-vs-volatile
branch in git.go moves inline (one `if` with `SetPermanent` vs
`Set`).

Net: ~130 LOC deleted, no behaviour change, type ambiguity stays
contained because each call site is the only place that knows its
key type.

Author judgement at implementation time.

## Tests

If Option B: the existing per-cache test files become tests against
`internal/pkg/cache.TTLCache` (which is already tested) and can be
deleted. Add a single integration test in `git_test.go` that
exercises the immutable branch end-to-end so the inline `if` is
covered.

## Out of scope

- Changes to `internal/pkg/cache`. The generic primitive is fine.
- The `internal/handler/file_index.go` stale-while-revalidate cache
  (163 LOC) — different shape (serve-stale + background refresh), and
  worth keeping as its own type. Possibly worth promoting to
  `internal/pkg/cache.StaleWhileRevalidate[K,V]` later, but separate
  from this spec.
