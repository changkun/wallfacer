---
title: Expand /create slash command to a /spec-new directive
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/spec-new-directive-parser.md
affects:
  - internal/planner/commands.go
  - internal/planner/commands_templates/
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Expand /create slash command to a /spec-new directive

## Goal

Route the user-driven `/create <title>` command through the same `spec.Scaffold` library call as the agent-driven `/spec-new` directive. Forces a scaffold regardless of the agent's intent judgment, giving users an explicit opt-in path when they want a spec.

## What to do

1. Update `internal/planner/commands_templates/create.tmpl` (the existing `/create` expansion template) so its expanded prompt includes a literal `/spec-new` directive as the first line of what the agent sees, followed by a one-line instruction to write a first-draft body:
   ```
   /spec-new specs/local/{{ slugify .Args }}.md title="{{ .Args }}"
   User requested a spec with title "{{ .Args }}". Write a first-draft body for it below.
   ```
2. Add a `slugify` template helper in `internal/planner/commands.go`: lowercases, strips non-alphanumeric, collapses runs of `-`, trims to 48 chars. De-dup with numeric suffixes if `specs/local/<slug>.md` already exists (append `-2`, `-3`, etc.).
3. Because `/spec-new` is a directive the SERVER recognises (from `spec-new-directive-parser.md`), the flow is: user types `/create Auth` → slash expansion produces a prompt starting with `/spec-new specs/local/auth.md title="Auth"` → planner sends this as the user's message to the agent → agent either echoes the directive verbatim and writes body content, OR writes different body content but the server's directive scanner still picks up the directive from the agent's output because the agent tends to repeat the scope line it was given.

   Alternative (safer): have the handler treat a user message starting with `/spec-new` after slash-expansion as a direct server-side scaffold, without relying on the agent to echo it. Implement this second path: in `SendPlanningMessage`, after slash expansion, if the expanded prompt's first line is `/spec-new ...`, parse it and call `spec.Scaffold` immediately (before the agent exec), inject the resulting path as the focused spec for the agent to populate. This avoids depending on the agent's echo behaviour.

## Tests

- `internal/planner/commands_test.go` (extend):
  - `TestSlashCreate_ExpandsToDirective`: `/create Auth Refactor` expands to a prompt starting with `/spec-new specs/local/auth-refactor.md title="Auth Refactor"`.
  - `TestSlugify_BasicCases`: "Auth Refactor" → "auth-refactor"; "Hello, World!" → "hello-world"; "" → error.
  - `TestSlugify_LengthCapped`: input >48 chars → truncated cleanly at a word boundary.
  - `TestSlugify_Collision`: if `specs/local/auth.md` exists, `/create Auth` produces `specs/local/auth-2.md`.
- `internal/handler/planning_test.go` (extend):
  - `TestSendPlanningMessage_SlashCreate_ScaffoldsDirectly`: POST body `{message: "/create Auth"}` → a file is scaffolded at the computed path BEFORE the agent exec, and the agent's system-prompt is extended to reference the scaffolded path.
  - `TestSendPlanningMessage_SlashCreate_InvalidTitle`: `/create` (no args) → 400 with a clear error; no scaffold.

## Boundaries

- **Do NOT** change the user-facing semantics of `/create`: it still creates a spec from the user's intent. Only the plumbing changes to route through `spec.Scaffold`.
- **Do NOT** require the user to specify a path. The slug is derived from the title argument.
- **Do NOT** remove the directive-scanner path from `spec-new-directive-parser.md`. `/create` is an additional entry point; the parser-based path still handles agent-originated directives.
- **Do NOT** add new slash commands beyond `/create` in this task. Other commands are unchanged.
