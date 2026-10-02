---
title: "Cloud Clone and Remote Fix (Gated)"
status: vague
depends_on:
  - specs/cloud/latere-integration/topos-remote-executor.md
affects:
  - internal/github/clone.go
  - internal/hostedagents/
  - internal/handler/github.go
  - frontend/src/components/GithubPanel.vue
effort: large
created: 2026-06-26
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Cloud Clone and Remote Fix (Gated)

Child of [github-integration](../github-integration.md). Gated and dispatched
last; blocked on the hosted executor, which is drafted and not built.

## Design Problem

The "remotely clone repo and fix things on GitHub like Codex" ask: pick a GitHub
repo with no local checkout, run an agent against it in the cloud, and get the
result back as a branch or a pull request.

## Context

- Remote execution is **Cloud Axis B**, and it is one executor: a task runs as
  a hosted agent session on the Latere platform
  ([topos-remote-executor.md](../../cloud/latere-integration/topos-remote-executor.md)).
  Wallfacer creates no sandbox and stages no files; the session clones its
  repositories and pushes a branch of its own.
- A hosted session's workload holds no credential. The platform injects one at
  the workload's egress, and only for the platform's own git host. A repository
  on another host is cloned without a credential.
- For a GitHub repository that means: a **public** one can be cloned by a
  session and cannot be pushed to; a **private** one cannot be cloned at all.
- The local flow today always assumes a host worktree; there is no clone path
  and no no-local-checkout execution.
- This spec is intentionally `vague`: it captures the target and the gate, not a
  ready design.

The earlier draft of this spec asked how to deliver a GitHub token into a
sandbox wallfacer created. That question is gone with the sandbox: wallfacer
never holds a session's workload, so it has nowhere to put a token.

## Options

The open shape is how a GitHub repository reaches a hosted session and how the
result returns to GitHub.

| Option | How | Cost |
|---|---|---|
| **A. Mirror through the platform's git host** | Wallfacer (or the user) keeps a repository on the platform's git host that mirrors the GitHub one. The session works there. Wallfacer fetches the session's branch and pushes it to GitHub with the user's GitHub token, then opens the pull request through the [pull-request](pull-request.md) write path | Works with the platform as it is. Needs somewhere to run the fetch and push when there is no local checkout, which is the hosted board's repository question ([cloud-infrastructure](../../cloud/cloud-infrastructure.md)) |
| **B. The platform holds the GitHub credential** | The platform learns to inject a GitHub credential at a session's egress, from a connection the user makes once. The session clones from and pushes to GitHub directly | No mirror and no relay. Not a wallfacer change: it is a platform capability that does not exist |
| **C. Public repositories only, patch back** | The session clones a public GitHub repository without a credential and returns its work as a patch in its final message; wallfacer applies and pushes it | Narrow, and a patch in a message is a poor transport for large changes |

## Open Questions

1. Which option, and whether the answer waits for the platform (B) or ships on
   what exists (A).
2. Where the relay in option A runs when the user has no local checkout.
3. Does the remote result reuse the existing [pull-request](pull-request.md)
   path to open the pull request, in every option?
4. How a task with no local worktree appears on the board alongside local tasks:
   what the diff view, the file explorer and the commit pipeline show.
5. Should this stay one spec, or split into "repository route" and "remote task
   on the board" once the hosted executor is real?

## Affects

When unblocked: a repository route in `internal/github`, a session repository
entry built in `internal/hostedagents`, and a remote-task affordance in the UI.
Until then, the action is hidden or disabled in the UI (per the umbrella's
error handling).
