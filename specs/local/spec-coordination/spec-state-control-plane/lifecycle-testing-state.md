---
title: "Lifecycle: should there be a testing state?"
status: drafted
depends_on: []
affects:
  - internal/spec/lifecycle.go
  - internal/spec/model.go
  - specs/local/spec-coordination/spec-document-model.md
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
effort: small
---

# Lifecycle: Should There Be a `testing` State?

Load-bearing decision that blocks the drift pipeline
([drift-pipeline.md](drift-pipeline.md)): when a task's implementation is
done but the tester hasn't rendered a verdict yet, what status does the
spec carry?

No code to write for this spec — the output is **a call**: adopt a 7th
lifecycle state, or don't. Subsequent specs consume the answer.

---

## Context

Today the lifecycle has 6 states: `vague → drafted → validated →
complete → stale → archived`. `SpecCompletionHook` writes `complete` the
moment a task reaches `done` — no hold, no verdict gate.

The drift pipeline wants to insert a tester step between `done` and the
final status write. During that step the spec is: not `validated`
anymore (implementation is already landed), not `complete` yet (verdict
pending), not `stale` (no verdict says it diverged). There's no state
that fits.

---

## Options

### Option A — Add `testing` as a 7th lifecycle state

Transition map extension:

```
validated → testing → complete | stale
```

Changes:
- `internal/spec/model.go`: add `StatusTesting Status = "testing"`
- `internal/spec/lifecycle.go`: update transition map; `ValidStatuses()`
  returns 7 values
- `spec-document-model.md` et al.: update 6→7 in every place the
  document model enumerates states (lifecycle diagram, enum comments,
  per-spec validation `valid-status` rule, cross-spec rules)
- Validator: `body-not-empty` and `affects-exist` should probably skip
  `testing` too (like they skip archived)
- Explorer: status icon + badge + filter-dropdown entry
- Planning chat skills (`wf-spec-validate`, `wf-spec-status`): document
  `testing`

**Semantics**: testing is transient — entered on task done, exited on
tester verdict. External writers (manual edits, `/wf-spec-refine`)
should not produce or reach `testing`; only the drift pipeline writes
it.

### Option B — Keep implicit via task state

Spec stays at `validated` during the verdict phase. "Is this spec in
testing?" is answered by `store.Task` state: the spec's
`dispatched_task_id` resolves to a task that is `done` but whose
tester hasn't finished.

Changes:
- None to the state machine.
- Explorer: a separate "testing" badge renders when
  `spec.dispatched_task_id != null && task.status == done &&
  !tester_done`. Requires the explorer to cross-reference task state
  per render.

**Semantics**: spec status and tester status are separate dimensions.
The invariant "validated spec has no landed implementation" is broken
— a spec can be `validated` with a landed, untested implementation on
disk.

### Option C — Non-status frontmatter flag

Add a boolean frontmatter field `testing: true` (or `testing_since:
<timestamp>`). Status stays `validated`; the flag drives the badge.

Changes:
- `internal/spec/model.go`: add field
- Frontmatter schema docs
- Explorer renders the flag as a badge
- Drift pipeline sets/clears the flag

**Semantics**: similar to Option B but explicit. Flag can be cleared on
any status write to avoid forgotten-flag hazards.

---

## Trade-offs

| Dimension | A (7th state) | B (implicit via task) | C (frontmatter flag) |
|---|---|---|---|
| Invariants clean? | ✓ spec status = ground truth | ✗ validated can carry landed code | ~ ground truth split across fields |
| Migration cost | high (every enumerator updated) | zero | low (new optional field) |
| Explorer plumbing | simple (one more status icon) | needs task cross-reference | simple (one more badge) |
| External edit hazard | `testing` in frontmatter by hand is a weird edge case | none | forgotten `testing: true` sticks around |
| Coupling to task state | none | strong (spec explorer depends on task store) | weak |
| Extensibility (spec tested without a board task) | supported | broken | supported |

---

## Recommendation

**Option A (add `testing` as a 7th state).**

Rationale:
- The "validated with landed code" state-of-the-world under Option B is
  a lie — `validated` means "reviewed, ready to execute." A spec whose
  implementation is already on disk is not waiting for execution.
- Option B couples the spec explorer to the task store; today they're
  decoupled. The coupling propagates into every status report and query.
- External extensibility: manual testing without a board task (e.g.,
  "someone tested this by hand, mark it") requires a state the
  lifecycle already recognizes. Option A gives it for free.
- Migration cost is one-time; the ongoing cost of the other options
  accumulates.

Transition edges to add to `StatusMachine`:
- `validated → testing` (drift pipeline on task done)
- `testing → complete` (tester verdict: minimal/moderate drift)
- `testing → stale` (tester verdict: significant drift; or tester
  failed and user overrode)
- `testing → archived` (covers race: user archives a spec whose tester
  was still running)

Transitions to **reject**:
- Direct `validated → complete` or `validated → stale` — every verdict
  must go through `testing`. Enforces the gate at the state-machine
  level.
- External writes landing on `testing` (e.g., `/wf-spec-refine` setting
  `testing`): validator error. Only the drift pipeline should write it.

---

## Open Questions

1. **Timeout.** What happens if a spec sits in `testing` for days because
   the tester agent is down? Needs a handling story — likely lives in
   [drift-pipeline.md](drift-pipeline.md), not here.
2. **Dispatch rejection on `testing`.** Should dispatch refuse
   `testing` specs? (A spec can't be re-dispatched while a verdict is
   pending.) Tentative: yes — add to the pre-dispatch guard.
3. **Does `stale` still go back to `drafted`?** Yes — `testing → stale`
   and the existing `stale → drafted` together handle the re-refine
   loop. No change needed.
