---
title: Dead-code cleanup after runAgent migration
status: archived
depends_on:
  - specs/shared/agent-abstraction/headless-roles.md
  - specs/shared/agent-abstraction/inspector-roles.md
  - specs/shared/agent-abstraction/heavyweight-roles.md
affects:
  - internal/runner/
  - internal/constants/
effort: small
created: 2026-04-18
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---


# Dead-code cleanup after runAgent migration

## Goal

After the three migration tasks land, the runner package will have
left-over container-spec builders and wrappers that no caller uses.
Remove them in a small, self-contained cleanup commit so the package
reads cleanly against the agent-descriptor pattern.

## What to do

1. Grep `internal/runner/` for references to each of the following
   and delete any that are now unreferenced:
   - `buildTitleContainerSpec`, `buildOversightContainerSpec`,
     `buildCommitMessageContainerSpec`
   - `buildRefinementContainerSpec`, `buildIdeationContainerSpec`
   - `oversightRunResult`, any similar result-wrapper types that
     `runAgent`'s unified return now covers
   - Per-role fallback retry wrappers superseded by the shared
     `retryWithFallbackSandbox`
2. Consolidate any remaining hardcoded per-role timeouts into
   `internal/constants/`. Remove role-file-local timeout constants
   that were copied into the shared location during the phase-0 task.
3. Grep for `wallfacer-ideate-` (timestamp-based) container names —
   the `uuid8` rename in the inspector migration should have
   replaced them, but tests / fixtures / docs may still reference
   the old format. Update any that remain.
4. Normalize flag ordering in any remaining call sites to
   `--verbose --output-format stream-json -p <prompt>` (the canonical
   order `runAgent` emits). The migration tasks should already do
   this; this step is a grep-and-align pass.
5. Run `make build-binary && make test` and assert the package size
   shrinks net-negative (migrations + cleanup combined). This is a
   soft signal, not a hard assertion — just useful to record in the
   commit message.

## Tests

- No new tests. This task removes code; regressions show up as
  breakage in the existing test suite.
- Full `make test` must stay green, including the e2e lifecycle
  script for both sandboxes.

## Boundaries

- Do not extract or reshape anything new — deletion only.
- Do not touch the descriptors or `runAgent` themselves.
- Do not modify the turn loop, the commit pipeline, or any handler.
- If a dead-code candidate turns out to still have callers, leave it
  and note the missed callsite in the commit message — the sibling
  migration task is the right place to finish the extraction, not
  this one.
