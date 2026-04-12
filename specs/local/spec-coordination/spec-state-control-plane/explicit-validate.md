---
title: "Explicit drafted → validated transition"
status: drafted
depends_on: []
affects:
  - internal/handler/specs.go
  - internal/apicontract/routes.go
  - ui/partials/spec-mode.html
  - ui/js/spec-mode.js
  - .claude/skills/wf-spec-breakdown/skill.md
created: 2026-04-12
updated: 2026-04-12
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

## Option A (recommended) — Validate toolbar action

Mirror the archive/unarchive UX:

**Trigger**: user clicks "Validate" in the focused-view toolbar; or
issues a chat command. Visible only when `status == "drafted"`.

**Endpoint**: `POST /api/specs/validate`, shape identical to
`/api/specs/archive`.

```
POST /api/specs/validate   { "path": "specs/local/foo.md" }

→ 200 { "path": "...", "status": "validated" }
→ 422 invalid transition (not drafted)
→ 422 spec is archived
```

Handler steps:
1. Load spec, validate transition via
   `StatusMachine.Validate(current, StatusValidated)`.
2. Write `status: validated` + `updated: now` via
   `UpdateFrontmatter`.
3. Commit via shared `commitSpecChanges`, subject `<path>: mark validated`.

**Non-goals**: no review gate, no signature, no checklist. Validation
is an intent signal, not a review process.

---

## Option B (complementary) — Breakdown tasks-mode auto-validates

`/wf-spec-breakdown <path> tasks` produces child impl specs. If the
parent was `drafted`, upgrade it to `validated` after the children are
written.

Non-presumptuous: the user explicitly asked for an implementation
breakdown, signaling intent to proceed. Ship alongside Option A.

Skill change: after successful child creation, if the parent is
`drafted`, call the new validate endpoint (or write the frontmatter
directly and rely on the same commit). Staying consistent with the
control-plane pattern: always go through the endpoint, not direct
writes.

---

## Option C (deferred) — "Unresolved Open Questions" soft-warn

Some specs carry "Open Questions" sections with unchecked items. A
reviewer about to click Validate might want a nudge.

Tentative: **skip for v1**. "Open Questions" semantics vary (some
sections are archival notes, not gating checklists). Adding a
heuristic that fires false positives is worse than no heuristic.
Revisit if users ask.

---

## UI

Focused view toolbar gets a Validate button between Dispatch and
Break Down:

- Visible: `status === "drafted"` only.
- Click: `POST /api/specs/validate`, reload focused view on 200.
- Error handling: show the 422 text in a toast; no silent failures.

Archived specs never show Validate (state-machine rejects; button
hidden by the existing archived affordance rules).

---

## Acceptance

- Validate button is visible for `drafted` specs, hidden elsewhere.
- Clicking Validate on a `drafted` spec transitions to `validated`,
  commits with subject `<path>: mark validated`, re-renders the
  focused view.
- `POST /api/specs/validate` on non-`drafted` returns 422 with a
  useful message.
- `/wf-spec-breakdown <drafted-spec> tasks` upgrades the parent to
  `validated` after child specs are written.
- Unit tests: drafted → 200, complete → 422, archived → 422, vague → 422.

---

## Open Questions

1. **Chat command alias.** `/validate` exists as a slash command
   template but today only populates a prompt. Should the command also
   hit the endpoint directly? Tentative: yes — `/validate` in the chat
   should produce the same transition as the toolbar button. The
   prompt template's role shifts to "ask the agent whether this spec
   is ready"; a confirmation in chat → endpoint call.
2. **Reverse action.** Should there be an `unvalidate` / "demote to
   drafted"? The state machine already allows `validated → drafted`
   via `/wf-spec-refine`. Tentative: no separate action; refining a
   validated spec already demotes it to `drafted` if meaningful edits
   happened.
3. **Audit.** Do we need a per-spec history of who validated it and
   when? Today git log answers this. Tentative: no additional
   metadata; rely on git.
