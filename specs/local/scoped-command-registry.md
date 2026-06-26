---
title: Scoped Slash Command Registry
status: stale
depends_on: []
affects:
  - internal/planner/commands.go
  - internal/planner/commands_templates/
  - internal/handler/planning.go
  - internal/apicontract/routes.go
  - frontend/src/components/plan/PlanningChatPanel.vue
  - frontend/src/components/TaskComposer.vue
  - frontend/src/composables/usePlanningAutocomplete.ts
  - frontend/src/composables/useMentions.ts
effort: medium
created: 2026-04-12
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Scoped Slash Command Registry

## Overview

Slash commands (`/summarize`, `/break-down`, `/validate`, ...) today live in `internal/planner/commands.go` and are served only at `GET /api/planning/commands`. The planning chat is the only UI that can trigger them. This is a historical accident, not a design decision. The registry was built where the first consumer happened to live.

This spec promotes the command registry to a cross-surface mechanism: commands declare which UI surfaces they apply to ("planning", "task create", "task waiting", ...), and each surface queries the registry scoped to itself. The immediate motivator is letting the task board's prompt input (the composer in `frontend/src/components/TaskComposer.vue`, plus the edit and retry prompt inputs in the task detail flow) trigger commands that make sense when drafting a task, without dragging planning-only commands along with them.

## Current State

`internal/planner/commands.go` holds a hardcoded list of 12 commands, each tied to a template in `commands_templates/*.tmpl` that expands into a planning prompt. The registry is planning-specific in two senses:

- It lives under `internal/planner/`. The package is named and organized around planning.
- Its template context is planning-shaped. Templates reference `FocusedSpec`, which only makes sense when a spec is open in the planning view.

The concrete shape today is flat and has no scope concept anywhere:

- `Command` is `{ Name, Description, Usage string }`. There is no `Scopes` field and no `Template` field exposed on the public struct (the template filename lives on the internal `commandDef`).
- `Commands() []Command` takes no scope argument and returns every command sorted by name.
- `Expand(input, focusedSpec string) (string, bool)` takes the focused-spec string directly, not a context struct. Internally it builds an `expandData{ FocusedSpec, Args, WordLimit, Title, State }` and renders the matching template, returning the original input and `false` when the input is not a known slash command.
- The registry is a `*planner.CommandRegistry`, constructed in `internal/handler/handler.go` and stored on the handler as `commandRegistry`.

`GET /api/planning/commands` (declared in `internal/apicontract/routes.go`, `Name: "GetPlanningCommands"`, `JSName: "commands"`) returns the full list via `commandRegistry.Commands()`. The planning chat UI calls it on demand and caches it.

On the frontend, slash autocomplete lives in `frontend/src/composables/usePlanningAutocomplete.ts`. That composable fuses slash and `@`-mention handling into one keystroke handler and **hardcodes** the slash fetch at `/api/planning/commands` (caching the result). `PlanningChatPanel.vue` consumes it and renders the slash dropdown.

The task board composer (`TaskComposer.vue`) uses a different composable, `frontend/src/composables/useMentions.ts`, which is `@`-file-mention only and wires no slash path at all. Both composables share the pure matching helpers in `frontend/src/lib/mentions.ts`, but there is no single shared autocomplete widget that surfaces could repoint by passing a different `fetchItems`. There is no command registry for the board and no surface-scoping concept anywhere.

## Problems

1. **Commands are trapped in the planning namespace.** Adding a task-board command (e.g. `/template foo` to expand a saved prompt template) requires inventing a parallel registry or awkwardly extending the planning one.
2. **No scoping model.** Even if the task board queried `/api/planning/commands`, it would see planning-only commands (`/impact`, `/wrapup`) that reference `FocusedSpec` and have no meaning on the board.
3. **Template context is monolithic.** The `expandData` struct mixes variables from different surfaces (`FocusedSpec`, `Args`, `WordLimit`, `Title`, `State`). Adding a task-board command with its own context (`WorkspaceGroup`, `TaskID`) means either bloating the struct or forking the expansion path.
4. **Discovery is one-off.** The only way to see "what commands exist and where" is to read `commands.go`. No CLI or API surfaces the catalog with scope metadata.
5. **Hidden coupling to planning.** The registry type is `*planner.CommandRegistry` and depends on planner state. Other handlers that want commands (task board, waiting-task feedback) have no clean way to consume the same registry.

## Design Direction

Extract the registry into a surface-agnostic package and attach each command to one or more **scopes**. A scope is a stable string that identifies a UI surface or invocation context. Each surface queries the registry scoped to itself and only sees commands relevant to it.

### Scopes (initial set)

