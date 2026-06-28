---
title: Agon Supersedes the Test Step (Verification Gate + Feedback Loop)
status: stale
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
2. **Feedback loop** (auto mode only): when agon finds unresolved attacks,
   auto-resume the task with the attacks as feedback — reusing the test-fail
   resume machinery — so the agent fixes them; then re-verify. Capped by
   `MaxAgonRetries`, mirroring `MaxTestFailRetries`.
3. **Auto-submit gates on agon**: an agon-eligible task auto-submits only when
   its agon verdict is clean (`AgonUnresolved == 0`), replacing the test "pass"
   requirement.

Agon **off** → the test agent path is exactly as today. Nothing is deleted.

## Design

### Task model (`store/models.go`)

```go
PendingAgonFeedback string `json:"pending_agon_feedback,omitempty"` // unresolved attacks awaiting auto-resume
AgonRetryCount      int    `json:"agon_retry_count,omitempty"`      // consecutive agon-feedback resumes
```

`MaxAgonRetries` in constants (e.g. 2 — agon cycles are expensive; lower than the
test cap of 3).

### runAgon (`tasks_autopilot.go`)

After persisting the verdict:
- `AgonUnresolved == 0` (clean): reset `AgonRetryCount` to 0, clear
  `PendingAgonFeedback`.
- `AgonUnresolved > 0` and `AgonRetryCount < MaxAgonRetries`: build feedback from
  the run's `summary.md` (agon writes the rendered attack ledger there as of
  v0.1.2) — `"Adversarial verification found N unresolved attack(s):\n\n<summary>\n\nPlease address them."` — store it in `PendingAgonFeedback` and increment
  `AgonRetryCount`. At the cap, stop (leave a system event; manual intervention).

A new `UpdateTaskAgonFeedback` store method sets the verdict + feedback +
incremented count atomically (or compose existing setters).

### Auto-resume branch (`tryAutoPromote`)

Parallel to the existing test-fail branch (`tasks_autopilot.go` Phase 1/2): a
waiting task with `PendingAgonFeedback != "" && session && AgonRetryCount <
MaxAgonRetries` is auto-resumed via `resumeWaitingTaskWithFeedbackLocked(...,
PendingAgonFeedback, TriggerFeedback, ...)`. The resume path already calls
`ClearAgonResult`; also clear `PendingAgonFeedback` there so the loop advances.

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

All three phases shipped. `agonSupersedesTest(t)` (agon enabled + session) is the
single gate: `tryAutoTest` skips such tasks, `tryAutoSubmit` requires a clean
agon verdict for them, and `tryAutoPromote` gained an agon-feedback resume branch
that mirrors the failed-test one. `runAgon` drives the cycle (feedback from
summary.md under `MaxAgonRetries=2`, reset on clean). Agon off → the test path is
unchanged. Tests cover the gate, the feedback cycle (set/cap/reset), and the
auto-resume; full handler+store suites and golangci-lint pass.

## Non-Goals

- Removing the test agent entirely (it's the path when agon is off).
- Blocking *manual* submit on agon (this gates the autopilot path only).
- Gating non-autopilot flows; the feedback loop is auto-mode only by virtue of
  living in tryAutoPromote.

## Phasing / Acceptance Criteria

Phase 1 — model + store + constants. Fields, setters, `MaxAgonRetries`, clear on
resume. Tests: setting feedback + count persists; clear resets.

Phase 2 — runAgon feedback. Build feedback from summary.md under the cap; reset
on clean. Tests: unresolved under cap sets PendingAgonFeedback + increments;
clean resets; at cap does not set feedback.

Phase 3 — autopilot wiring. Auto-resume branch; suppress auto-test for
agon-eligible; auto-submit gates on agon. Tests: agon-eligible task skipped by
tryAutoTest when agon on; pending agon feedback auto-resumes; agon-clean task
auto-submits while agon-unresolved does not; agon-off behavior unchanged.
