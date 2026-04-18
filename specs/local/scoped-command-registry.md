---
title: Scoped Slash Command Registry
status: drafted
depends_on: []
affects:
  - internal/planner/commands.go
  - internal/planner/commands_templates/
  - internal/handler/planning.go
  - internal/apicontract/routes.go
  - ui/js/planning-chat.js
  - ui/js/tasks.js
  - ui/js/mention.js
effort: medium
created: 2026-04-12
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Scoped Slash Command Registry

## Overview

Slash commands (`/summarize`, `/break-down`, `/validate`, ...) today live in `internal/planner/commands.go` and are served only at `GET /api/planning/commands`. The planning chat is the only UI that can trigger them. This is a historical accident, not a design decision — the registry was built where the first consumer happened to live.

This spec promotes the command registry to a cross-surface mechanism: commands declare which UI surfaces they apply to ("planning", "task create", "task waiting", ...), and each surface queries the registry scoped to itself. The immediate motivator is letting the task board's prompt inputs (`#new-prompt`, `#modal-edit-prompt`, `#modal-retry-prompt`) trigger commands that make sense when drafting a task, without dragging planning-only commands along with them.

## Current State

`internal/planner/commands.go` holds a hardcoded list of 12 commands, each tied to a template in `commands_templates/*.tmpl` that expands into a planning prompt. The registry is planning-specific in two senses:

- **Lives under `internal/planner/`** — the package is named and organized around planning.
- **Template context is planning-shaped.** Templates reference `FocusedSpec`, which only makes sense when a spec is open in the planning view.

`GET /api/planning/commands` returns the full list. The planning chat UI calls it once on init, caches it, and uses it to drive the shared autocomplete widget (`ui/js/lib/autocomplete.ts`, introduced in commit `5b144d74`).

The task board UI uses the same widget for `@` file mentions but does not wire `/` at all. There is no equivalent registry nor any surface-scoping concept.

## Problems

1. **Commands are trapped in the planning namespace.** Adding a task-board command (e.g. `/template foo` to expand a saved prompt template) requires inventing a parallel registry or awkwardly extending the planning one.
2. **No scoping model.** Even if the task board queried `/api/planning/commands`, it would see planning-only commands (`/impact`, `/wrapup`) that reference `FocusedSpec` and have no meaning on the board.
3. **Template context is monolithic.** The `expandData` struct mixes variables from different surfaces (`FocusedSpec`, `Args`, `WordLimit`, `Title`, `State`). Adding a task-board command with its own context (`WorkspaceGroup`, `TaskID`) means either bloating the struct or forking the expansion path.
4. **Discovery is one-off.** The only way to see "what commands exist and where" is to read `commands.go`. No CLI or API surfaces the catalog with scope metadata.
5. **Hidden coupling to planning handler.** The registry is instantiated inside `internal/handler/planning.go`'s setup. Other handlers that want commands (task board, waiting-task feedback) have no clean way to consume the same registry.

## Design Direction

Extract the registry into a surface-agnostic package and attach each command to one or more **scopes**. A scope is a stable string that identifies a UI surface or invocation context. Each surface queries the registry scoped to itself and only sees commands relevant to it.

### Scopes (initial set)

| Scope | Surface | Context vars available |
|-------|---------|------------------------|
| `planning` | Spec-mode chat composer | `FocusedSpec`, `WorkspaceGroup`, `ThreadID` |
| `task_create` | Task board prompt inputs (new / edit / retry) | `WorkspaceGroup`, `Args` |
| `task_waiting` | Feedback composer on waiting tasks | `TaskID`, `WorkspaceGroup`, `Args` |

The scope set is extensible — future surfaces (e.g., `oversight_followup`, `spec_explorer_quickaction`) register their own. Each scope declares the context variables it supplies; templates that reference variables the scope doesn't supply are rejected at registration time.

### Command shape

A command declares:

