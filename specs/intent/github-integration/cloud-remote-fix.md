---
title: "Cloud Clone and Remote Fix (Gated)"
status: vague
depends_on:
  - specs/intent/github-integration/oauth-token-store.md
  - specs/cloud/latere-integration/cella-runtime.md
  - specs/cloud/latere-integration/topos-remote-executor.md
affects:
  - internal/github/clone.go
  - internal/executor/
  - internal/handler/github.go
  - frontend/src/components/GithubPanel.vue
effort: large
created: 2026-06-26
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Cloud Clone and Remote Fix (Gated)

Child of [github-integration](../github-integration.md). Gated and dispatched
last; blocked on the Cloud Axis B Executor seam, which does not exist yet.

## Design Problem

The "remotely clone repo and fix things on GitHub like Codex" ask: pick a GitHub
repo with no local checkout, clone it into a cloud sandbox, run agents against
it, and push results back as a branch/PR. This is the only component that
reaches the runner/executor machinery rather than plain API calls, and it cannot
ship until the remote executor seam exists.

## Context

- Remote execution is **Cloud Axis B**, marked demand-gated and blocked on the
  `Executor` seam in [README](../../README.md). The seam is specced but not
  built: [cella-runtime.md](../../cloud/latere-integration/cella-runtime.md)
  (`CellaBackend` implementing `executor.Backend`) and
  [topos-remote-executor.md](../../cloud/latere-integration/topos-remote-executor.md).
- The local flow today always assumes a host worktree; there is no clone path
  and no no-local-checkout execution. This component depends on both the GitHub
  token (to clone private repos) and a working remote backend.
- This spec is intentionally `vague`: it captures the target and the gate, not a
  ready design. It is refined once the Executor seam lands.

## Options

Deferred until the Executor seam is concrete. The open shape:

- Where the clone happens (sandbox-side `git clone` with a token injected via
  the existing `sandbox_proxy.go` credential substitution, vs. FS-staged
  worktree per [tenant-filesystem.md](../../cloud/tenant-filesystem.md)).
- How the remote run's branch/diff returns and feeds the
  [pull-request](pull-request.md) write path.
- How a "remote task" appears on the board alongside local tasks.

## Open Questions

1. Blocked: which executor backend (Cella vs Topos) is the first target, and
   what is its actual clone + run + return contract once built?
2. How is the GitHub token delivered into the sandbox for a private clone
   without leaking it (reuse `sandbox_proxy.go` delegation)?
3. Does the remote result reuse the existing [pull-request](pull-request.md)
   path to open the PR, or open it sandbox-side?
4. Should this stay one spec, or split into "remote clone/checkout" and "remote
   agent run" once the seam is real?

## Affects

When unblocked: a clone path in `internal/github`, integration with
`internal/executor/` remote backends, and a remote-task UI affordance. Until
then, the action is hidden/disabled in the UI (per the umbrella's error
handling).
