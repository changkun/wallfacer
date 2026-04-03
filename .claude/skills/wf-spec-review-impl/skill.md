---
name: wf-review-impl
description: Review an implementation against its spec or task. Check that acceptance criteria are met, no unintended changes were made, and tests cover the requirements. Use after implement-spec or after a task completes.
argument-hint: <spec-file.md or task-file.md> [commit-range]
allowed-tools: Read, Grep, Glob, Agent, Bash(git diff *), Bash(git log *), Bash(git show *), Bash(go test *), Bash(ls *)
---

# Review Implementation Against Spec

Compare what was actually implemented against what the spec or task asked for.
Catch deviations, missed requirements, and unintended side effects.

## Step 0: Parse arguments

Extract the spec or task file path from the first token. Optionally accept a
commit range (e.g., `abc123..HEAD`) as the second token.

If no commit range is given, infer it: find the commit that added/modified the
spec's task breakdown or the commit tagged in the task's status change to "Done".

## Step 1: Load the requirements

1. Read the spec or task file in full. **Parse YAML frontmatter** to extract
   `title`, `status`, `track`, `depends_on`, `affects`, `effort`.
2. Extract acceptance criteria, "What to do" steps, "Tests" requirements, and
   "Boundaries" constraints from the body.
3. Use the `affects` list from frontmatter as the expected set of files that
   should be modified by this implementation.
4. If reviewing a full spec (non-leaf), read all child spec files in the
   subdirectory and aggregate requirements recursively.

## Step 2: Load the implementation

1. Get the diff for the commit range: `git diff <range> --stat` for file list,
   then `git diff <range>` for full changes.
2. Get the commit messages: `git log <range> --oneline` for the narrative.
3. Group changes by file to understand what was touched.

## Step 3: Check acceptance criteria

For each acceptance criterion or "What to do" step:

- Search the diff for evidence that it was implemented.
- Classify as: Implemented, Partially implemented, Not implemented, or
  Cannot determine.
- For "Partially implemented", explain what's missing.

Report: checklist of criteria with status.

## Step 4: Check for unintended changes

Compare files touched in the diff against the `affects` list from frontmatter
and files listed in the spec body's "What to do" section:

- Flag files modified that aren't in `affects` or mentioned in "What to do".
- For each unexpected file, read the diff hunk and assess whether the change is:
  - Necessary (e.g., import added by a refactor)
  - Cleanup (formatting, dead code removal)
  - Scope creep (new behavior not in the spec)
  - Regression risk (modifying unrelated logic)

Report: list of unexpected changes with assessment.

## Step 5: Check test coverage

For each "Tests" requirement in the spec/task:

- Search for a matching test function in the diff or codebase.
- Verify the test actually tests what the requirement asks for (not just that a
  test with a similar name exists).
- Flag requirements with no corresponding test.
- Run `go test ./...` on affected packages to verify tests pass.

Report: test coverage against requirements.

## Step 6: Check boundary compliance

For each "Boundaries" constraint ("do NOT change X"):

- Search the diff for changes to the forbidden area.
- Flag violations with the specific diff hunk.

Report: boundary violations, or "Boundaries: respected".

## Step 7: Check documentation

If the spec or task mentions documentation updates:

- Verify the relevant doc files were modified in the diff.
- Check that new API routes, env vars, or CLI flags appear in the docs.
- Cross-reference against CLAUDE.md for consistency.

Report: documentation gaps, or "Docs: OK".

## Step 8: Summary

Present a structured report:

```
## Implementation Review: <spec/task name>

Commits: N commits, M files changed, +A/-D lines
Commit range: <range>

### Acceptance Criteria
- [x] <criterion> — implemented in <file>
- [ ] <criterion> — NOT implemented
- [~] <criterion> — partial: <what's missing>

### Unintended Changes
- <file>: <assessment>

### Test Coverage
- [x] <requirement> — covered by TestFoo
- [ ] <requirement> — NO TEST

### Boundary Compliance
- OK / <violations>

### Documentation
- OK / <gaps>

### Verdict: COMPLETE / INCOMPLETE / NEEDS FIXES
```

Do NOT modify any files. This skill is read-only. If issues are found, list
specific remediation steps for the user to act on.
