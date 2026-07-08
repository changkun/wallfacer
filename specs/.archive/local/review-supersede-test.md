---
title: Review Supersedes the Test Step (Verification Gate)
status: archived
depends_on:
  - review-adversarial-verification
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

# Review Supersedes the Test Step

## Problem

Review and the single test agent are both post-implementation verification gates.
Today review is purely informational (badge + panel), while the test agent drives
the real autopilot loop: a failing test auto-resumes the task with feedback, and
a passing test gates auto-submit. Running both under autopilot is redundant cost
and two verdicts to reconcile. The user wants review, when enabled, to **replace**
the test agent's role — but only when enabled (review is opt-in and expensive; the
test path must stay intact as the default).

## Decision

When **review is enabled** and a task is **review-eligible** (has a Claude
`session_id`), review supersedes the test agent for that task under autopilot:

1. **Suppress auto-test** for review-eligible tasks (no double verification).
   Non-session tasks still use the test agent even when review is on (review can't
   run without a fork-session).
2. **Unresolved verdict is a hard barrier**: when review finds unresolved attacks,
   the task is parked in `waiting` and autopilot does not advance it. Clearing
   the barrier is an explicit human act — confirm the work, or resume with
   steering (which calls `ClearReviewResult` and triggers fresh re-verification).
   Autopilot does not auto-resume the task on its own.
3. **Auto-submit gates on review**: an review-eligible task auto-submits only when
   its review verdict is clean (`ReviewUnresolved == 0`), replacing the test "pass"
   requirement.

Review **off** → the test agent path is exactly as today. Nothing is deleted.

## Design

### Task model (`store/models.go`)

No new fields. The existing review verdict fields (`ReviewUnresolved`,
`ReviewHeadline`, `ReviewSessionDir`) carry the barrier state: a non-nil
`ReviewUnresolved > 0` is the parked-for-review signal. `ClearReviewResult` wipes the
verdict on resume so the re-worked diff is re-verified.

### runReview (`tasks_autopilot.go`)

After persisting the verdict:
- `ReviewUnresolved == 0` (clean): emit a "verification clean" timeline event; the
  task is eligible for auto-submit.
- `ReviewUnresolved > 0` (unresolved): emit a "task halted for review — confirm or
  resume with steering to re-verify" timeline event. The task stays in `waiting`;
  autopilot does not auto-resume it. The verdict (and `ReviewHeadline`) drives the
  UI badge/panel so the unresolved attacks are visible.

### Human steering (`resumeWaitingTaskWithFeedbackLocked` / manual resume)

There is no autopilot auto-resume branch for review. A human clears the barrier by
resuming the task (optionally with steering feedback); the resume path calls
`ClearReviewResult`, dropping the stale verdict so the next run re-verifies the new
work. Re-failure re-blocks. (An earlier design auto-resumed with the attacks as
feedback up to `MaxReviewRetries`; that loop was dropped in favor of explicit human
confirmation, so the verdict is a deterministic barrier rather than a cost-capped
retry cycle.)

### Suppress auto-test (`tryAutoTest`)

In the Phase-1 candidate filter, skip a task when `h.ReviewEnabled() && t.SessionID
!= nil` — review will verify it. Non-session tasks fall through to the test agent.

### Auto-submit gate (`tryAutoSubmit`)

Replace the `tested := t.LastTestResult == "pass"` gate with a helper:

```go
func (h *Handler) taskVerifiedForSubmit(t *store.Task) bool {
    if h.ReviewEnabled() && t.SessionID != nil && *t.SessionID != "" {
        return t.ReviewUnresolved != nil && *t.ReviewUnresolved == 0
    }
    return t.LastTestResult == "pass"
}
```

The `naturallyComplete` branch (end_turn, untested, autotest off) must also wait
for review when the review gate applies — i.e. don't naturally-submit an
review-eligible task that review hasn't cleared.

## Outcome (2026-06-28)

`reviewSupersedesTest(t)` (review enabled + session) is the single gate: `tryAutoTest`
skips such tasks and `tryAutoSubmit` requires a clean review verdict for them. An
unresolved verdict is a hard barrier — `runReview` parks the task in `waiting` with
a "halted for review" timeline event and autopilot does not advance it; a human
confirms or resumes with steering (`ClearReviewResult` re-arms verification). Review
off → the test path is unchanged.

The design initially shipped with an auto-resume feedback loop (attacks fed back
to the implementation agent, capped at `MaxReviewRetries=2`). That loop was removed
in a follow-up: the `PendingReviewFeedback` / `ReviewRetryCount` fields,
`MaxReviewRetries`, the `SetReviewFeedback` / `ResetReviewRetry` setters, and the
`tryAutoPromote` review-resume branch are gone. The verdict is now a deterministic
human-gated barrier rather than a cost-capped retry cycle. `TestRunReview_BlocksOnUnresolved`
covers it (verdict persists, task stays waiting, no auto-resume, clean clears the
barrier); full handler+store suites and golangci-lint pass.

## Non-Goals

- Removing the test agent entirely (it's the path when review is off).
- Blocking *manual* submit on review (this gates the autopilot path only).
- Auto-resuming an review-halted task; clearing the barrier is a human act.

## Phasing / Acceptance Criteria

Phase 1 — suppress auto-test for review-eligible tasks. Test: review-eligible task
skipped by `tryAutoTest` when review on; non-session tasks fall through.

Phase 2 — auto-submit gates on review. Test: review-clean task auto-submits while an
review-unresolved task does not; review-off behavior unchanged.

Phase 3 — unresolved verdict barrier. `runReview` parks the task and emits the
review event; no autopilot auto-resume. Test: unresolved verdict persists, task
stays waiting, autopilot does not resume it, a clean verdict clears the barrier.
