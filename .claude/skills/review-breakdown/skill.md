---
name: review-breakdown
description: Validate a task breakdown for correctness — check dependency ordering, task sizing, gap coverage, and boundary conflicts. Use after wf-spec-breakdown to catch issues before implementation.
argument-hint: <spec-file.md or task-folder/>
allowed-tools: Read, Grep, Glob, Agent, Bash(ls *)
---

# Review Task Breakdown

Validate a task breakdown produced by `/wf-spec-breakdown` before starting
implementation. Catch structural issues that would cause failures mid-execution.

## Step 0: Parse arguments

Extract the spec file path or task folder from the first token. If given a spec
file, locate its child spec directory (sibling directory with matching name,
e.g., `specs/foundations/sandbox-backends/` for `sandbox-backends.md`). If given
a directory, locate the parent spec (the `.md` file in the containing directory
whose name matches the directory).

## Step 1: Load the breakdown

1. Read the parent spec in full. **Parse its YAML frontmatter** to extract
   `title`, `status`, `track`, `depends_on`, `affects`, `effort`.
2. Read every child spec file in the subdirectory. **Parse each child's YAML
   frontmatter** — extract `title`, `status`, `track`, `depends_on`, `affects`,
   `effort`, `dispatched_task_id`.
3. Read `specs/README.md` for track context and cross-spec dependencies.

## Step 2: Check dependency correctness

For each child spec's `depends_on` list in its YAML frontmatter:

- Verify every path in `depends_on` resolves to an existing spec file.
- Verify no self-dependency (a spec cannot list itself in `depends_on`).
- Verify no circular dependencies in the DAG (topological sort must succeed).
- Verify dependency direction: if spec B modifies a function that spec A creates,
  B must depend on A. Cross-reference `affects` lists to catch this.
- Flag missing dependencies: if two child specs have overlapping `affects` paths,
  check whether they need ordering.
- Verify `depends_on` edges that cross subtrees or tracks are intentional and
  the target spec exists.

Report: list of dependency issues, or "Dependencies: OK".

## Step 3: Check task sizing

For each child spec, estimate scope by examining the `affects` list from
frontmatter and the "What to do" section in the body:

- Read each file in `affects` to check its size and complexity.
- Cross-check `effort` (small/medium/large/xlarge) against actual scope:
  - `small` with 6+ files in `affects` is suspicious.
  - `xlarge` with only 1-2 files may be over-estimated.
- Flag specs whose "What to do" has fewer than 3 steps as potentially too small
  (might be foldable into a sibling spec).
- Flag specs that create new packages AND refactor existing code (should usually
  be split).
- Verify leaf specs are small enough for one agent task (2-5 files, one clear
  goal) per the spec document model.

Report: list of sizing concerns, or "Sizing: OK".

## Step 4: Check spec coverage

Compare the spec's implementation plan against the task breakdown:

- For each item in the spec (sections, bullet points, requirements), check that
  at least one task covers it.
- Flag spec items with no corresponding task.
- Flag tasks that don't trace back to any spec item (scope creep).

Report: uncovered spec items and untraceable tasks, or "Coverage: OK".

## Step 5: Check boundary conflicts

For each pair of child specs that have overlapping `affects` paths in their
frontmatter:

- Check that their "Boundaries" sections don't overlap (both claiming to modify
  the same function or type).
- Check that the later spec's "What to do" accounts for changes made by the
  earlier spec (follow the `depends_on` ordering).
- Flag cases where two independent specs (no `depends_on` edge between them)
  share `affects` entries — they may conflict during parallel execution.

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
`/wf-spec-breakdown` with feedback.
