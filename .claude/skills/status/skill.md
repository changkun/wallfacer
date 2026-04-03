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

1. Glob for all spec files recursively: `specs/**/*.md` (excluding README.md).
   Specs are organized by track: `specs/foundations/`, `specs/local/`,
   `specs/cloud/`, `specs/shared/`.
2. For each spec, **parse YAML frontmatter** to extract `title`, `status`,
   `track`, `depends_on`, `affects`, `effort`, `dispatched_task_id`.
3. Determine parent-child relationships from the filesystem: a spec at
   `specs/<track>/foo.md` with a directory `specs/<track>/foo/` is a non-leaf
   spec; its children are the specs inside that directory.
4. Read `specs/README.md` for track organization and dependency context.

## Step 2: Classify each spec

For each spec file, use the frontmatter `status` field as the primary source of
truth:

1. **Read the `status` from frontmatter**: `vague`, `drafted`, `validated`,
   `complete`, or `stale`.
2. **For non-leaf specs** (those with a child directory), compute progress by
   recursively counting leaf specs in the subtree:
   - Count leaves with `status: complete` vs total leaves.
   - Report progress as `N/M leaves done (X%)`.
3. **For leaf specs**, check `dispatched_task_id` to see if the spec has been
   dispatched to the kanban board. If dispatched, cross-reference the task
   status if possible.
4. **Cross-check with codebase** — for specs that claim `complete`, optionally
   verify the `affects` paths exist and look implemented. For specs that claim
   `validated` but may be partially done, check git log for commits referencing
   the spec or its `affects` paths.

## Step 3: Check dependencies

For each non-complete spec:

1. Read the `depends_on` list from frontmatter. Each entry is a path to another
   spec file.
2. For each dependency, check its frontmatter `status`. A dependency is met
   only when its status is `complete`.
3. Flag specs that are **actionable** — `status` is `validated`, all
   `depends_on` entries are `complete`, and the spec is a leaf (or has a child
   breakdown ready).
4. Flag specs that are **blocked** — at least one `depends_on` entry is not
   `complete`.
5. Flag specs that are **stale** — their `status` is `stale` and they need
   human review before proceeding.

## Step 4: Identify what's next

From the actionable specs, determine the recommended next steps:

- Specs with task breakdowns ready → can start `/implement-spec`.
- Specs without task breakdowns → need `/wf-spec-breakdown <spec> tasks` first.
- Specs that need updating → suggest `/refine` first.

## Step 5: Generate report

### For a single spec:

```
## Status: <title> (<spec-path>)

Status: <frontmatter status>
Track: <track>
Effort: <effort>
Progress: N/M leaves complete (X%)  [for non-leaf specs]
Blocked by: <nothing or list of incomplete depends_on entries>
Affects: <list of code paths from frontmatter>

### Child Specs  [if non-leaf]
| Spec | Status | Effort | Depends on |
|------|--------|--------|-----------|
| <title> | complete | small | — |
| <title> | validated | medium | <sibling> |

### Next Action
<what to do next for this spec>
```

### For the full project:

Group specs by track (foundations, local, cloud, shared), then by lifecycle
state within each track:

```
## Project Status

### foundations
- <spec-name> (complete) — <one-line summary>
- <spec-name> (validated, 3/5 leaves done) — <progress note>

### local
- <spec-name> (drafted) — <one-line summary>

### cloud
- <spec-name> (vague) — <one-line summary>

### Actionable (ready to implement)
- <spec-name> — validated, all depends_on complete, <has/needs> child breakdown

### Blocked
- <spec-name> — waiting on: <depends_on list with statuses>

### Stale (needs review)
- <spec-name> — <reason for staleness or last updated date>

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
