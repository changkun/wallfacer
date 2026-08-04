---
name: wf-spec-validate
description: Structural lint over the spec tree — required frontmatter fields, valid status and effort values, track location, DAG acyclicity, dispatch consistency, orphans, status consistency. Read-only; reports problems and fixes nothing. Use to check the tree is well-formed; use report for what the specs say about progress.
argument-hint: [spec-file.md]
allowed-tools: Read, Grep, Glob, Bash(ls *)
---

# Validate Specs

Run structural validation on spec documents against the repo's document model
— a document-model spec under `specs/`, an internals doc describing how specs
are parsed, or, failing both, the conventions below. If a specific spec file is given as
`$ARGUMENTS`, validate only that spec (and run cross-spec checks it
participates in). Otherwise, validate the entire spec tree.

## Step 0: Parse arguments

If an argument is provided, treat it as a single spec file to validate.
Otherwise, validate all specs under `specs/`.

## Step 1: Discover all specs

1. Glob for all spec files recursively: `specs/**/*.md` (excluding README.md
   and any non-spec markdown files like changelogs).
2. For each spec file, parse YAML frontmatter between `---` fences. Extract:
   `title`, `status`, `depends_on`, `affects`, `effort`, `created`,
   `updated`, `author`, `dispatched_task_id`.
3. Determine leaf vs non-leaf: a spec is non-leaf if a subdirectory with the
   same name (without `.md`) exists and contains at least one child spec.
4. Build the full spec tree (parent-child from filesystem) and the dependency
   DAG (from `depends_on` edges).

## Step 2: Per-spec validation

For each spec, check these rules. Classify each finding as `error` or
`warning` per the severity column.

