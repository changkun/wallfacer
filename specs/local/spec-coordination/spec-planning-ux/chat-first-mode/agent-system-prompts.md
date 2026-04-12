---
title: Planning agent system prompt variants for empty vs non-empty tree
status: validated
depends_on: []
affects:
  - internal/prompts/
  - internal/handler/planning.go
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Planning agent system prompt variants

## Goal

Select the planning agent's system prompt per-turn based on whether the spec tree is effectively empty (zero non-archived parseable specs). Empty tree gets a prompt that encourages emitting `/spec-new` directives for substantive planning work; non-empty tree gets a prompt that prefers editing existing specs.

## What to do

1. Add two new template files under `internal/prompts/`:
   - `planning_system_empty.tmpl` — the empty-tree variant from the parent spec:
     > The user is planning from a clean slate. If their message is substantive design or planning work — a feature request, a refactor proposal, an investigation with an outcome, a new document worth drafting — emit a single `/spec-new` directive on its own line, followed by the body of the spec. Pick a slugged path under specs/local/ (or the appropriate track) that describes the work. The server will create the file with valid frontmatter; you only need to write the body.
     >
     > For casual conversation, questions about Wallfacer itself, clarifying questions, or anything that doesn't produce a durable design document, respond conversationally without any /spec-new directive.
   - `planning_system_nonempty.tmpl` — the non-empty variant:
     > Relevant specs already exist in this workspace. Prefer editing existing specs (using your Write/Edit tools) over creating new ones. Only emit a `/spec-new` directive if the user's request clearly introduces an entirely new concern that doesn't fit into any existing spec.
2. Register both templates in the prompt manager (`internal/prompts/manager.go` or equivalent) so they're overridable via `/api/system-prompts/{name}`.
3. In `internal/handler/planning.go:SendPlanningMessage`, before building the agent exec args:
   - Query `spec.ResolveIndex` / `spec.ParseTree` (existing helpers) for the current workspaces.
   - Count non-archived parseable specs. Zero → load `planning_system_empty`; else → `planning_system_nonempty`.
   - Prepend the rendered template to the user's prompt (same pattern as the existing `archivedSpecGuard` prefix).
4. Expose an internal helper `selectPlanningSystemPrompt(workspaces []string) (string, error)` so handler code doesn't have to inline the tree-count logic.

## Tests

- `internal/handler/planning_test.go` (extend):
  - `TestSelectPlanningSystemPrompt_EmptyTree`: zero non-archived specs → returns the `empty` variant.
  - `TestSelectPlanningSystemPrompt_NonEmptyTree`: at least one non-archived spec → `nonempty` variant.
  - `TestSelectPlanningSystemPrompt_IgnoresArchived`: a workspace with only archived specs → `empty` variant.
  - `TestSendPlanningMessage_PrependsEmptyVariant`: with zero specs, the exec's `-p` argument starts with the empty-variant prompt text (or a unique substring from it).
  - `TestSendPlanningMessage_PrependsNonemptyVariant`: same but with non-empty tree.
- `internal/prompts/*_test.go`:
  - `TestPromptRegistry_RegistersPlanningEmpty` / `Nonempty`: the two new templates are present in the registry.
  - `TestPromptRegistry_PlanningOverridable`: `/api/system-prompts/planning_system_empty` read/write cycle works like other user-overridable prompts.

## Boundaries

- **Do NOT** implement the `/spec-new` directive parser yet — that's `spec-new-directive-parser.md`. This task only wires the prompt; the agent output will describe the intent, and the parser will pick it up in the next task.
- **Do NOT** change the existing `archivedSpecGuard` behaviour. The new prompt prepend sits alongside it.
- **Do NOT** cache the empty/non-empty decision across turns. Check per-turn so archiving the last spec takes effect on the very next message.
- **Do NOT** change the interrupt, undo, or commit-pipeline paths. Only the exec prompt is modified.
