---
title: Slash Command Registry
status: complete
track: local
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-chat-agent/message-api.md
affects:
  - internal/planner/commands.go
  - internal/planner/commands_test.go
  - internal/prompts/planning/
  - internal/handler/planning.go
  - internal/apicontract/routes.go
effort: medium
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Slash Command Registry

## Goal

Implement the `/command` system for the planning chat. Slash commands expand
into structured prompts that guide the agent to perform specific actions
(summarize, break down, create specs, etc.). The UI sends the raw `/command`
text; the server expands it before passing to the agent.

## What to do

1. Create `internal/planner/commands.go` with the command registry:

   ```go
   type Command struct {
       Name        string `json:"name"`
       Description string `json:"description"`
       Usage       string `json:"usage"` // e.g. "/summarize [words]"
   }

   type CommandRegistry struct {
       commands map[string]Command
       prompts  *prompts.Manager
   }
   ```

2. Implement `NewCommandRegistry(pm *prompts.Manager) *CommandRegistry`
   that registers the built-in commands:
   - `/summarize` — "Produce a structured summary of the focused spec"
   - `/break-down` — "Decompose the focused spec into sub-specs or tasks"
   - `/create` — "Create a new spec file with proper frontmatter"
   - `/status` — "Update the focused spec's status"
   - `/validate` — "Check the focused spec against document model rules"
   - `/impact` — "Analyze what code and specs would be affected"
   - `/dispatch` — "Prepare the focused spec for dispatch to the task board"

3. Implement `Expand(input, focusedSpec, treeJSON string) (string, bool)`:
   - If `input` starts with `/`, extract the command name and args
   - Look up the command in the registry
   - Render the corresponding template with context (focused spec path,
     args, tree summary)
   - Return the expanded prompt and `true`, or the original input and
     `false` if not a slash command

4. Implement `Commands() []Command` — returns all registered commands
   for the UI autocomplete.

5. Create slash command templates in `internal/prompts/planning/`:
   - `summarize.tmpl` — "Summarize the spec at {{.FocusedSpec}} in
     {{.WordLimit}} words or fewer. Output a structured summary."
   - `breakdown.tmpl` — "Break down the spec at {{.FocusedSpec}} into
     implementable sub-specs or tasks. Follow the spec document model."
   - `create.tmpl` — "Create a new spec file titled '{{.Title}}' in the
     appropriate track directory with proper YAML frontmatter."
   - `status.tmpl` — "Update the status of {{.FocusedSpec}} to '{{.State}}'.
     Also update specs/README.md to reflect the change."
   - `validate.tmpl` — "Validate {{.FocusedSpec}} against the spec document
     model rules. Check frontmatter, DAG, naming, and status consistency."
   - `impact.tmpl` — "Analyze what existing code and specs would be affected
     by implementing {{.FocusedSpec}}."
   - `dispatch.tmpl` — "Prepare {{.FocusedSpec}} for dispatch to the task
     board. Set dispatched_task_id in the frontmatter."

6. Add route to `internal/apicontract/routes.go`:
   ```go
   {GET, "/api/planning/commands", "GetPlanningCommands", "commands", "List available slash commands.", {"planning"}},
   ```

7. Implement `Handler.GetPlanningCommands(w, r)`:
   - Return the command list from the registry as JSON

8. Integrate expansion into `SendPlanningMessage` (from message-api):
   - Before building exec args, call `registry.Expand(message, focusedSpec, treeJSON)`
   - Use the expanded prompt as the `-p` argument

9. Register handler in `BuildMux()` and run `make api-contract`.

## Tests

- `TestCommandRegistry_Expand_SlashCommand` — verify `/summarize 100` expands
  to a prompt containing the word limit and focused spec path
- `TestCommandRegistry_Expand_NotSlashCommand` — verify "hello world" returns
  the original input and `false`
- `TestCommandRegistry_Expand_UnknownCommand` — verify `/unknown` returns
  the original input and `false`
- `TestCommandRegistry_Commands` — verify all 7 built-in commands are listed
- `TestGetPlanningCommands` — HTTP test, verify JSON response contains all
  commands with name, description, usage fields

## Boundaries

- Do NOT implement UI autocomplete here — that's ui-chat-send-stream
- Do NOT add user-defined custom commands — only built-in commands for now
- Templates should be simple and focused — they'll be iterated on in practice
