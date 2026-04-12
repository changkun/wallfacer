---
title: Planning agent system prompt variants for empty vs non-empty tree
status: complete
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

## Implementation notes

1. **Templates genericized for project-agnostic use.** The spec text listed Wallfacer-specific track names in the empty-tree variant (`specs/foundations/`, `specs/local/`, etc.). During implementation the user pointed out that Wallfacer is a tool for building *any* project, not just itself — baking Wallfacer's own roadmap taxonomy into system prompts would mislead the agent when used against other repos. The shipped templates instruct the agent to pick `specs/<track>/<slug>.md` where `<track>` matches the repo's existing directory structure, falling back to `specs/default/` when none is present.

2. **Template registration moved slugs `planning_system_empty` / `planning_system_nonempty`.** The spec named them without specifying whether the `api` surface uses `planning_empty` vs `planning_system_empty` vs similar. Shipped with the fully-qualified `planning_system_*` names so they sort together with the existing `planning` template in prompt listings and don't collide with any future `planning_*` template namespace.

3. **Prepend order.** The spec said "Prepend the rendered template to the user's prompt (same pattern as the existing `archivedSpecGuard` prefix)." Implementation places the planning system prompt *inside* the archivedSpecGuard (i.e. archivedSpecGuard wraps the planning prompt wraps the user message), so a focused-archived-spec warning remains closest to the user's words. This matches the UX intent — the archived-spec guard is the highest-priority safety rail and must not be hidden behind other planning instructions.

4. **Existing `planning.tmpl` left unchanged.** `prompts.Planning()` is defined but not currently wired into the exec path (the planner's container boots with AGENTS.md mounted; Claude Code consumes that directly). Rather than retiring `planning.tmpl` in this task, the two new `planning_system_*` templates land alongside it. A follow-up cleanup can decide whether to delete `planning.tmpl` or repurpose it.

5. **Tree-count check uses an early-exit.** `selectPlanningSystemPrompt` iterates `tree.All` once, breaking on the first non-archived spec found. Avoids counting the full tree for a boolean question on repos with hundreds of specs.

6. **`internal/handler/prompts_test.go` count bumped 9 → 11.** Adding two registered templates bumps the count in the existing "list all system prompts" test — a mechanical adjustment, not a behavior change.
