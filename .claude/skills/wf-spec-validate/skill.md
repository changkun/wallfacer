---
name: wf-spec-validate
description: Validate all specs against the document model rules — check required frontmatter fields, valid status/effort values, track location (path-derived), DAG acyclicity, dispatch consistency, orphan detection, and status consistency. Use to catch structural issues across the spec tree.
argument-hint: [spec-file.md]
allowed-tools: Read, Grep, Glob, Bash(ls *)
---

# Validate Specs

Run structural validation on spec documents as defined in
`specs/local/spec-document-model.md`. If a specific spec file is given as
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
the frontmatter. Report each missing field. (`track` is **not** a frontmatter
field — in the model it is `yaml:"-"`, derived from the path; see Valid track.)

### Valid status (error)
`status` must be one of: `vague`, `drafted`, `validated`, `testing`, `complete`,
`stale`, `archived` (the seven states in `internal/spec/lifecycle.go`). `testing`
is the transient drift-verdict state between `validated` and `complete`; a spec in
`testing` is valid (not an error), though a long-lived `testing` carrying a
`testing_pending` reason is worth a warning. The server may also set the optional
fields `implementation_commit` (`base..tip`, present during `testing`) and
`testing_pending` (a reason string when the drift tester failed); both are valid.

### Valid track (location, error)
`track` is **derived from the path**, not a frontmatter field (the model marks it
`yaml:"-"`): a spec at `specs/foundations/foo.md` is in track `foundations` (the
segment immediately after `specs/`). So there is no `track:` value to cross-check —
instead validate the *location*: every spec must live under a non-empty track
directory (`specs/<track>/...`); flag any spec directly under `specs/` (no track
segment) as an error. If a spec still carries a stale `track:` key in its
frontmatter, flag it as a removable no-op (the loader ignores it).

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
Track is the path segment after `specs/`, not a frontmatter value, so there is
nothing to cross-check between a field and the path. Warn only if a spec sits
directly under `specs/` with no track directory, or still carries a stale
no-op `track:` key in its frontmatter.

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

#### specs/foundations/sandbox-backends.md
- [error] Missing required field: author
- [error] depends_on target does not exist: specs/foundations/nonexistent.md

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
- [warning] Orphan directory: specs/foundations/old-feature/ has no parent spec
- [warning] Stale propagation: specs/foundations/api.md is stale, but
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
- The validation rules match those defined in the "Spec Validation" section of
  `specs/local/spec-document-model.md`.