### Required fields (error)
`title`, `status`, `effort`, `created`, `updated`, `author` must all be present in
the frontmatter. Report each missing field. (`track` may be a frontmatter field or
derived from the path, depending on the repo's layout; see Valid track.)

### Valid status (error)
`status` must be one of: `vague`, `drafted`, `validated`, `testing`, `complete`,
`stale`, `archived` — the seven lifecycle states. `testing`
is the transient drift-verdict state between `validated` and `complete`; a spec in
`testing` is valid (not an error), though a long-lived `testing` carrying a
`testing_pending` reason is worth a warning. The server may also set the optional
fields `implementation_commit` (`base..tip`, present during `testing`) and
`testing_pending` (a reason string when the drift tester failed); both are valid.

### Valid track (error)
Grouping and ordering are independent axes, and they compose —
`specs/local/003-live-serve.md` is grouped by directory *and* numbered. Check
each separately; never infer one from the other, and never decide a single
layout for the whole repo from a majority vote.

**Grouping.** Every spec must have a track from exactly one source:
- A directory under `specs/` supplies it — the segment immediately after
  `specs/`. A `track:` key that merely restates the directory is a removable
  no-op.
- A spec sitting directly under `specs/` must carry a `track:` frontmatter
  field. A spec with neither is an error.

**Ordering.** An `NNN-` filename prefix is an optional, *per-directory* reading
order. Judge it within each directory, never across the tree:
- A directory whose specs are numbered should number all of them; flag an
  unnumbered straggler.
- Flag duplicate numbers inside one directory. The same number in two different
  directories is fine — they are separate number spaces.
- A directory with no numbering at all is not an error. Do not ask for numbers
  that were never adopted.
- An archived spec keeps the number it had (`specs/.archive/…/042-foo.md`), so
  `depends_on` paths pointing at it still resolve.

Numbers are never the dependency order. That is `depends_on`, checked below.

### Valid effort (error)
`effort` must be one of: `small`, `medium`, `large`, `xlarge`.

### Date format (error)
`created` and `updated` must be valid ISO dates (YYYY-MM-DD). `updated` must
be greater than or equal to `created`.

### Dispatch consistency (error)
Non-leaf specs must have `dispatched_task_id: null` (or absent). Leaf specs
may have `null` or a valid UUID.

### `depends_on` targets exist (error)
Every path in `depends_on` must resolve to an existing spec file relative to
the repository root.

### No self-dependency (error)
A spec must not appear in its own `depends_on` list.

### `affects` paths exist (warning)
Every path in `affects` should resolve to an existing file or directory in the
codebase. Only a warning because code may not exist yet for `vague`/`drafted`
specs. Suppressed for `archived` specs — deleted paths are not actionable.

### Body not empty (warning)
Specs with status beyond `vague` should have meaningful content below the
frontmatter (more than just a title heading). Suppressed for `archived` specs —
a stub with only frontmatter is valid.

## Step 3: Cross-spec validation (tree-wide)

Run these checks across the full spec tree.

### DAG is acyclic (error)
Perform a topological sort on the `depends_on` graph. If a cycle is detected,
report the full cycle path (e.g., `A -> B -> C -> A`).

### No orphan directories (warning)
A `<name>/` subdirectory under a spec track should have a corresponding
`<name>.md` parent spec file in the same directory.

### No orphan specs (warning)
A `<name>.md` file that has a `<name>/` subdirectory should have at least one
child spec inside that directory.

### Status consistency (warning)
A `complete` non-leaf spec should not have incomplete leaves in its subtree.
Check recursively: if any leaf in the subtree has a status other than
`complete`, warn. Skipped when the non-leaf is `archived` — the subtree is
considered below glass regardless of leaf states.

### Stale propagation (warning)
If a spec is `stale`, check all specs that list it in their `depends_on`.
Those that are still `validated` should be flagged for review — their
assumptions about the stale spec may no longer hold. Does not fire for
`archived` dependencies — a validated spec depending on an archived spec
receives a `dependency-is-archived` advisory note instead (see below).

### Track location (warning)
Where a spec sits in a track directory, warn if it also carries a `track:` key —
the directory already supplies it and the loader ignores the field. Where a spec
sits directly under `specs/`, there is no directory to cross-check, so this
warning does not apply; a missing `track:` there is the error above.

### dependency-is-archived (warning)
A live spec whose `depends_on` includes an archived spec. Advisory only —
recommend removing the edge or documenting why it still matters. Does not
count as a stale-propagation warning.

### Unique dispatches (error)
No two specs may share the same non-null `dispatched_task_id` value. Collect
all `dispatched_task_id` values and report duplicates.

## Step 4: Generate report

Present findings grouped by severity, then by spec:

```
## Spec Validation Report

Specs scanned: N
Errors: N
Warnings: N

### Errors

#### specs/shared/sandbox-backends.md
- [error] Missing required field: author
- [error] depends_on target does not exist: specs/shared/nonexistent.md

#### specs/local/foo.md
- [error] Invalid status: "wip" (must be vague|drafted|validated|testing|complete|stale|archived)

### Cross-Spec Errors
- [error] Cycle detected: A.md -> B.md -> C.md -> A.md
- [error] Duplicate dispatched_task_id "abc-123": specs/a.md, specs/b.md

### Warnings

#### specs/cloud/bar.md
- [warning] affects path does not exist: internal/cloud/bar.go
- [warning] Body is empty for a "drafted" spec

### Cross-Spec Warnings
- [warning] Orphan directory: specs/shared/old-feature/ has no parent spec
- [warning] Stale propagation: specs/shared/api.md is stale, but
  specs/local/client.md (validated) depends on it

### Verdict: PASS / N errors, M warnings
```

If there are zero errors, the verdict is **PASS**. If there are errors, list the
count. Warnings alone do not cause a failure.

## Notes

- This skill is **read-only**. It does not modify any files.
- When validating a single spec (`$ARGUMENTS` provided), still run cross-spec
  checks that involve that spec (its `depends_on` targets, specs that depend on
  it, cycle detection through it).
- Specs without YAML frontmatter are reported as having all required fields
  missing — they may be legacy specs that predate the document model.
- When the repo implements its own spec validator, these rules mirror it.
  Report a disagreement rather than silently preferring one side.
