# Task Goal Field — Separating Human-Readable Goal from Agent Spec

**Status:** Draft
**Date:** 2026-03-21

---

## Problem

Today the `Prompt` field serves double duty: it is both the human-readable description shown on task cards **and** the full implementation spec consumed by agents. After refinement replaces the prompt with a detailed spec, the card preview becomes a wall of technical text that is hard to scan. The auto-generated `Title` (2-5 words) is too terse to compensate.

What users actually need on the card is a concise **goal** — one or two sentences explaining *what* the task achieves and *why* — while agents need the full spec with acceptance criteria, file lists, and step-by-step instructions.

---

## Goal

Introduce a `Goal` field on every task that:

1. Gives humans a scannable, meaningful card preview (replacing the raw prompt preview).
2. Preserves the full spec in `Prompt` for agent execution — no behavioral change to the runner.
3. Integrates cleanly with refinement: refinement rewrites the spec but the goal stays stable.
4. Is auto-generated from the prompt at creation time (like `Title`) so users don't have to write it manually.

---

## Design

### New field

```go
// In store/models.go, Task struct:
Goal string `json:"goal,omitempty"`  // 1-3 sentence human-readable summary
```

`Goal` sits alongside `Title` (ultra-short label) and `Prompt` (full spec). The three form a hierarchy:

| Field | Length | Audience | Example |
|-------|--------|----------|---------|
| `Title` | 2-5 words | Tab/list header | "WebSocket auth middleware" |
| `Goal` | 1-3 sentences | Card preview | "Add JWT-based authentication to the WebSocket upgrade handler so that unauthenticated clients are rejected before the connection is established." |
| `Prompt` | Unbounded | Agent | Full spec with acceptance criteria, files to touch, constraints… |

### Card rendering change

`cardDisplayPrompt(t)` currently returns `t.prompt` (or `t.execution_prompt` for idea-agent tasks). Change it to prefer `t.goal` when present:

```javascript
function cardDisplayPrompt(t) {
  if (typeof taskDisplayPrompt === 'function') return taskDisplayPrompt(t);
  if (t && t.kind === 'idea-agent' && t.execution_prompt) return t.execution_prompt;
  // NEW: prefer goal for card display
  if (t && t.goal) return t.goal;
  return t ? t.prompt : '';
}
```

This is fully backward-compatible: tasks without a goal fall back to showing the prompt as today.

### Auto-generation

Reuse the same `GenerateTitleBackground` pattern. After task creation and after refinement-apply, fire a background job that produces the goal from the current prompt. Concretely:

1. Add a new prompt template `GoalGeneration` (parallel to `TitleGeneration`) that instructs the LLM: *"Summarize the following task spec into 1-3 sentences describing what the task achieves and why. Do not include implementation details."*
2. Add `Runner.GenerateGoalBackground(taskID, prompt)` — spawns a goroutine, calls the LLM, stores the result via `Store.UpdateTaskGoal(id, goal)`.
3. Call sites (all in `internal/handler/`):
   - `CreateTask` (`tasks.go:208`): after `GenerateTitleBackground`, also call `GenerateGoalBackground`.
   - `RefineApply` (`refine.go:176`): after `GenerateTitleBackground`, also call `GenerateGoalBackground` (conditional on `!GoalManuallySet`).
   - `BatchCreateTasks` (`tasks.go:494`): same as `CreateTask`.
   - `tasks_events.go:217` (title backfill endpoint): add parallel goal backfill or expose a separate goal backfill endpoint.

### Manual editing

The goal should be editable through the same PATCH endpoint that handles prompt edits:

```go
// In UpdateTask request struct:
Goal *string `json:"goal"`
```

When the user edits the goal via the UI, PATCH updates it. The API accepts `goal` alongside `prompt` in both create and update paths.

### Refinement interaction

Refinement's core behavior is unchanged — it still rewrites the **prompt** (the full spec). The key design decision:

- **Refinement does NOT overwrite the goal.** The goal captures the user's original intent and should remain stable across spec rewrites. If the user wants to change the goal, they edit it directly.
- `ApplyRefinement` continues to update `t.Prompt` only. No change to `ApplyRefinement` logic.
- After apply, `GenerateGoalBackground` is called to re-derive the goal from the new spec — but only if the goal was auto-generated (not manually edited). Track this with a boolean:

```go
GoalManuallySet bool `json:"goal_manually_set,omitempty"`
```

If `GoalManuallySet` is true, skip re-generation after refinement. If false, regenerate to keep the auto-summary in sync with the updated spec.

### ExecutionPrompt (idea-agent) interaction

No change. `ExecutionPrompt` remains the agent-facing override for idea-agent tasks. The goal field is orthogonal — idea-agent tasks can also have a goal for card display. Priority chain for card display becomes:

