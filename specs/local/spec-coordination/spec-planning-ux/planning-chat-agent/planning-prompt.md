---
title: Planning System Prompt
status: validated
track: local
depends_on: []
affects:
  - internal/prompts/planning.tmpl
  - internal/prompts/prompts.go
  - internal/prompts/prompts_test.go
effort: small
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Planning System Prompt

## Goal

Create the system prompt template that establishes the planning agent's role,
permissions, and conventions. This is mounted into the planning container as
a CLAUDE.md file so Claude Code operates as a spec writer rather than a code
implementer.

## What to do

1. Create `internal/prompts/planning.tmpl` with the planning agent system
   prompt. The template should include:

   - **Identity**: "You are a spec planning assistant. You help design,
     break down, summarize, and validate design specs."
   - **Permissions**: "You can read all workspace files. You may only write
     to files under `specs/` directories."
   - **Conventions**: Spec document model rules — required frontmatter fields
     (`title`, `status`, `depends_on`, `affects`, `effort`, `created`,
     `updated`, `author`, `dispatched_task_id`), valid status values
     (`vague`, `drafted`, `validated`, `complete`, `stale`), DAG rules
     (no cycles in `depends_on`), track organization (`foundations/`,
     `local/`, `cloud/`, `shared/`).
   - **Entry-point maintenance**: "When you change a spec's status, also
     update the status in `specs/README.md` to keep it consistent."
   - **Output style**: "Be concise. When creating or editing spec files, use
     proper YAML frontmatter."

2. Register the template in `internal/prompts/prompts.go`:
   - Add `"planning.tmpl"` to the embedded filesystem
   - Add the API name mapping: `"planning.tmpl"` -> `"planning"`
   - Add a `RenderPlanning()` method on Manager (no template data needed
     for v1 — the focused spec context is prepended per-message, not baked
     into the system prompt)

3. Add the planning prompt to the `TestTemplatesHaveRequiredFields` test
   or equivalent validation ensuring the template parses without error.

## Tests

- `TestRenderPlanning` — verify the template renders without error and
  contains key phrases ("spec", "planning", "specs/")
- Verify the existing `TestTemplatesHaveRequiredFields` passes with the
  new template included

## Boundaries

- Do NOT implement slash command templates here — that's the
  slash-command-registry task
- Do NOT modify the planning container mount logic — the template is just
  a file; mounting is already handled by `planner/spec.go`
- Keep the template simple — it will be iterated on as the chat agent is
  used in practice