| Scope | Surface | Context vars available |
|-------|---------|------------------------|
| `planning` | Spec-mode chat composer (`PlanningChatPanel.vue`) | `FocusedSpec`, `Args` (matches today's `expandData`) |
| `task_create` | Task board composer and the edit / retry prompt inputs | `WorkspaceGroup`, `Args` (proposed) |
| `task_waiting` | Feedback composer on waiting tasks | `TaskID`, `WorkspaceGroup`, `Args` (proposed) |

The `planning` row lists only the variables the current `expandData` actually exposes to templates (`FocusedSpec` plus the args-derived `Args`/`WordLimit`/`Title`/`State`). The `task_create` and `task_waiting` rows are forward proposals; their context vars do not exist yet and are introduced by this work.

The scope set is extensible. Future surfaces (e.g., `oversight_followup`, `spec_explorer_quickaction`) register their own. Each scope declares the context variables it supplies; templates that reference variables the scope does not supply are rejected at registration time.

### Command shape

A command declares:

- `name`: the slash token (`summarize`, `break-down`).
- `description`: one-line help text shown in the dropdown.
- `usage`: `/summarize [words]` style hint.
- `scopes`: list of scopes where this command is visible. Most commands target exactly one scope; shared commands (e.g., a hypothetical `/help`) may list several. This field is new; the current `Command` struct has none.
- `template`: the prompt template file, still under `commands_templates/`.

Commands that live in multiple scopes must only reference context variables available in the **intersection** of those scopes' contexts. The registry validates this at load time.

### API

Replace (or alias) `GET /api/planning/commands` with `GET /api/commands?scope=<scope>`. The response is unchanged in shape (a list of `{name, description, usage}`); what changes is that the caller selects which slice of the catalog to receive.

Expansion remains server-side: each handler that accepts input from its surface calls `registry.Expand(scope, input, ctx)` which chooses the right template and renders it. The planning flow continues to expand at message-send time (`internal/handler/planning.go`); the task-board flow expands at task-create time before the prompt is persisted and sent to the executor.

### Where the registry lives

Move `internal/planner/commands.go` to `internal/commands/` as a package with no dependency on planner state. The planner constructs a `Context{Scope: "planning", FocusedSpec: ...}` when expanding; the task-board handler builds its own `Context{Scope: "task_create", WorkspaceGroup: ...}`. Templates move with the registry. The handler field changes from `*planner.CommandRegistry` to the new package's registry type.

### UI integration

There is no single shared autocomplete widget to repoint, so this work is not a zero-cost `fetchItems` swap. Today slash and mention live in two separate composables, and only the planning surface has a slash path. The frontend work is:

- Parameterize the slash fetch in `usePlanningAutocomplete.ts` so the scoped endpoint is an option instead of the hardcoded `/api/planning/commands`. The planning surface passes `scope=planning`.
- Give `TaskComposer.vue` a slash-capable path. It currently uses `useMentions.ts` (mention-only), so it needs either a slash-aware composable or the slash branch factored out of `usePlanningAutocomplete.ts` into a shared piece that both surfaces consume, pointed at `scope=task_create`.

A sketch of the planning call site after parameterization:

```ts
const autocomplete = usePlanningAutocomplete({
  inputEl,
  inputText,
  commandsScope: 'planning', // -> GET /api/commands?scope=planning
});
```

The task composer adopts the same slash composable with `commandsScope: 'task_create'`. The pure matching helpers in `frontend/src/lib/mentions.ts` are reused unchanged.

## Components

### `internal/commands/` package

- `Registry` with scope-indexed storage.
- `Command` struct as above, gaining a `Scopes` field.
- `Register(cmd Command)` validates the template parses and that its referenced variables are a subset of the union of its scopes' context schemas.
- `Commands(scope string) []Command` for UI.
- `Expand(scope string, input string, ctx Context) (string, bool)` for handlers. `Context` is a typed struct per scope (planning, task_create, task_waiting), or a map with declared variables. Choice deferred to implementation.

### Scope catalog

A small file (`internal/commands/scopes.go`) enumerates the valid scopes and the context variables each provides. Adding a new scope is a code change; new commands inside an existing scope are not.

### Migration

The 12 existing planning commands move to `internal/commands/` with `scopes: ["planning"]`. The planning handler continues to call `registry.Commands("planning")` and `registry.Expand("planning", ...)`; behaviour for existing callers is preserved.

`GET /api/planning/commands` remains as an alias (or redirects to `/api/commands?scope=planning`) for a deprecation window, then is removed. `usePlanningAutocomplete.ts` is updated alongside.

### New task-board commands (out of spec, but motivating example)

Once the registry is scoped, the task board can adopt commands like:

- `/template <name>`: expand a saved prompt template from `/api/templates`.
- `/describe <path>`: inject a short description of a file or dir.
- `/dependency <task-prefix>`: add a dependency by task ID.

These commands are not part of this spec; they are justifications for why the registry needs surface awareness.

## Testing Strategy

- Unit tests for `internal/commands/`:
  - Registration rejects commands referencing undeclared context variables.
  - `Commands(scope)` returns only commands tagged with that scope.
  - `Expand` renders with the right context; unknown slash names are passed through unchanged.
  - Multi-scope commands are returned for each of their scopes and render correctly under each.
- Handler tests:
  - `/api/commands?scope=planning` returns the same payload the old `/api/planning/commands` did (golden-file comparison against a snapshot).
  - Invalid scope returns 400.
- Migration regression:
  - All 12 existing planning commands expand to byte-identical output before and after the move.
- UI smoke (Vue, via the frontend test harness):
  - Task board `/` autocomplete opens a dropdown populated from `scope=task_create`.
  - Planning `/` autocomplete is unchanged.

## Open Questions

1. **Naming.** `Context` vs the existing `expandData` vs a per-scope typed struct. Typed structs are safer but require a type switch in `Expand`. A map plus declared schema is more flexible but defers validation.
2. **Command name collisions across scopes.** Two scopes both registering `/validate` with different templates: allow (each scope sees its own) or forbid (global uniqueness)? Leaning toward **allow** since scopes are surfaces and the name appears only in one surface at a time from the user's perspective.
3. **User-defined commands.** Out of scope here, but the registry should leave room for it. Overlaps with [extensible-prompts.md](../shared/extensible-prompts.md) and could be the eventual integration point.
4. **Cross-scope inheritance.** Should a `task_waiting` scope inherit the `task_create` context? Probably not. Keep scopes flat and explicit, and let shared variables (like `WorkspaceGroup`) appear in each scope's schema.
5. **Frontend factoring.** Whether to split the slash branch out of `usePlanningAutocomplete.ts` into a shared composable both surfaces import, or to add a separate slash composable for the board. Either keeps `frontend/src/lib/mentions.ts` as the shared pure-matching layer.
