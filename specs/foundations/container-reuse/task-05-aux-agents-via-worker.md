---
title: "Verify Auxiliary Agents Through Per-Task Worker"
status: complete
track: foundations
depends_on:
  - specs/foundations/container-reuse/task-03-launch-routing.md
affects:
  - internal/runner/container.go
effort: small
created: 2026-03-27
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 5: Verify Auxiliary Agents Through Per-Task Worker

## Goal

Verify that title generation, oversight summaries, and commit message
generation work correctly when routed through the per-task worker
container instead of ephemeral containers.

## What to do

1. Verify that the container specs built for title, oversight, and commit
   message agents include the `wallfacer.task-id` label. Check
   `internal/runner/container.go`:
   - `buildContainerSpecForSandbox()` — already sets labels
   - Confirm the task-id label is present for all agent activities

2. If any auxiliary agent spec is missing the task-id label, add it.

3. Write integration tests that:
   - Create a task with a per-task worker
   - Run a title generation through it (via `Launch()`)
   - Run an oversight summary through it
   - Verify outputs match expectations (same as ephemeral)

4. Verify concurrent exec within the same task worker works (e.g., title
   generation running while implementation turn is in progress — though
   this is rare since they're usually sequential).

## Tests

- `TestTitleGenerationViaWorker` — verify title agent output through
  worker matches ephemeral.
- `TestOversightViaWorker` — verify oversight output through worker.
- `TestCommitMessageViaWorker` — verify commit message output through
  worker.

## Boundaries

- Do NOT change the agent prompts or output parsing.
- This is a verification task, not new feature work.
