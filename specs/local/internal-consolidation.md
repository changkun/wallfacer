---
title: Internal consolidation — extract shared helpers and fix layer boundaries
status: draft
depends_on: []
affects:
  - internal/handler/
  - internal/runner/
  - internal/store/
  - internal/workspace/
  - internal/prompts/
  - internal/agentsession/
  - internal/executor/
effort: large
created: 2026-07-21
updated: 2026-07-21
author: changkun
dispatched_task_id: null
---

# Internal consolidation — extract shared helpers and fix layer boundaries

A codebase audit surfaced repeated logic and a few wrong-layer boundaries that
warrant coordinated refactoring rather than site-by-site edits. Each item below
is behavior-preserving by intent: the goal is a single source of truth for
logic that is currently copy-pasted, so a change to one site cannot silently
diverge from its siblings. Mechanical cleanups and bug fixes from the same audit
already landed on `main`; this spec covers only the changes that alter shared
structure and therefore need review before implementation.

Sequencing note: the runner terminal-state work touches the same files as the
already-fixed commit-message and PR-repo bugs; land it after those are settled
on `main` (they are). Confirm each extraction preserves behavior with the
existing package tests plus a focused test on the new helper.

## Theme 1 — Handler mutate-commit helper family

The single-spec lifecycle handlers and the prompt-round pipeline repeat a
`decode -> mutate -> commit-under-workspace-lock -> emit-event` shape.

- `runSpecTransition` helper for the lifecycle handlers.
  Sites: `internal/handler/specs.go:458` (`ValidateSpecTransition`), `:515`
  (`ForceCompleteSpec`), `:577` (`MarkStaleTransition`), `:633` (`UnstaleSpec`).
  The per-handler delta is the target state and validation predicate, which
  parameterize cleanly. Risk: medium.
- Consolidate `commitSpecTransition` into `commitSpecChanges`.
  Sites: `internal/handler/specs.go:200`, `:904`. Near-identical
  workspace-lock -> stage -> commit -> event. Risk: medium.
- `recordPromptRound` helper.
  Sites: `internal/handler/agentsession_tool.go:78`,
  `internal/handler/agentsession.go:139`. Identical prompt-round pipeline
  (count prompt_round events -> UpdateTaskPromptDirect -> NewPromptRoundEvent
  -> InsertEvent); the only difference is the context source, which is a
  parameter. `planning_undo.go:434` shares only the first call and is NOT
  folded in. Risk: medium.

## Theme 2 — Runner terminal-state transitions

Best-effort terminal sequences are hand-repeated across the runner.

- `(*Runner).failTask(ctx, taskID, category, errEvent)` helper.
  Sites: `internal/runner/execute.go:163, 234, 257, 357, 506, 633`,
  `internal/runner/agentic.go:220`. The sequence
  (SetTaskFailureCategory -> UpdateTaskStatus(Failed) -> InsertEvent(Error) ->
  StateChange in_progress->failed) repeats ~7 times; only the FailureCategory
  and error text vary. Consolidating guarantees a new fail site cannot forget
  the StateChange event. Risk: medium.
- Shared auto-push gate helper.
  Sites: `maybeAutoPush` and `MaybeAutoPushWorkspace` in
  `internal/runner/commit.go` duplicate the entire gate. Risk: medium.

## Theme 3 — One-shot LLM-call + NDJSON-parse pipeline

`runOneShotContainer` (runner) and `ndjson.PreferResultLine` already encapsulate
launch/drain/parse, but several call sites bypass them.

- Route `GenerateAgentSessionTitle` (`internal/runner/title.go:83`) through
  `runOneShotContainer`.
- Replace the three hand-rolled backward result-line scans in
  `internal/agentsession/conversation.go:329` with `ndjson.PreferResultLine`.
- Fold the four `internal/executor/host_*_test.go` launch/drain helpers into one
  shared test helper.
  Net effect: removes duplication and adopts already-tested paths. Risk: medium.

## Theme 4 — Single source of truth and layer boundaries

- Prompts template-name set is encoded four times (`embeddedToAPI`,
  `apiToEmbedded`, `knownNames`, `mockContextFor` in
  `internal/prompts/prompts.go`). Define one canonical
  `[]struct{Embedded, API string}` table and derive the maps, known-names set,
  and mock-context switch from it; add a round-trip completeness test. Risk:
  medium.
- Wrong-layer boundary: `WorkspaceDataKey`/`NewDataKey`
  (`internal/prompts/instructions.go:28`) compute workspace storage identity but
  live in the prompts (agent-template) package; the workspace package imports
  prompts solely for these (an inverted dependency). Move them into
  `internal/workspace` (or a small storage-identity package). Risk: medium.
- `updateTaskStatus` core shared by `UpdateTaskStatus` / `ForceUpdateTaskStatus`
  (`internal/store/tasks_update.go:15`, `:127`). Extract a private core taking a
  `force bool` (or a validate predicate) so persistence stays single-sourced.
  Risk: medium (the state-machine guard is exactly what differs).
- Byte-identical alias functions to collapse (mechanical, low risk):
  `visibilityPrincipal` vs `ownerPrincipal` (`internal/handler/workspace_crud.go:70`),
  `Registry.Leave` vs `LeaveRegistration` (`internal/coordinator/registry.go:133`).
  (`getenvOr`, `cloneTask`/`deepCloneTask` were already handled or intentionally
  retained.)
- `internal/agentsession` `selectActiveThread` helper: the "clear active, then
  set active to the first non-archived thread" selection is copied three times
  in `sessions.go`. Risk: low.

## Out of scope (tracked separately)

- StorageBackend abstraction leak in usage I/O (`internal/store/turn_usage.go`):
  turn-usage and agent-session usage hardcode filesystem paths, bypassing the
  StorageBackend abstraction, so a non-filesystem backend would lose usage data.
  Needs a decision on whether usage is intentionally filesystem-pinned before
  refactoring; kept out of this spec.

## Acceptance criteria

- Each extracted helper has a single definition and a focused unit test.
- No behavior change: the affected packages' existing test suites pass
  unchanged, and `make build` / `make lint` stay green.
- The prompts template-name table has a round-trip test that fails if a template
  is added to only one derived structure.