- `name` — the slash token (`summarize`, `break-down`).
- `description` — one-line help text shown in the dropdown.
- `usage` — `/summarize [words]` style hint.
- `scopes` — list of scopes where this command is visible. Most commands target exactly one scope; shared commands (e.g., a hypothetical `/help`) may list several.
- `template` — the prompt template file, still under `commands_templates/`.

Commands that live in multiple scopes must only reference context variables available in the **intersection** of those scopes' contexts. The registry validates this at load time.

### API

Replace (or alias) `GET /api/planning/commands` with `GET /api/commands?scope=<scope>`. The response is unchanged in shape (a list of `{name, description, usage}`); what changes is that the caller selects which slice of the catalog to receive.

Expansion remains server-side: each handler that accepts input from its surface calls `registry.Expand(input, scope, contextVars)` which chooses the right template and renders it. The planning flow continues to expand at message-send time; the task-board flow expands at task-create time before the prompt is persisted and sent to the sandbox.

### Where the registry lives

Move `internal/planner/commands.go` to `internal/commands/` as a package with no dependency on planner state. The planner constructs a `CommandContext{scope: "planning", focused_spec: ...}` when expanding; the task-board handler builds its own `CommandContext{scope: "task_create", workspace_group: ...}`. Templates move with the registry.

### UI integration

The shared autocomplete widget (`ui/js/lib/autocomplete.ts`) already supports arbitrary `fetchItems`. Each surface's attach site points its `fetchItems` at the scoped endpoint:

```js
attachAutocomplete(input, {
  shouldActivate: (ta) => startsWithSlashOnLine(ta),
  fetchItems: () => api('/api/commands?scope=task_create'),
  renderItem: renderCommandRow,
  onSelect: replaceInputWithUsage,
});
```

No new widget work is required.

## Components

### `internal/commands/` package

- `Registry` with scope-indexed storage.
- `Command` struct as above.
- `Register(cmd Command)` validates the template parses and that its referenced variables are a subset of the union of its scopes' context schemas.
- `Commands(scope string) []Command` for UI.
- `Expand(scope string, input string, ctx Context) (string, bool)` for handlers. `Context` is a typed struct per scope (planning, task_create, task_waiting), or a map with declared variables — choice deferred to implementation.

### Scope catalog

A small file (`internal/commands/scopes.go`) enumerates the valid scopes and the context variables each provides. Adding a new scope is a code change; new commands inside an existing scope are not.

### Migration

The 12 existing planning commands move to `internal/commands/` with `scopes: ["planning"]`. The planning handler continues to call `registry.Commands("planning")` and `registry.Expand("planning", ...)`; behaviour for existing callers is preserved.

`GET /api/planning/commands` remains as an alias (or redirects to `/api/commands?scope=planning`) for a deprecation window, then is removed. UI code is updated alongside.

### New task-board commands (out of spec, but motivating example)

Once the registry is scoped, the task board can adopt commands like:

- `/template <name>` — expand a saved prompt template from `/api/templates`.
- `/describe <path>` — inject a short description of a file or dir.
- `/dependency <task-prefix>` — add a dependency by task ID.

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
- UI smoke:
  - Task board `/` autocomplete opens a dropdown populated from `scope=task_create`.
  - Planning `/` autocomplete is unchanged.

## Open Questions

1. **Naming.** `CommandContext` vs `expandData` vs a per-scope typed struct. Typed structs are safer but require a type switch in `Expand`. A map + declared schema is more flexible but deferred validation.
2. **Command name collisions across scopes.** Two scopes both registering `/validate` with different templates — allow (each scope sees its own) or forbid (global uniqueness)? Leaning toward **allow** since scopes are surfaces and the name appears only in one surface at a time from the user's perspective.
3. **User-defined commands.** Out of scope here, but the registry should leave room for it — overlaps with [extensible-prompts.md](../shared/extensible-prompts.md) and could be the eventual integration point.
4. **Cross-scope inheritance.** Should a `task_waiting` scope inherit the `task_create` context? Probably not — keep scopes flat and explicit, and let shared variables (like `WorkspaceGroup`) appear in each scope's schema.
