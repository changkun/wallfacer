---
name: wf-spec-diff
description: Compare a completed task's implementation against its source spec. Produces a structured divergence report — which acceptance criteria were satisfied, which diverged, and what was implemented but unspecified. Appends an Outcome section to the spec. Use after a dispatched task completes.
argument-hint: <spec-file.md> [commit-range]
allowed-tools: Read, Grep, Glob, Agent, Bash(git diff *), Bash(git log *), Bash(git show *), Bash(go test *), Bash(ls *)
---

# Spec-Implementation Diff

Compare what a dispatched task actually built against what the spec asked for.
This is the non-deterministic layer 2 of the task completion feedback loop —
it goes beyond the mechanical status flip (layer 1) to assess whether the
implementation truly satisfies the spec's intent.

## Step 0: Parse arguments

$ARGUMENTS has the form: `<spec-file.md> [commit-range]`

- The **first token** is the spec file path.
- The optional **second token** is a git commit range (e.g., `abc123..HEAD`).

## Step 1: Load the spec

1. Read the spec file in full. **Parse YAML frontmatter** — extract `title`,
   `status`, `depends_on`, `affects`, `effort`, `dispatched_task_id`.
2. Verify `dispatched_task_id` is non-null. If null, the spec was never
   dispatched — suggest `/wf-spec-review-impl` instead (which works without
   a dispatch link).
3. Extract the acceptance criteria from the spec body:
   - Design specs: items from Options (chosen option), Components, Architecture
   - Task specs: items from "What to do", "Goal", "Tests"
   - Any numbered or bulleted requirements
4. If the spec is non-leaf, read child specs recursively and aggregate their
   criteria.

## Step 2: Load the implementation

1. If a commit range was provided, use it directly.
2. Otherwise, infer the range: find commits associated with the dispatched task.
   Look for the task UUID in commit messages, or use the task's worktree diff
   via `GET /api/tasks/{id}/diff`.
3. Get the diff: `git diff <range> --stat` for file list, `git diff <range>`
   for full changes.
4. Get commit messages: `git log <range> --oneline`.

Use Agent subagents (Explore type) to parallelize diff analysis across
independent packages if the change spans many files.

## Step 3: Classify each spec item

For every acceptance criterion or deliverable in the spec:

- **Satisfied** — the diff contains clear evidence of implementation. The code
  matches what the spec described (types, functions, behavior).
- **Diverged** — the item was addressed but the implementation differs from
  what the spec specified. Note *what* the spec said vs *what* was built and
  *why* the divergence may have occurred (common reasons: runtime constraints,
  API changes, simpler approach found).
- **Not implemented** — no evidence in the diff. The item was skipped or
  deferred.
- **Superseded** — the item was replaced by a different approach that achieves
  the same goal. Note the replacement.

For each classification, cite specific files, functions, and diff hunks as
evidence.

## Step 4: Identify unspecified work

Scan the diff for changes not traceable to any spec item:

- New files, types, or functions not mentioned in the spec
- Refactoring of existing code beyond what the spec required
- Test additions beyond what the spec's "Tests" section listed

Classify each as:
- **Necessary scaffolding** — required to make the spec work (e.g., imports,
  helper types, error handling)
- **Improvement** — reasonable enhancement discovered during implementation
- **Scope creep** — work that belongs in a different spec or wasn't requested

## Step 5: Assess overall drift

Compute a drift summary:

- **Satisfaction rate**: N of M spec items satisfied (percentage)
- **Divergence count**: how many items were implemented differently
- **Unimplemented count**: how many items were skipped
- **Unspecified count**: how many changes weren't in the spec

Based on these numbers, determine the drift level:
- **Minimal** (>90% satisfied, ≤1 divergence) — spec is accurately complete
- **Moderate** (70-90% satisfied, or 2-3 divergences) — spec is mostly complete
  but the Outcome section should document the deviations
- **Significant** (<70% satisfied, or major divergences) — the spec should
  transition to `stale` rather than `complete`, as it no longer accurately
  describes what was built

## Step 6: Write the Outcome section

Append or update an `## Outcome` section on the spec file (before any "Future
Work" or "Open Questions" sections):

````markdown
## Outcome

**Drift**: Minimal | Moderate | Significant
**Satisfaction**: N/M items (X%)

### What Shipped
- <bullet list of key deliverables with file paths>

### Divergences
- **<spec item>**: spec said X, implementation does Y. Reason: <why>

### Not Implemented
- **<spec item>**: <reason — deferred, descoped, superseded by Z>

### Unspecified Work
- **<file/function>**: <classification and brief description>
````

If drift is significant, also update the spec's `status` to `stale` in the
frontmatter and set `updated` to today's date. Add a note explaining why the
spec needs revision.

If drift is minimal or moderate, leave `status` as `complete` (the layer 1
hook already set it).

## Step 7: Run tests

If the spec has a "Tests" or "Testing Strategy" section:

1. Run `go test ./...` on affected packages to verify tests pass.
2. Cross-reference test results against the spec's test requirements.
3. Note any spec-required tests that are missing or failing.

Include test results in the Outcome section.

## Step 8: Summary

Report to the user:

```
## Spec Diff: <spec title>

Task: <task UUID>
Commits: N commits, M files changed
Drift: Minimal | Moderate | Significant
Satisfaction: N/M items (X%)

### Satisfied
- <item> — in <file>

### Diverged
- <item> — spec: X, impl: Y

### Not Implemented
- <item>

### Unspecified
- <file>: <classification>

### Recommendation
- <what to do next — nothing, update spec, or mark stale>
```

## Guidelines

- This skill bridges the gap between "task done" and "spec accurately complete."
  The server-side hook (layer 1) marks the spec `complete` immediately. This
  skill (layer 2) verifies that claim and corrects it if needed.
- Be factual, not judgmental. Divergences aren't failures — implementations
  often improve on specs. Document what happened and why.
- Preserve the spec's existing content. The Outcome section is additive.
- If you can't determine whether an item was satisfied (ambiguous spec language,
  unclear diff), classify it as "Cannot determine" and flag it for human review.
- This skill is most valuable for design specs where the implementation had
  latitude to interpret. For task specs with precise "What to do" steps, the
  assessment is usually straightforward.
