---
name: review-breakdown
description: Validate a task breakdown for correctness — check dependency ordering, task sizing, gap coverage, and boundary conflicts. Use after task-breakdown to catch issues before implementation.
argument-hint: <spec-file.md or task-folder/>
allowed-tools: Read, Grep, Glob, Agent, Bash(ls *)
---

# Review Task Breakdown

Validate a task breakdown produced by `/task-breakdown` before starting
implementation. Catch structural issues that would cause failures mid-execution.

## Step 0: Parse arguments

Extract the spec file path or task folder from the first token. If given a spec
file, locate its task folder (sibling directory with matching name). If given a
task folder, locate the parent spec.

## Step 1: Load the breakdown

1. Read the parent spec in full.
2. Read every task file in the task folder (task-01-*.md, task-02-*.md, ...).
3. Read `specs/README.md` for milestone context and cross-spec dependencies.

## Step 2: Check dependency correctness

For each task's `Depends on` field:

- Verify referenced task numbers exist.
- Verify no circular dependencies (topological sort must succeed).
- Verify dependency direction: if task B modifies a function that task A creates,
  B must depend on A.
- Flag missing dependencies: if two tasks modify the same file, check whether
  they need ordering.

Report: list of dependency issues, or "Dependencies: OK".

## Step 3: Check task sizing

For each task, estimate scope by examining the files listed in "What to do":

- Read each referenced file to check its size and complexity.
- Flag tasks that reference 6+ files as potentially too large.
- Flag tasks whose "What to do" has fewer than 3 steps as potentially too small
  (might be foldable into another task).
- Flag tasks that create new packages AND refactor existing code (should usually
  be split).

Report: list of sizing concerns, or "Sizing: OK".

## Step 4: Check spec coverage

Compare the spec's implementation plan against the task breakdown:

- For each item in the spec (sections, bullet points, requirements), check that
  at least one task covers it.
- Flag spec items with no corresponding task.
- Flag tasks that don't trace back to any spec item (scope creep).

Report: uncovered spec items and untraceable tasks, or "Coverage: OK".

## Step 5: Check boundary conflicts

For each pair of tasks that reference the same file:

- Check that their "Boundaries" sections don't overlap (both claiming to modify
  the same function or type).
- Check that the later task's "What to do" accounts for changes made by the
  earlier task.
- Flag cases where two independent tasks (no dependency between them) modify the
  same file — they may conflict during parallel execution.

Report: list of boundary conflicts, or "Boundaries: OK".

## Step 6: Check test completeness

For each task:

- Verify the "Tests" section exists and is non-empty.
- Check that test cases cover the "Goal" (not just the mechanics of "What to do").
- Flag tasks that modify existing behavior but only test new behavior.

Report: list of test gaps, or "Tests: OK".

## Step 7: Verify the critical path

Compute the critical path through the dependency graph:

- Identify the longest chain of sequential tasks.
- Flag if the critical path has more than 6 tasks (may indicate missing
  parallelism opportunities).
- Identify tasks with no dependents that could be reordered earlier.

Report: critical path length, parallelism opportunities.

## Step 8: Summary

Present a structured report:

```
## Breakdown Review: <spec-name>

Tasks: N total, N phases
Critical path: N tasks deep
Parallelism: up to N tasks can run concurrently

### Issues Found
- [ ] <issue description with task references>
- [ ] <issue description>

### Recommendations
- <suggestion for improvement>

### Verdict: PASS / NEEDS REVISION
```

If issues are found, list specific remediation steps. Do NOT modify any files —
this skill is read-only. The user decides whether to revise manually or re-run
`/task-breakdown` with feedback.
