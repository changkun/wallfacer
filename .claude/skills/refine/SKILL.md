---
name: refine
description: Refine a spec markdown file by removing already-completed items and updating remaining items to reflect the current project state. Optionally accepts feedback after the file path to guide specific changes (e.g., rewrite scope, change priorities, add/remove items). Use when a spec has drifted from reality and needs to be brought up to date.
argument-hint: <spec-file.md> [feedback...]
allowed-tools: Read, Grep, Glob, Edit, Write, Agent, Bash(git log *), Bash(git show *), Bash(git diff *), Bash(ls *)
---

# Refine Spec

Update a spec file so it accurately reflects the current state of the project.
Remove work that is already done, update partially-done items, and revise
descriptions to match what actually exists in the codebase.

## Step 0: Parse arguments

$ARGUMENTS has the form: `<spec-file.md> [feedback...]`

- The **first token** is the spec file path (e.g., `specs/windows-support.md`).
- Everything after the first token is **feedback** — free-form instructions from
  the user that guide the refinement. Feedback may request specific changes such
  as: removing certain sections, rewriting scope, changing priorities, splitting
  or merging items, adding new items, or adjusting tone/structure.

If feedback is present, apply it **in addition to** the standard codebase audit
below. Feedback takes precedence over the default rules when they conflict
(e.g., if feedback says "keep the done items as a checklist", do that instead
of removing them per rule 3a).

## Step 1: Read the spec

Read the spec file identified in Step 0. Parse it into a list of proposed work
items, features, or changes. For each item, note:
- What it proposes (the deliverable or change)
- Any acceptance criteria or sub-tasks
- Its stated priority or tier

## Step 2: Audit each item against the codebase

For every item in the spec, determine its current status by searching the
codebase. Use Grep, Glob, and Read to find evidence. Launch Agent subagents
in parallel for independent items to speed this up.

Classify each item as one of:
- **Done** — fully implemented and working. Evidence: code exists, tests pass,
  docs updated.
- **Partially done** — some sub-tasks complete, others remain. Note exactly
  which parts are done and which are not.
- **Not started** — no evidence of implementation in the codebase.
- **Obsolete** — the item is no longer relevant due to architectural changes,
  new dependencies, or revised project direction.

For each classification, record the specific files, functions, tests, or
commits that serve as evidence.

## Step 3: Rewrite the spec

Apply these rules:

### 3a. Remove done items
Delete items that are fully implemented. Do not leave them as "completed"
checkboxes — they clutter the spec. If the done work is context for remaining
items, mention it briefly in a "Current State" or "Already Implemented" summary
section at the top (keep this concise — a few bullet points, not a full recap).

### 3b. Update partially done items
For items that are partially complete:
- Strike or remove the finished sub-tasks
- Update descriptions to reflect the current starting point
  (e.g., "Add path translation" becomes "Add Windows drive-letter translation
  in ContainerSpec.Build(); runtime detection and mount-option filtering are
  already implemented")
- Update file/function references if they have changed

### 3c. Keep not-started items
Retain these, but revise their descriptions if the surrounding code has changed
since the spec was written (new function names, moved files, changed APIs).

### 3d. Remove obsolete items
Delete items that no longer apply. If the reason for obsolescence is
non-obvious, add a one-line note explaining why it was removed.

### 3e. Update metadata
- Update any "Current State" or "Status" sections
- Fix file paths, function names, and code references to match the codebase
- Update priority/effort estimates if the scope has changed
- Ensure the spec's title and introduction still accurately describe the
  remaining work

### 3f. Apply user feedback
If feedback was provided in $ARGUMENTS (see Step 0), apply it now. Feedback
may include directives such as:
- **Add items**: Add new work items the user describes; audit the codebase to
  fill in status, effort, and file references just like existing items.
- **Remove/drop items**: Remove specific items the user no longer wants,
  regardless of their codebase status.
- **Reprioritize**: Change the ordering, effort estimates, or dependencies.
- **Restructure**: Split, merge, rename, or reorganize sections.
- **Scope changes**: Narrow or expand what the spec covers.
- **Style/tone**: Rewrite prose to match a requested voice or level of detail.

When feedback conflicts with rules 3a–3e (e.g., "keep done items visible"),
follow the feedback. When feedback is ambiguous, make a reasonable choice and
flag it in the Step 5 summary.

## Step 4: Write the updated spec

Use Edit (preferred) or Write to update the spec file in place. Preserve the
original markdown style (heading levels, list format, code fence style).

## Step 5: Summary

Report to the user:
- How many items were removed (done)
- How many items were updated (partially done)
- How many items remain unchanged (not started)
- How many items were removed as obsolete
- Any items where the status was ambiguous and you made a judgment call

## Guidelines

- Always READ actual source code — never classify items based on memory or
  assumptions about what "should" exist
- When in doubt about whether something is "done", check for both the
  implementation AND tests/docs. Code without tests may be partially done.
- Preserve the spec's voice and structure — this is a refinement, not a rewrite
  from scratch
- Do not add new items to the spec unless the user explicitly asks
- If the spec references external systems (CI, release pipelines, etc.), check
  those too (e.g., read workflow YAML files for CI items)
- Keep the refined spec actionable — every remaining item should clearly state
  what needs to be built or changed
