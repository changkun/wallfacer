---
name: wf-spec-create
description: Create a new design spec in specs/. Gathers context, explores the codebase, writes the spec with proper frontmatter, and updates specs/README.md. Use when the user says "create a spec", "write a spec", "new spec", or "/spec".
argument-hint: <track/name> [one-line description...]
user-invocable: true
allowed-tools: Read, Grep, Glob, Edit, Write, Agent, Bash(ls *), Bash(mkdir *), Bash(git log *), Bash(git diff *)
---

# Create a Design Spec

Create a new design spec in `specs/` following the spec document model.

## Step 0: Parse arguments

$ARGUMENTS has the form: `<track/name> [description...]`

- The **first token** is the spec location: `<track>/<name>` where `<track>` is
  one of the directories under `specs/` (e.g., `local`, `cloud`, `shared`,
  `foundations`) and `<name>` is the kebab-case spec name without `.md`.
  Examples: `local/live-serve`, `shared/sandbox-hooks`, `cloud/tenant-api`.
- If only a name is given without a track, ask the user which track it belongs
  to. Explain the tracks briefly:
  - `foundations` — abstraction interfaces that all tracks build on
  - `local` — desktop experience and developer workflow (single-user)
  - `cloud` — cloud platform and multi-tenant deployment
  - `shared` — cross-track specs used by both local and cloud
- Everything after the first token is a **description** — a short explanation
  of what the spec should cover. If no description is provided, ask the user
  what the spec should address.

Derive the output file path: `specs/<track>/<name>.md`.

## Step 1: Read context

1. Read `specs/README.md` to understand the track organization, dependency
   graph, and what already exists.
2. Read `specs/local/spec-coordination/spec-document-model.md` (first 80 lines)
   to review the frontmatter schema and spec conventions.
3. Grep spec files for any existing specs that overlap with the proposed topic
   — check by name and by `affects` paths. If a closely related spec exists,
   warn the user and ask whether to proceed, merge, or abort.

## Step 2: Explore the codebase

Based on the description, identify which parts of the codebase are relevant:

1. Determine which packages, files, and interfaces the spec will affect.
2. Use Grep and Glob to find existing code, types, and patterns in those areas.
3. Launch Agent subagents (Explore type) for up to 3 independent areas in
   parallel if the spec spans multiple subsystems.
4. Note existing patterns, interfaces, and constraints that the spec must
   account for.

The goal is to ground the spec in reality — reference actual file paths,
function names, and existing patterns rather than hypothetical code.

## Step 3: Identify dependencies

Determine which existing specs this new spec depends on:

1. Check `specs/README.md` for specs that produce interfaces, types, or
   infrastructure this spec needs.
2. Check the `affects` lists of existing specs for overlapping code paths.
3. Only add `depends_on` entries for specs whose deliverables are prerequisites
   — not merely related specs.

Also identify which existing specs might depend on this new one (reverse
impact). Flag these to the user but do NOT modify them.

## Step 4: Write the spec

Create the spec file at `specs/<track>/<name>.md` with this structure:

````markdown
---
title: <Human-readable title>
status: drafted
depends_on:
  - <spec paths, or empty list>
affects:
  - <code paths and packages this spec will modify>
effort: <small | medium | large | xlarge>
created: <today's date, YYYY-MM-DD>
updated: <today's date, YYYY-MM-DD>
author: changkun
dispatched_task_id: null
---

# <Title>

## Overview

<2-4 sentences: what this spec delivers and why it matters. State the problem,
the user need, or the architectural gap it fills.>

## Current State

<Brief description of what exists today in the codebase that is relevant.
Reference actual file paths, types, and functions. This grounds the spec in
reality and helps readers understand the starting point.>

## Architecture

<How the solution fits into the existing system. Describe the key components,
their relationships, and where they live in the codebase. Use a diagram
(Mermaid) if the relationships are non-trivial.>

## Components

<For each major component or change:>

### <Component Name>

<What it does, where it lives, key design decisions. Reference existing
patterns in the codebase where relevant. Include:>
- File paths (existing files to modify, new files to create)
- Key types and interfaces
- Integration points with existing code

## Data Flow

<How data moves through the system for the primary use cases. Describe the
request/response path, state transitions, or processing pipeline. Skip this
section if the spec doesn't involve data flow.>

## API Surface

<New or modified API routes, CLI flags, env variables, or configuration
options. Use the existing format from CLAUDE.md. Skip this section if no
external surface changes.>

## Error Handling

<How errors are detected, reported, and recovered from. What failure modes
exist and how the system degrades. Skip this section if error handling is
trivial.>

## Testing Strategy

<What to test and how. Reference existing test patterns in the affected
packages. Identify:>
- Unit tests (per-function, per-method)
- Integration tests (cross-package, end-to-end)
- Edge cases and failure scenarios
````

**Writing guidelines:**

- Focus on system design, not inline code. Use references to actual files
  instead of code blocks where possible.
- Keep sections proportional to complexity — a simple spec doesn't need all
  sections. Delete sections marked "skip if..." when they don't apply.
- Reference existing patterns: "follows the same pattern as `internal/handler/tasks.go`"
  is better than re-explaining a pattern.
- Be specific about file paths and function names. Vague specs produce vague
  implementations.
- Size the spec appropriately:
  - **Small/medium effort**: one file, all sections concise. Can be implemented
    directly.
  - **Large/xlarge effort**: may be a parent spec that will be broken down via
    `/wf-spec-breakdown`. Focus on architecture and
    component boundaries rather than implementation details.

## Step 5: Update specs/README.md

1. Read `specs/README.md`.
2. Add the new spec to the appropriate track table, maintaining alphabetical
   order within the table. Use the format:
   ```
   | [<name>.md](<track>/<name>.md) | Not started | <one-line deliverable> |
   ```
3. Add the spec to the Status Quo section if appropriate (use `○` for not
   started).
4. If the spec has dependencies, note them in the dependency graph section
   if one exists.

## Step 6: Commit

Stage the new spec file and the updated `specs/README.md`. Commit with:
`specs: add <name> spec for <one-line purpose>`

Do NOT push unless the user explicitly asks.

## Step 7: Summary

Report to the user:
- The spec file path and a one-line summary
- Dependencies identified (both upstream and downstream impact)
- Effort estimate and rationale
- Suggested next steps:
  - If large/xlarge: "Run `/wf-spec-breakdown <spec-path> design` to decompose
    into sub-design problems"
  - If small/medium: "Run `/wf-spec-breakdown <spec-path> tasks` to create
    implementable tasks, or `/wf-spec-implement <spec-path>` to implement directly"
  - If dependencies are incomplete: "Blocked by <spec>; implement that first
    or run `/wf-spec-impact <spec-path>` for full analysis"
