---
title: update_task_prompt Tool and Task-Mode System Prompt
status: validated
depends_on:
  - specs/local/refinement-into-plan/message-schema-task-mode.md
  - specs/local/refinement-into-plan/prompt-round-events.md
affects:
  - internal/handler/planning.go
  - internal/runner/refine.go
  - internal/prompts/refinement.tmpl
  - internal/prompts/
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# update_task_prompt Tool and Task-Mode System Prompt

## Goal

Give the planning sandbox a tool to write `task.Prompt` when in task mode, and frame the turn with the refinement system prompt so the agent knows its job is reshaping prompts rather than editing code.

## What to do

1. **Tool registration.** Register `update_task_prompt` through the existing sandbox HTTP bridge used by planning (the same bridge that exposes git and spec operations to the agent). Input schema: `{task_id: string, prompt: string}`. Output: `{prev_prompt: string, round: int}`.
2. **Handler.** When the tool is invoked, resolve the pinned task from the thread's `SessionInfo`. Reject with an error if:
   - the thread is not in task mode,
   - the tool's `task_id` does not match the pinned task,
   - the target task is no longer in an allowed state (see task-lock-and-cascade spec for the rule; for this task assume backlog and waiting are allowed).
3. **Write path.** On success, atomically update `task.Prompt` and append a `prompt_round` event with `prev_prompt`, `new_prompt`, thread id, round counter, and `resume_hint=true` if the task's status is `waiting`.
4. **Tool manifest branching.** For task-mode threads, strip file-edit tools (Edit, Write, etc.) from the tool manifest so the agent cannot wander off into the workspace repo. Keep Read available for context.
5. **System prompt.** Rename `internal/prompts/refinement.tmpl` to `internal/prompts/task_prompt_refine.tmpl` (or a similarly named template under the prompts tree) and render it as the system prompt fragment when a planning turn runs in task mode. The existing `buildRefinementPrompt` prose moves here.
6. **Parameterize.** The template receives `{Prompt, CreatedAt, AgeDays, Status, UserInstructions}` as before. Pull the values from the pinned task rather than a standalone refine session.

## Tests

- `internal/handler/planning_test.go::TestUpdateTaskPromptTool_WritesPrompt` — tool call in a task-mode thread updates `task.Prompt` and appends a `prompt_round` event.
- `TestUpdateTaskPromptTool_WrongThreadMode` — tool error if the thread is file-mode.
- `TestUpdateTaskPromptTool_MismatchedTaskID` — tool error if the tool's task_id differs from the pinned one.
- `TestUpdateTaskPromptTool_ResumeHintOnWaiting` — event carries `resume_hint=true` when the target task is in `waiting`.
- Prompt render test confirming the old refinement template's fields are still populated under the new name.

## Boundaries

- Do NOT change the undo path (separate task).
- Do NOT add the in-flight turn lock yet (separate task).
- Do NOT delete `internal/runner/refine.go` here; only extract the prompt text. The old pipeline is removed in the retirement task once every replacement is in place.
- Keep file-edit tool stripping narrow: only task-mode threads. Spec-mode tool manifest is untouched.
