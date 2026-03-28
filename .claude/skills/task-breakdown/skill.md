---
name: task-breakdown
description: Break down a spec into smaller, implementable task files stored in a folder. Each task has status, dependencies, phase, effort, goal, instructions, tests, and boundaries. Use when a spec needs to be decomposed into discrete implementation steps.
argument-hint: <spec-file.md>
allowed-tools: Read, Grep, Glob, Edit, Write, Agent, Bash(ls *), Bash(mkdir *)
---

# Break Down Spec into Tasks

Decompose a design spec into smaller, implementable task files organized in a
folder alongside the spec.

## Step 0: Parse arguments

Extract the spec file path from the first token of the arguments.

## Step 1: Read the spec and context

1. Read the spec file in full.
2. Read `specs/README.md` to understand milestone ordering and dependencies.
3. Identify the spec's implementation plan, phases, and any existing task
   breakdown structure.

## Step 2: Explore the codebase

For each phase or major section in the spec, explore the codebase to understand:

- What files will be created or modified
- What existing patterns, types, and interfaces are relevant
- What test patterns exist in those packages
- Whether any items are already partially implemented

Use Agent subagents (Explore type) for thorough codebase exploration. Launch up
to 3 in parallel for independent areas.

## Step 3: Design the task breakdown

Break the spec into discrete, implementable tasks. Each task should:

- Be completable in a single commit
- Leave the project in a working state (tests pass)
- Have clear boundaries (what to change, what NOT to change)
- Include specific test requirements

Order tasks so dependencies flow forward (no task depends on a later task).

Guidelines for task granularity:
- **Small** (~50-100 lines changed): Add a type, add a method, add a field
- **Medium** (~100-300 lines): Refactor a function, add a new file with tests
- **Large** (~300+ lines): Multi-file refactor, complex feature with many touchpoints

Prefer smaller tasks. If a task feels large, split it further.

## Step 4: Create the task folder and files

1. Create a folder named after the spec (e.g., `specs/03-container-reuse/`)
2. Create one markdown file per task, numbered sequentially:
   `task-01-<name>.md`, `task-02-<name>.md`, etc.

Each task file must follow this template:

```markdown
# Task N: <Title>

**Status:** Todo
**Depends on:** <Task numbers or "None">
**Phase:** <Phase number and name from the spec>
**Effort:** <Small | Medium | Large>

## Goal

<1-2 sentences explaining what this task achieves and why>

## What to do

<Numbered list of specific implementation steps with file paths,
function names, and code patterns. Include pseudocode for non-obvious
changes.>

## Tests

<Bulleted list of specific test cases to write, with test function
names and what they verify>

## Boundaries

<Bulleted list of what NOT to change in this task — helps scope the
work and prevents task creep>
```

## Step 5: Verify the breakdown

Check that:
- Every item from the spec's implementation plan is covered by at least one task
- No circular dependencies exist between tasks
- The dependency graph allows parallel execution where possible
- Each task's "What to do" section references real file paths and function names

## Step 6: Commit

Stage the new task folder and files. Commit with a message like:
`specs: break down <spec-name> into implementable tasks`

## Step 7: Summary

Report to the user:
- Total number of tasks created
- The dependency graph (which tasks can run in parallel)
- Any spec items that were intentionally excluded and why
