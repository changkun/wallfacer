---
title: "Archival: Skills and README updates"
status: validated
depends_on: []
affects:
  - .claude/skills/wf-spec-validate/skill.md
  - .claude/skills/wf-spec-status/skill.md
  - specs/README.md
  - specs/local/spec-coordination/spec-document-model.md
  - specs/local/spec-coordination/spec-document-model/spec-lifecycle.md
  - specs/local/spec-coordination/spec-document-model/per-spec-validation.md
  - specs/local/spec-coordination/spec-document-model/cross-spec-validation.md
  - specs/local/spec-coordination/spec-document-model/impact-analysis.md
  - specs/local/spec-coordination/spec-document-model/progress-tracking.md
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: d1abe93d-5ea9-4d95-84f1-288b09c0580e
---


# Archival: Skills and README updates

## Goal

Update the two coordination skills and `specs/README.md` to reflect the new
`archived` status, and refresh the completed `spec-document-model/*` subtree
so it documents the 6-state lifecycle instead of the 5-state one. This task
is documentation-only — no Go or JS code changes.

## What to do

### `.claude/skills/wf-spec-validate/skill.md`

1. Find the line that lists valid status values (currently: `vague`, `drafted`,
   `validated`, `complete`, `stale`) and add `archived`.
2. In the cross-spec validation rules section, add two new entries:
   - Status consistency: "Archived non-leaf specs are exempt from the incomplete-children
     check — their subtree is considered below glass regardless of leaf states."
   - Stale propagation: "Stale propagation does not fire for archived dependencies.
     A validated spec depending on an archived spec receives only a soft note
     (`dependency-is-archived` warning), not a stale-propagation warning."
   - New rule: "dependency-is-archived (warning): a live spec whose `depends_on`
     includes an archived spec. Advisory only — remove the edge or document why it
     still matters."
3. In the per-spec rules section, add two skip conditions:
   - `body-not-empty`: "suppressed for `archived` specs — a stub with only frontmatter is valid."
   - `affects-paths-exist`: "suppressed for `archived` specs — deleted paths are not actionable."

### `.claude/skills/wf-spec-status/skill.md`

1. In the status field description (line listing `vague`, `drafted`, `validated`,
   `complete`, `stale`), add `archived` with definition:
   "spec is retired from the live graph; read-only, hidden by default, excluded from
   progress, impact, drift, and dispatch."
2. In the actionable-spec classification, add: archived specs are never actionable;
   do not count them in blocked or unblocked totals.
3. In the progress computation notes, add: archived leaves contribute 0 to both
   `done` and `total`; an archived non-leaf masks its entire subtree from progress.
4. Document the resurrection transition (`archived → drafted`) as the only valid
   inbound transition for archived specs.

### `specs/README.md`

5. In the **Status Quo** table for "Local Product", update `spec-archival.md` from
   "Drafted" to "In progress" (since it is now being implemented via child tasks).
6. In the `spec-coordination.md` row description, the sub-bullet for `spec-archival.md`
   should reflect implementation is underway.
7. Archived specs in the README tables: the README currently has no archived specs,
   so no filtering change is needed now. Add a comment in the README template section
   (if any) noting that archived specs are omitted by default.

### `spec-document-model/*` subtree refreshes

These completed specs document the 5-state enum inline. Update each to add
`archived` as the sixth state. All changes are additive (no existing behavior removed),
so the `status` field stays `complete` with an updated `Outcome` note.

8. **`spec-document-model.md`** — update the lifecycle diagram (ASCII art), the
   `status` enum comment in the example frontmatter, and the lifecycle transition table.
   Add `archived` as a terminal state that exits only via `drafted`.

9. **`spec-document-model/spec-lifecycle.md`** — update the transition map table,
   the test case enumeration section, and any `ValidStatuses()` count references
   (5 → 6).

10. **`spec-document-model/per-spec-validation.md`** — update the `valid-status` rule
    enum list; add the `body-not-empty` and `affects-paths-exist` skip conditions for
    archived specs; note the archived-specific validation exemptions.

11. **`spec-document-model/cross-spec-validation.md`** — update status-consistency
    skip condition; update stale-propagation skip condition; add the new
    `dependency-is-archived` soft note rule.

12. **`spec-document-model/impact-analysis.md`** — update `Adjacency` skip conditions;
    update `ComputeImpact` empty-return guard; update `allDepsComplete` archived-as-satisfied
    semantics; update `UnblockedSpecs` archived-exclusion rule.

13. **`spec-document-model/progress-tracking.md`** — update `NodeProgress` with
    archived-leaf and archived-subtree exclusion rules; note that `TreeProgress` is
    unchanged (inherits from `NodeProgress`).

## Tests

No automated tests (documentation-only). Manual check:
- Run `/wf-spec-validate` after this task: no `valid-status` errors for specs with
  `status: archived`
- Run `/wf-spec-status` after this task: archived specs appear under a separate
  "Archived" heading rather than counting toward progress or blocked totals

## Boundaries

- Do NOT touch any `.go` or `.js` files
- Do NOT change the spec-document-model subtree status from `complete` — these are
  additive updates, not structural drift; update the `updated` date only
- Do NOT implement any new skill commands (archive/unarchive as chat commands are
  part of `focused-view-ux.md`'s scope when the chat agent wiring is extended later)
