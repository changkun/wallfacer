---
title: Agon Supersedes the Test Step (Verification Gate)
status: complete
depends_on:
  - agon-adversarial-verification
affects:
  - internal/store/models.go
  - internal/store/tasks_update.go
  - internal/handler/tasks_autopilot.go
  - internal/handler/execute.go
  - internal/constants/constants.go
effort: large
created: 2026-06-28
updated: 2026-06-28
author: changkun
dispatched_task_id: null
---

# Agon Supersedes the Test Step

## Problem

Agon and the single test agent are both post-implementation verification gates.
Today agon is purely informational (badge + panel), while the test agent drives
the real autopilot loop: a failing test auto-resumes the task with feedback, and
a passing test gates auto-submit. Running both under autopilot is redundant cost
and two verdicts to reconcile. The user wants agon, when enabled, to **replace**
the test agent's role — but only when enabled (agon is opt-in and expensive; the
test path must stay intact as the default).

## Decision

When **agon is enabled** and a task is **agon-eligible** (has a Claude
`session_id`), agon supersedes the test agent for that task under autopilot:

1. **Suppress auto-test** for agon-eligible tasks (no double verification).
   Non-session tasks still use the test agent even when agon is on (agon can't
   run without a fork-session).
2. **Unresolved verdict is a hard barrier**: when agon finds unresolved attacks,
   the task is parked in `waiting` and autopilot does not advance it. Clearing
   the barrier is an explicit human act — confirm the work, or resume with
   steering (which calls `ClearAgonResult` and triggers fresh re-verification).
   Autopilot does not auto-resume the task on its own.
3. **Auto-submit gates on agon**: an agon-eligible task auto-submits only when
   its agon verdict is clean (`AgonUnresolved == 0`), replacing the test "pass"
   requirement.

Agon **off** → the test agent path is exactly as today. Nothing is deleted.

## Design

### Task model (`store/models.go`)

No new fields. The existing agon verdict fields (`AgonUnresolved`,
`AgonHeadline`, `AgonSessionDir`) carry the barrier state: a non-nil
`AgonUnresolved > 0` is the parked-for-review signal. `ClearAgonResult` wipes the
verdict on resume so the re-worked diff is re-verified.

### runAgon (`tasks_autopilot.go`)

After persisting the verdict:
- `AgonUnresolved == 0` (clean): emit a "verification clean" timeline event; the
  task is eligible for auto-submit.
- `AgonUnresolved > 0` (unresolved): emit a "task halted for review — confirm or
  resume with steering to re-verify" timeline event. The task stays in `waiting`;
  autopilot does not auto-resume it. The verdict (and `AgonHeadline`) drives the
  UI badge/panel so the unresolved attacks are visible.

### Human steering (`resumeWaitingTaskWithFeedbackLocked` / manual resume)

There is no autopilot auto-resume branch for agon. A human clears the barrier by
resuming the task (optionally with steering feedback); the resume path calls
`ClearAgonResult`, dropping the stale verdict so the next run re-verifies the new
work. Re-failure re-blocks. (An earlier design auto-resumed with the attacks as
feedback up to `MaxAgonRetries`; that loop was dropped in favor of explicit human
confirmation, so the verdict is a deterministic barrier rather than a cost-capped
retry cycle.)

### Suppress auto-test (`tryAutoTest`)

In the Phase-1 candidate filter, skip a task when `h.AgonEnabled() && t.SessionID
!= nil` — agon will verify it. Non-session tasks fall through to the test agent.

### Auto-submit gate (`tryAutoSubmit`)

Replace the `tested := t.LastTestResult == "pass"` gate with a helper:

```go
func (h *Handler) taskVerifiedForSubmit(t *store.Task) bool {
    if h.AgonEnabled() && t.SessionID != nil && *t.SessionID != "" {
        return t.AgonUnresolved != nil && *t.AgonUnresolved == 0
    }
    return t.LastTestResult == "pass"
}
```

The `naturallyComplete` branch (end_turn, untested, autotest off) must also wait
for agon when the agon gate applies — i.e. don't naturally-submit an
agon-eligible task that agon hasn't cleared.

## Outcome (2026-06-28)

`agonSupersedesTest(t)` (agon enabled + session) is the single gate: `tryAutoTest`
skips such tasks and `tryAutoSubmit` requires a clean agon verdict for them. An
unresolved verdict is a hard barrier — `runAgon` parks the task in `waiting` with
a "halted for review" timeline event and autopilot does not advance it; a human
confirms or resumes with steering (`ClearAgonResult` re-arms verification). Agon
off → the test path is unchanged.

The design initially shipped with an auto-resume feedback loop (attacks fed back
to the implementation agent, capped at `MaxAgonRetries=2`). That loop was removed
in a follow-up: the `PendingAgonFeedback` / `AgonRetryCount` fields,
`MaxAgonRetries`, the `SetAgonFeedback` / `ResetAgonRetry` setters, and the
`tryAutoPromote` agon-resume branch are gone. The verdict is now a deterministic
human-gated barrier rather than a cost-capped retry cycle. `TestRunAgon_BlocksOnUnresolved`
covers it (verdict persists, task stays waiting, no auto-resume, clean clears the
barrier); full handler+store suites and golangci-lint pass.

## Non-Goals

- Removing the test agent entirely (it's the path when agon is off).
- Blocking *manual* submit on agon (this gates the autopilot path only).
- Auto-resuming an agon-halted task; clearing the barrier is a human act.

## Phasing / Acceptance Criteria

Phase 1 — suppress auto-test for agon-eligible tasks. Test: agon-eligible task
skipped by `tryAutoTest` when agon on; non-session tasks fall through.

Phase 2 — auto-submit gates on agon. Test: agon-clean task auto-submits while an
agon-unresolved task does not; agon-off behavior unchanged.

Phase 3 — unresolved verdict barrier. `runAgon` parks the task and emits the
review event; no autopilot auto-resume. Test: unresolved verdict persists, task
stays waiting, autopilot does not resume it, a clean verdict clears the barrier.
