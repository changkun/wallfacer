---
title: Prompt Round Event Types
status: validated
depends_on: []
affects:
  - internal/store/events.go
  - internal/store/backend_fs.go
effort: small
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Prompt Round Event Types

## Goal

Register two new event types, `prompt_round` and `prompt_round_revert`, so task-mode rounds have a durable, auditable record on each task's event trace.

## What to do

1. **Register event types.** Add `prompt_round` and `prompt_round_revert` as recognized `EventType` values alongside `state_change`, `output`, `error`, `system`, `span_start`, `span_end` in `internal/store/events.go` (or wherever the enum lives).
2. **Payload shape.** `prompt_round` data: `{thread_id: string, round: int, prev_prompt: string, new_prompt: string, resume_hint: bool}`. `prompt_round_revert` data: `{thread_id: string, reverted_round: int}`.
3. **Persistence.** Events use the existing `InsertEvent` path; no new storage plumbing. Confirm they survive a reload through `backend_fs` by reading trace files back.
4. **Helper constructors.** Add small helpers (e.g. `NewPromptRoundEvent`, `NewPromptRoundRevertEvent`) to reduce stringly-typed construction at call sites.

## Tests

- `internal/store/events_test.go::TestPromptRoundEvent_RoundTrip` — insert event, reload store from disk, assert payload fields.
- `TestPromptRoundRevertEvent_RoundTrip` — same for the revert event.
- `TestPromptRoundEvent_ResumeHintFlag` — flag defaults to false, flips when set.

## Boundaries

- Do NOT add callers yet (tool and undo tasks own their own writes).
- Do NOT modify the existing event types or their serialization.
- Keep `resume_hint` simple: a bool, no structured annotation. Richer resume surfacing is out of scope.
