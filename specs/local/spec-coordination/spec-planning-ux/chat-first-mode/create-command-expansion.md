---
title: Expand /create slash command to a /spec-new directive
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/spec-new-directive-parser.md
affects:
  - internal/planner/commands.go
  - internal/planner/commands_templates/create.tmpl
  - internal/handler/planning.go
  - internal/handler/planning_directive.go
effort: small
created: 2026-04-12
updated: 2026-04-13
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

## Implementation notes

- Collision resolution lives in the handler (`resolveUniqueSpecPath`) rather than in `Slugify`. `Slugify` stays pure — `(title) → slug` — which keeps the template FuncMap simple and makes it trivially testable. The handler already has the workspace root needed to check for existing files; teaching the slugger about workspaces would force the CommandRegistry to accept one too, for no real gain.
- The "safer" path from the spec is the one taken: the server scaffolds the file before the agent runs, strips the `/spec-new` line from the expanded prompt, and prepends a `[Focused spec: …]` hint so the agent targets the freshly created file. The directive scanner in the post-stream handler still fires if the agent echoes the directive, but the scaffold collision check there turns any duplicate into a benign `system`-role message; it does not corrupt the already-written spec.
- `TestSendPlanningMessage_SlashCreate_*` integration tests are not included for the same reason noted on `spec-new-directive-parser.md`: the planner sandbox is not mocked in the handler test harness. The function-level tests (`TestApplySlashSpecNew_*`, `TestResolveUniqueSpecPath_*`, `TestSlashCreate_ExpandsToDirective`, and the `TestSlugify_*` set) cover every branch that integration tests would exercise.
- An empty-title edge case (`TestSlugify_BasicCases` covers "" → "") surfaces through `applySlashSpecNew` as a 400: when the slug is empty the template produces `specs/local/.md`, the handler detects the empty filename stem, and returns a clear error before touching the filesystem.
