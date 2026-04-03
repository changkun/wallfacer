---
title: Intent-Driven Commit Tracking
status: vague
depends_on: []
affects:
  - internal/runner/commit.go
  - internal/handler/explorer.go
  - internal/planner/
  - internal/gitutil/
effort: large
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Intent-Driven Commit Tracking

## Overview

Treat code as a managed resource where every intent that produces a change — a task completion, a planning chat message, a manual file edit in the explorer — results in a git commit. This gives the platform a complete, fine-grained history of who (or what) changed what and why, enabling reliable undo/revert at any granularity. Today, commits only happen at the end of the task commit pipeline (`internal/runner/commit.go`); explorer edits and planning agent writes are left uncommitted until some external action flushes them.

## Current State

Three distinct paths produce file changes, each with different commit behavior:

1. **Task execution** (`internal/runner/commit.go`): The commit pipeline runs after a task reaches `done`. `hostStageAndCommit()` stages all uncommitted changes, generates a commit message via a lightweight container, then `rebaseAndMerge()` rebases onto the default branch. Commits are **deferred and batched** — all changes from a multi-turn task become a single commit (or a small set if the agent committed inside the container).

2. **Explorer file editing** (`internal/handler/explorer.go:ExplorerWriteFile`): Atomic file writes (temp + rename) with workspace boundary validation. **No commit** — changes remain uncommitted in the worktree indefinitely.

3. **Planning agent** (`internal/planner/`): The planning sandbox writes spec files directly into the `specs/` directory (read-write mount). **No commit** — changes accumulate in the working tree across multiple chat rounds. The `undo-snapshots` sub-design proposes per-round snapshots but hasn't settled on a mechanism.

The result is that the only changes with reliable git history are completed tasks. Explorer edits and planning writes can be lost on a bad `git checkout` or overwritten by the next task's rebase.

## Problem

Without consistent commit tracking:
- **Undo is ad hoc.** The undo-snapshots spec explores file-copy, stash, and branch-based snapshots — but if every change were already committed, undo becomes `git revert` or `git reset` on a well-defined commit, and the snapshot mechanism is just git itself.
- **Attribution is lost.** There's no record of which planning chat message produced which file change, or when a user manually edited a spec via the explorer. Git blame shows nothing for uncommitted work.
- **Revert scope is unclear.** When something goes wrong, it's hard to identify the minimal set of changes to undo because multiple intents may have been batched or left uncommitted.
- **Concurrent task isolation is fragile.** Tasks use worktrees for isolation, but explorer edits happen on the main working tree and can conflict with worktree merges.

## Design Direction

### Core Principle: One Intent = One Commit

Every discrete user or agent intent that modifies files should produce a commit with metadata linking it to the originating intent:

| Intent source | Commit trigger | Commit message pattern |
|---------------|----------------|----------------------|
| Task completion | Existing commit pipeline | `task(<short-id>): <generated message>` |
| Planning chat round | After agent response completes | `plan(<round>): <summary of changes>` |
| Explorer file edit | On save (debounced for rapid edits) | `edit: <filename> via explorer` |
| Manual batch | User explicitly requests | `batch: <user-provided message>` |

### Key Design Questions

- **Commit granularity vs noise.** One commit per explorer save can produce many tiny commits. Should explorer edits be debounced (e.g., batch all saves within 5 seconds into one commit)? Should there be an explicit "save point" button instead of auto-commit?
- **Branch strategy.** Should intent commits land on the main working tree, a per-session branch, or a per-source branch (e.g., `planning/<fingerprint>`, `explorer/<timestamp>`)? Per-source branches keep the main branch clean but complicate merging.
- **Relationship to undo-snapshots.** If every change is committed, the undo-snapshots spec simplifies dramatically: undo = revert the commit. The snapshot storage question (file-copy vs stash vs branch) is answered by "just use git history." The undo-snapshots spec could be superseded or refactored to build on this.
- **Relationship to planning-chat-agent.** The planning agent's message flow (step 7: "agent writes files directly") would gain a post-write commit step. The conversation store already tracks which message produced which changes; the commit message can reference the message ID.
- **Relationship to task-revert.** The task-revert spec proposes agent-assisted revert of merged task changes. If tasks already produce well-attributed commits, revert becomes simpler — `git revert <commit-range>` instead of trying to reconstruct what a task changed.
- **Commit message generation.** Task commits already use a lightweight container to generate messages. Planning and explorer commits need something lighter — perhaps a template-based message without LLM generation, or a shared lightweight summarizer.
- **Performance.** Git commits are fast (~10-50ms) but add up. Need to measure impact on the explorer editing experience and planning agent throughput.
- **Non-git workspaces.** Some users may work in directories that aren't git repositories. The system needs a fallback (initialize git? skip commits? use a separate tracking mechanism?).

## Components

### Intent Commit Layer

A shared abstraction that all change sources (runner, planner, explorer handler) call after writing files. Responsibilities:
- Stage changed files
- Generate or accept a commit message
- Create the commit with structured metadata (intent type, source ID, timestamp)
- Handle debouncing for rapid sequential writes

### Commit Metadata

Structured trailers or commit message conventions that link commits to their originating intent:
- `Intent-Type: task | plan | edit | batch`
- `Intent-Source: <task-id> | <message-id> | explorer`
- Machine-readable for programmatic undo/revert

### Undo via Git History

With every change committed, undo becomes:
- Single-intent undo: `git revert <commit>`
- Multi-intent undo: `git revert <commit-range>`
- Planning session undo: revert all commits with `Intent-Type: plan` for a given session

This potentially supersedes or simplifies the undo-snapshots spec's design.

### Batch Commit Mode

For cases where auto-commit is too noisy (e.g., iterating on a spec via the explorer), the user can enter "draft mode" where changes accumulate uncommitted, then explicitly commit with a summary message. This is the escape valve from the one-intent-one-commit principle.

## Related Specs

- **`undo-snapshots.md`** — Currently explores file-copy/stash/branch snapshots. Would be simplified or superseded by intent commits: undo = git revert.
- **`planning-chat-agent.md`** — Message flow gains a post-write commit step. Conversation store references commit hashes.
- **`task-revert.md`** — Revert of merged task changes is easier with well-attributed commits.
- **`pull-request.md`** — PR creation benefits from clean, well-attributed commit history.

## Testing Strategy

- Unit tests for the intent commit layer (stage, commit, metadata trailers)
- Unit tests for debouncing logic (rapid writes produce one commit)
- Integration tests for each change source: explorer write → commit exists, planning round → commit exists
- Integration tests for undo: revert a planning commit, revert an explorer commit
- Edge cases: non-git workspace, empty change set (no-op commit), concurrent writes from multiple sources
