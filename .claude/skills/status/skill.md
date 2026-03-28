---
name: status
description: Report current status across all specs — what's done, in progress, blocked, and what's next. Reads reality (spec files, task files, git history) instead of relying on manually maintained status tables.
argument-hint: [spec-file.md]
allowed-tools: Read, Grep, Glob, Agent, Bash(git log *), Bash(ls *)
---

# Project Status

Generate a live status report by reading specs, task files, and git history.
If a specific spec is given, report on that spec only. Otherwise, report across
all specs.

## Step 0: Parse arguments

If an argument is provided, treat it as a spec file path for focused status.
Otherwise, report on the full project.

## Step 1: Discover specs and tasks

1. Glob for all spec files: `specs/*.md` (excluding README.md).
2. For each spec, check for a task folder: `specs/<spec-name>/task-*.md`.
3. Read `specs/README.md` for milestone ordering and dependency context.

## Step 2: Classify each spec

For each spec file:

1. Read the spec's header for any status line.
2. If a task folder exists, read all task files and count by status:
   - `**Status:** Done` → done
   - `**Status:** In Progress` → in progress
   - `**Status:** Todo` → todo
   - `**Status:** Blocked` → blocked
3. If no task folder exists, classify based on spec content:
   - Check git log for commits referencing the spec name.
   - Check if the spec's key deliverables exist in the codebase (grep for
     types, functions, files mentioned in the spec).
   - Classify as: Complete, Partially implemented, Not started.

## Step 3: Check dependencies

For each non-complete spec:

1. Identify its dependencies from `specs/README.md` (the dependency graph
   and milestone tables).
2. Check whether each dependency is complete.
3. Flag specs that are **actionable** (all dependencies met, not yet started
   or partially done).
4. Flag specs that are **blocked** (at least one dependency incomplete).

## Step 4: Identify what's next

From the actionable specs, determine the recommended next steps:

- Specs with task breakdowns ready → can start `/implement-spec`.
- Specs without task breakdowns → need `/task-breakdown` first.
- Specs that need updating → suggest `/refine` first.

## Step 5: Generate report

### For a single spec:

```
## Status: <spec-name>

Progress: N/M tasks done (X%)
Phase: <current phase name>
Blocked by: <nothing or list of incomplete dependencies>

### Tasks
| # | Task | Status | Effort |
|---|------|--------|--------|
| 1 | <title> | Done | Small |
| 2 | <title> | In Progress | Medium |
| 3 | <title> | Todo | Large |

### Next Action
<what to do next for this spec>
```

### For the full project:

```
## Project Status

### Completed
- <spec-name> — <one-line summary of what shipped>

### In Progress
- <spec-name> — N/M tasks done, current phase: <name>

### Actionable (ready to start)
- <spec-name> — dependencies met, <has/needs> task breakdown

### Blocked
- <spec-name> — waiting on: <dependency list>

### Not Started
- <spec-name> — <one-line summary>

### Recommended Next Steps
1. <most impactful actionable item>
2. <second priority>
3. <third priority>
```

## Notes

- This skill is read-only. It does not modify any files.
- Status is derived from source of truth (files, git), not from manually
  maintained tables in README.md.
- If the report reveals that `specs/README.md` status is stale, note the
  discrepancies but do not fix them (suggest `/refine` or manual update).