1. `taskDisplayPrompt` custom function (if defined)
2. Idea-agent with `execution_prompt` → show `execution_prompt`
3. `goal` (if present) → show `goal`
4. Fall back to `prompt`

### Search integration

Add a `goal` field to the `indexedTaskText` struct in `internal/store/store.go` and include `strings.ToLower(t.Goal)` in `buildIndexEntry`. Update `matchTask` in `internal/store/tasks_worktree.go` to also match against the `goal` field, following the same priority pattern as `title` > `prompt` > `tags` > `oversight`.

### History

`Goal` does not need its own history array. The prompt history already captures spec evolution. If needed, the goal at each refinement point can be derived from the `RefinementSession.StartPrompt`.

---

## Implementation Phases

### Phase 1 — Data model + API (backend only)

**Files touched:**

| File | Change |
|------|--------|
| `internal/store/models.go` | Add `Goal string` and `GoalManuallySet bool` fields to `Task` |
| `internal/store/tasks_create_delete.go` | Accept `Goal` in `TaskCreateOptions`; pass through |
| `internal/store/store.go` | Add `goal` field to `indexedTaskText` struct and `buildIndexEntry` |
| `internal/store/tasks_worktree.go` | No change to `ApplyRefinement`; update `matchTask` for goal search |
| `internal/handler/tasks.go` | Accept `goal` in `CreateTask` and `UpdateTask` request structs |
| `internal/handler/refine.go` | After apply, call `GenerateGoalBackground` if `!GoalManuallySet` |
| `internal/apicontract/` | Regenerate contract artifacts with new field |

**Effort:** Low — additive field, no breaking changes.

### Phase 2 — Auto-generation

**Files touched:**

| File | Change |
|------|--------|
| `internal/store/models.go` | Add `SandboxActivityGoal SandboxActivity = "goal"` constant and register in `AllSandboxActivities` |
| `internal/runner/container.go` | Add `activityGoal` alias and model-selection case |
| `prompts/` | Add `goal.tmpl` template; register in `embeddedToAPI` map in `prompts/prompts.go` |
| `internal/runner/goal.go` (new) | Add `GenerateGoal` (mirrors `title.go` pattern) |
| `internal/runner/runner.go` | Add `GenerateGoalBackground` wrapper using `backgroundWg` (same as title) |
| `internal/runner/interface.go` | Add `GenerateGoalBackground(taskID uuid.UUID, prompt string)` to runner interface |
| `internal/runner/mock.go` | Add `GenerateGoalBackground` stub + `GenerateGoalCalls` tracking slice |
| `internal/store/tasks_update.go` | Add `UpdateTaskGoal` method (update `Goal` field + `indexedTaskText.goal`) |
| `internal/handler/tasks.go` | Call `GenerateGoalBackground` after create |
| `internal/handler/refine.go` | Call `GenerateGoalBackground` after apply (conditional) |

**Effort:** Low-Medium — mirrors existing title generation pattern.

### Phase 3 — UI

**Files touched:**

| File | Change |
|------|--------|
| `ui/js/render.js` | Update `cardDisplayPrompt` to prefer `goal`; update `_cardFingerprint` to include `goal` |
| `ui/js/render.js` (or task detail view) | Show goal as a distinct editable field in the task detail/edit modal |
| `ui/js/refine.js` | Show goal alongside the refined spec in the review panel so user can verify it still makes sense |

**Effort:** Low.

### Phase 4 — Backfill

For existing tasks that have a `prompt` but no `goal`, run a one-time backfill:
- On server startup, scan tasks where `goal == ""` and `prompt != ""`.
- Queue `GenerateGoalBackground` for each (rate-limited to avoid LLM burst).
- Or: expose a `POST /api/admin/backfill-goals` endpoint for manual trigger.

**Effort:** Low.

---

## Migration & Backward Compatibility

- `Goal` is `omitempty` — old task JSON files without the field decode cleanly as `""`.
- `cardDisplayPrompt` falls back to `prompt` when `goal` is empty, so no UI regression.
- The API accepts but does not require `goal` on create/update — existing clients work unchanged.
- No schema version bump needed (additive field).

---

## Open Questions

1. **Goal length limit?** Should we enforce a max length (e.g., 500 chars) to keep cards compact, or let the CSS `max-height` truncation handle it?
2. **Goal in refinement review panel:** Should the refine-apply UI show the current goal and let the user edit it inline, or keep it as a separate edit after apply?
3. **Idea-agent card display:** Should idea-agent tasks prefer `goal` over `execution_prompt` for card display, or keep the current behavior?

---

## What This Does NOT Require

- No changes to container execution logic (`runner/execute.go`) — agents still receive `Prompt` or `ExecutionPrompt`.
- No changes to worktree, commit, or test-verification pipelines.
- No new database or file-format migration — `Goal` is just another JSON field in `task.json`.
- No changes to the refinement sandbox agent's behavior — it still produces specs, not goals.
