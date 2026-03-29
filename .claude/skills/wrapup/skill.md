---
name: wrapup
description: Finalize a completed spec — verify all tasks are done, update the parent spec with Outcome and Design Evolution sections, update specs/README.md status, and commit. Use when all tasks in a spec are implemented and the spec needs its completion write-up.
argument-hint: <spec-file>
user-invocable: true
---

# Wrap Up a Completed Spec

Finalize the spec at `$ARGUMENTS` after all implementation tasks are done.

## Step 1: Verify completion

1. Read the spec file. **Parse YAML frontmatter** to extract `title`, `status`,
   `track`, `depends_on`, `affects`, `effort`, `created`, `updated`.
2. Extract the child spec directory path (sibling directory with the same name
   as the spec file without `.md`).
3. If a child spec directory exists, read all child spec files and parse their
   frontmatter. Verify every leaf spec in the subtree has `status: complete`.
   (Non-leaf children are complete when all their own leaves are complete.)
4. If any leaf specs are not `complete`, report them and stop — do not proceed
   with wrap-up.
5. Run `make test` to confirm all tests pass. If tests fail, stop.

## Step 2: Update the parent spec

Read the parent spec file and update it to match the completed-spec style:

### 2a. Update frontmatter

In the YAML frontmatter:
- Set `status: complete`
- Set `updated: <today>` (keep `created` unchanged)
- Verify `dispatched_task_id` is `null` for non-leaf specs

### 2b. Add Outcome section

Insert an `## Outcome` section before any "Future Work" or "Phase N (Future)" sections. It should contain:

1. **Summary paragraph** — 2-3 sentences describing what shipped at a high level.
2. **What Shipped** subsection — bullet list of key deliverables:
   - Number of API endpoints and their location
   - Frontend components and approximate size
   - Number of tests (backend + frontend)
   - Key features delivered
3. **Design Evolution** subsection — numbered list of deviations from the original spec:
   - What the spec said vs. what was actually done
   - Why the change was necessary
   - Reference commit hashes where relevant

To populate this, read the `## Implementation notes` section from each task file (if present) and synthesize.

### 2c. Update File Inventory

If the spec has a File Inventory section, verify it matches the actual files that were created/modified. Update any discrepancies.

## Step 3: Update `specs/README.md`

1. Read `specs/README.md`.
2. Change the status in the ASCII art tree (e.g., `◐  M4: File Explorer (N/M)` → `✅ M4: File Explorer`).
3. Change the status in the Milestones table (e.g., `**In progress** (N/M tasks done)` → `**Complete**`).
4. Update the delivers column if the implementation differs from what was originally described.

## Step 4: Check downstream specs (reverse dependency analysis)

Scan all spec files for `depends_on` entries that reference this spec's path:
1. **Reverse `depends_on` scan** — grep all spec frontmatter for this spec's
   path in their `depends_on` lists. These are specs that were blocked by this
   one and are now potentially unblocked.
2. If this spec introduced or changed interfaces listed in its `affects`, check
   whether downstream specs reference those same files/packages. Verify their
   descriptions are still accurate.
3. If a `stale` spec depends on this one, flag it — the completion may resolve
   or worsen the staleness.
4. Only make factual corrections — do not redesign other specs.

## Step 5: Commit

Stage all modified spec files and commit:
```
specs: mark <spec-name> as complete, update README
```

Do NOT push unless the user explicitly asks.

## Step 6: Report

Tell the user:
- The spec is marked complete
- How many tasks were verified
- Any downstream specs that were updated
- Whether any issues were found during verification

## Guidelines

- **Read before writing** — read the spec and all task files before making changes.
- **Preserve UX design** — the spec may contain detailed UX descriptions, wireframes, and user interaction flows. These are valuable documentation even after implementation. Do not remove or summarize them.
- **Preserve future work** — Phase 3, Phase 4, and "Future Work" sections describe planned extensions. Keep them intact.
- **Match the style** of other completed specs in the repo (e.g., `01-sandbox-backends.md`).
- **Be factual** — the Outcome and Design Evolution sections should document what actually happened, not what was planned. Read task implementation notes for accuracy.
