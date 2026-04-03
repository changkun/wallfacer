---
title: Extensible Prompt System
status: vague
depends_on: []
affects:
  - internal/prompts/
  - internal/handler/systemprompts.go
  - internal/runner/
effort: large
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Extensible Prompt System

## Overview

Rethink `internal/prompts/` so that prompts are a discoverable, user-creatable resource rather than a hardcoded set compiled into the binary. Today, adding a new prompt requires touching ~6 files (`.tmpl`, data struct, Manager method, API name maps, handler, mock context). The system should allow creating, editing, and composing prompts dynamically — closer to Claude Code's skill system where capabilities are defined as standalone files that the system discovers at runtime.

## Current State

The prompt system (`internal/prompts/`) embeds 8 `.tmpl` files at compile time via `go:embed`. Each template has:

- A fixed entry in `embeddedToAPI` / `apiToEmbedded` name maps (`prompts.go:74-95`)
- A typed Go data struct (e.g., `RefinementData`, `IdeationData`) defining the template context
- A dedicated `Manager.Render*()` method
- A `mockContextFor()` case for validation dry-runs
- Package-level wrapper functions for backward compatibility

Users can override templates by placing files in `~/.wallfacer/prompts/<name>.tmpl`, but they cannot create new prompts, change the input schema, or compose prompts from fragments.

The `internal/handler/systemprompts.go` exposes CRUD for the 8 known templates only — the API rejects unknown names.

## Problems

1. **Closed set.** The system only knows about 8 templates. New agent roles (planning, gate-checking, code review, etc.) each require code changes to register.
2. **Rigid schemas.** Each template's input is a fixed Go struct. Users cannot add fields, reuse fragments across templates, or define their own context variables.
3. **No composition.** Templates are monolithic. Common patterns (workspace context blocks, safety preambles, output format instructions) are duplicated across templates rather than shared.
4. **No discovery.** The system cannot enumerate available prompts at runtime beyond the hardcoded `knownNames` list. There is no mechanism to browse, search, or understand what prompts exist and what they do.
5. **Tight coupling to runner.** Each prompt is consumed by exactly one call site in the runner. The mapping from "agent role" to "which prompt to use" is implicit in code, not declarative.

## Design Direction

Take inspiration from Claude Code's skill system:

- **Skills as files.** Each skill is a standalone markdown/template file with frontmatter declaring its name, description, trigger conditions, and input schema. The system discovers skills by scanning a directory.
- **User-creatable.** Users drop a file in a known directory and the system picks it up — no code changes, no recompilation.
- **Composable.** Skills can reference other skills or include shared fragments.
- **Discoverable.** The system can list all available skills with their descriptions and trigger conditions.

### Key Design Questions

- **Schema flexibility vs type safety.** Go's `text/template` requires a concrete data type. How do we allow user-defined fields while maintaining template validation? Options: map-based context, schema-in-frontmatter, JSON Schema validation, or a lightweight DSL.
- **Fragment composition.** How do prompts include shared blocks? Go templates support `{{template "name"}}` but all templates must be in the same parse tree. Consider a preprocessing step or a partial-include mechanism.
- **Trigger/routing.** How does the system decide which prompt to use for a given agent invocation? Today this is hardcoded per role. A skill-like model would use declarative trigger conditions.
- **Backward compatibility.** The 8 existing templates must continue to work. Migration path from typed structs to a flexible schema.
- **Relationship to agent-abstraction.** The `agent-abstraction` spec (which also affects `internal/prompts/`) introduces a `AgentRole` descriptor that references prompt templates by name. This spec should define the extensible prompt layer that agent-abstraction consumes. The two specs are complementary: agent-abstraction is the consumer, this spec is the provider.

## Components

### Prompt Discovery

A scanner that discovers prompt definitions from:
1. Embedded defaults (the current 8, for backward compatibility)
2. User prompt directory (`~/.wallfacer/prompts/`)
3. Workspace-scoped prompts (per-project prompt overrides)

Each prompt is a file with structured frontmatter (name, description, input schema, output format, tags) and a template body.

### Schema System

A lightweight schema mechanism so prompts can declare their expected input variables and the system can validate that callers provide them. This replaces the current typed Go structs with something more flexible.

### Fragment Library

Reusable prompt fragments (safety preambles, output format blocks, workspace context injection) that can be included by any prompt. Reduces duplication across the current 8 templates.

### Prompt API

Extend the existing system-prompt CRUD API to support user-created prompts, not just overrides of the 8 built-ins. Allow listing, creating, editing, and deleting prompts through the UI.

### Runner Integration

Decouple the runner from specific prompt names. Instead of `m.Refinement(data)`, the runner looks up prompts by role tag or trigger condition, allowing the same extensible mechanism to serve both built-in and user-defined agent roles.

## Testing Strategy

- Unit tests for prompt discovery (scanning directories, merging embedded + user + workspace sources)
- Unit tests for schema validation (type-check template variables against declared schema)
- Integration tests for fragment composition (partial includes render correctly)
- Backward-compatibility tests: all 8 existing templates render identically after refactoring
- API tests for extended CRUD (create, list, delete user-defined prompts)
