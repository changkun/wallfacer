---
title: "Explicit drafted → validated transition"
status: drafted
depends_on: []
affects:
  - internal/handler/specs.go
  - internal/handler/specs_dispatch.go
  - internal/apicontract/routes.go
  - frontend/src/components/plan/SpecFocusedView.vue
  - .claude/skills/wf-spec-breakdown/skill.md
created: 2026-04-12
updated: 2026-06-14
author: changkun
dispatched_task_id: null
effort: small
---

# Explicit `drafted → validated` Transition

Today `drafted → validated` is the only lifecycle edge with no
server-driven trigger. Humans edit the YAML by hand or
`/wf-spec-refine` happens to write it. `/wf-spec-breakdown` tasks-mode
**presumes** the parent is already `validated` but neither enforces nor
sets it.

The control plane should expose "this design is settled" as an
explicit first-class action.

---

## Option A (recommended) - Validate toolbar action

Mirror the archive/dispatch UX, which already runs through the unified
`POST /api/specs/transition` endpoint
(`SpecTransition` in `internal/handler/specs_dispatch.go`, dispatching
on an `action` discriminator: `dispatch` / `undispatch` / `archive` /
`unarchive`).

**Trigger**: user clicks "Validate" in the focused-view toolbar
(`frontend/src/components/plan/SpecFocusedView.vue`); or issues a chat
command. Visible only when `status == "drafted"`.

**Endpoint**: add a `validate` action to `POST /api/specs/transition`,
alongside the existing actions. (A standalone `/api/specs/validate`
mirror was the original sketch, but the codebase consolidated all spec
transitions into one endpoint; follow that pattern.)

```
POST /api/specs/transition   { "action": "validate", "path": "specs/local/foo.md" }

→ 200 { "path": "...", "status": "validated" }
→ 422 invalid transition (not drafted)
→ 422 spec is archived
```

Handler steps:
1. Load spec, validate transition via
   `StatusMachine.Validate(current, StatusValidated)`.
2. Write `status: validated` + `updated: now` via
   `UpdateFrontmatter`.
3. Commit via the shared `commitSpecTransition` helper, subject
   `<path>: mark validated`.

**Non-goals**: no review gate, no signature, no checklist. Validation
is an intent signal, not a review process.

---

## Option B (complementary) - Breakdown tasks-mode auto-validates

`/wf-spec-breakdown <path> tasks` produces child impl specs. If the
parent was `drafted`, upgrade it to `validated` after the children are
written.

Non-presumptuous: the user explicitly asked for an implementation
breakdown, signaling intent to proceed. Ship alongside Option A.

Skill change: after successful child creation, if the parent is
`drafted`, call the new validate action (or write the frontmatter
directly and rely on the same commit). Staying consistent with the
control-plane pattern: always go through the endpoint, not direct
writes.

---

## Option C (deferred) - "Unresolved Open Questions" soft-warn

Some specs carry "Open Questions" sections with unchecked items. A
reviewer about to click Validate might want a nudge.

Tentative: **skip for v1**. "Open Questions" semantics vary (some
sections are archival notes, not gating checklists). Adding a
heuristic that fires false positives is worse than no heuristic.
Revisit if users ask.

---

## UI

Focused view toolbar
(`frontend/src/components/plan/SpecFocusedView.vue`) gets a Validate
button between Dispatch and Break Down. The toolbar already computes
`showDispatch` / `showBreakdown`; add a parallel `showValidate`:

- Visible: `status === "drafted"` only.
- Click: `POST /api/specs/transition` with `action: "validate"`, reload
  focused view on 200.
- Error handling: show the 422 text in a toast; no silent failures.

Archived specs never show Validate (state-machine rejects; button
hidden by the existing archived affordance rules, e.g. `isArchived`).

---

## Acceptance

- Validate button is visible for `drafted` specs, hidden elsewhere.
- Clicking Validate on a `drafted` spec transitions to `validated`,
  commits with subject `<path>: mark validated`, re-renders the
  focused view.
- `POST /api/specs/transition` with `action: "validate"` on a
  non-`drafted` spec returns 422 with a useful message.
- `/wf-spec-breakdown <drafted-spec> tasks` upgrades the parent to
  `validated` after child specs are written.
- Unit tests: drafted → 200, complete → 422, archived → 422, vague → 422.

---

## Open Questions

1. **New action vs new endpoint.** This refinement maps the validate
   transition onto the existing `/api/specs/transition` action
   discriminator rather than a standalone `/api/specs/validate`. That
   keeps it consistent with dispatch/archive, but the maintainer may
   prefer a dedicated route for clarity. Tentative: reuse the unified
   endpoint.
2. **Chat command alias.** `/validate` exists as a slash command
   template but today only populates a prompt. Should the command also
   hit the endpoint directly? Tentative: yes - `/validate` in the chat
   should produce the same transition as the toolbar button. The
   prompt template's role shifts to "ask the agent whether this spec
   is ready"; a confirmation in chat → endpoint call.
3. **Reverse action.** Should there be an `unvalidate` / "demote to
   drafted"? The state machine already allows `validated → drafted`
   via `/wf-spec-refine`. Tentative: no separate action; refining a
   validated spec already demotes it to `drafted` if meaningful edits
   happened.
4. **Audit.** Do we need a per-spec history of who validated it and
   when? Today git log answers this. Tentative: no additional
   metadata; rely on git.
