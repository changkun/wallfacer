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
3. Integrates cleanly with refinement: the refinement agent produces both a goal summary and a full spec.
4. Requires no additional LLM sandbox — goal comes directly from user input or refinement output.

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

### Goal lifecycle

**At task creation:** The user's original prompt text is copied into `Goal`. Both `Goal` and `Prompt` start with the same value. The card shows `Goal` (the user's own words).

**After refinement:** The refinement agent produces two outputs:
1. A full implementation spec → written to `Prompt`.
2. A concise goal summary (1-3 sentences) → written to `Goal`.

The card continues to show the goal (now a refinement-derived summary), while agents receive the full spec from `Prompt`.

**Agent execution:** The runner uses `Prompt` for execution. If `Prompt` is empty (edge case), it falls back to `Goal`. This is the same priority as today except `Goal` replaces the old prompt-as-display role.

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

### Manual editing

The goal should be editable through the same PATCH endpoint that handles prompt edits:

```go
// In UpdateTask request struct:
Goal *string `json:"goal"`
```

When the user edits the goal via the UI, PATCH updates it. The API accepts `goal` alongside `prompt` in both create and update paths.

### Refinement interaction

Refinement's behavior changes to produce both a spec and a goal. The refinement agent's output format is extended:

- The refinement prompt template instructs the agent to return a structured response containing both a `goal` (1-3 sentence summary) and the full `spec` (implementation details with acceptance criteria).
- `ApplyRefinement` writes the spec to `t.Prompt` and the goal to `t.Goal`.
- If the user has manually edited the goal before applying refinement, the refinement-derived goal is ignored and the manual goal is preserved. Track this with:

```go
GoalManuallySet bool `json:"goal_manually_set,omitempty"`
```

### ExecutionPrompt (idea-agent) interaction

No change. `ExecutionPrompt` remains the agent-facing override for idea-agent tasks. The goal field is orthogonal — idea-agent tasks can also have a goal for card display. Priority chain for card display becomes:

1. `taskDisplayPrompt` custom function (if defined)
2. Idea-agent with `execution_prompt` → show `execution_prompt`
3. `goal` (if present) → show `goal`
4. Fall back to `prompt`

### Search integration

Add a `goal` field to the `indexedTaskText` struct in `internal/store/store.go` and include `strings.ToLower(t.Goal)` in `buildIndexEntry`. Update `matchTask` in `internal/store/tasks_worktree.go` to also match against the `goal` field, following the same priority pattern as `title` > `prompt` > `tags` > `oversight`.

### History

`Goal` does not need its own history array. The prompt history already captures spec evolution. The goal at each refinement point is captured in the refinement session output.

---

## Implementation Phases

### Phase 1 — Data model + API (backend only)

**Files touched:**

| File | Change |
|------|--------|
| `internal/store/models.go` | Add `Goal string` and `GoalManuallySet bool` fields to `Task` |
| `internal/store/tasks_create_delete.go` | Accept `Goal` in `TaskCreateOptions`; default `Goal = Prompt` at creation |
| `internal/store/store.go` | Add `goal` field to `indexedTaskText` struct and `buildIndexEntry` |
| `internal/store/tasks_worktree.go` | Update `matchTask` for goal search |
| `internal/store/tasks_update.go` | Add `UpdateTaskGoal` method (update `Goal` field + `indexedTaskText.goal`) |
| `internal/handler/tasks.go` | Accept `goal` in `CreateTask` and `UpdateTask` request structs; set `GoalManuallySet` when user explicitly provides a goal |
| `internal/apicontract/` | Regenerate contract artifacts with new field |

**Effort:** Low — additive field, no breaking changes.

### Phase 2 — Refinement goal output

**Files touched:**

| File | Change |
|------|--------|
| `prompts/refine.tmpl` (or equivalent) | Update refinement prompt to instruct agent to produce both a goal summary and a full spec |
| `internal/runner/refine.go` | Parse refinement output to extract both goal and spec |
| `internal/store/tasks_worktree.go` | `ApplyRefinement` writes both `Prompt` (spec) and `Goal` (summary), respecting `GoalManuallySet` |
| `internal/handler/refine.go` | Pass goal through apply flow |

**Effort:** Low-Medium — requires updating the refinement prompt template and parsing the structured output.

### Phase 3 — UI

**Files touched:**

| File | Change |
|------|--------|
| `ui/js/render.js` | Update `cardDisplayPrompt` to prefer `goal`; update `_cardFingerprint` to include `goal` |
| `ui/js/render.js` (or task detail view) | Show goal as a distinct editable field in the task detail/edit modal |
| `ui/js/refine.js` | Show goal alongside the refined spec in the review panel so user can verify it still makes sense |

**Effort:** Low.

### Phase 4 — Backfill

For existing tasks that have a `prompt` but no `goal`, backfill `Goal = Prompt` (the original user text). Since pre-existing tasks were never refined through the new flow, their prompt IS their goal:
- On server startup, scan tasks where `goal == ""` and `prompt != ""`.
- Set `Goal = Prompt` for each (no LLM call needed).

**Effort:** Trivial.

---

## Migration & Backward Compatibility

- `Goal` is `omitempty` — old task JSON files without the field decode cleanly as `""`.
- `cardDisplayPrompt` falls back to `prompt` when `goal` is empty, so no UI regression.
- The API accepts but does not require `goal` on create/update — existing clients work unchanged.
- No schema version bump needed (additive field).
- Backfill is a simple copy (`Goal = Prompt`), not an LLM call.

---

## Open Questions

1. **Goal length limit?** Should we enforce a max length (e.g., 500 chars) to keep cards compact, or let the CSS `max-height` truncation handle it?
2. **Refinement output format:** What structured format should the refinement agent use to separate goal from spec? Options: JSON with `goal`/`spec` keys, XML-style tags, or a delimiter-based format.
3. **Idea-agent card display:** Should idea-agent tasks prefer `goal` over `execution_prompt` for card display, or keep the current behavior?

---

## What This Does NOT Require

- No new LLM sandbox activity — goal generation does not need a separate container or model call.
- No changes to container execution logic (`runner/execute.go`) — agents still receive `Prompt` or `ExecutionPrompt`.
- No changes to worktree, commit, or test-verification pipelines.
- No new database or file-format migration — `Goal` is just another JSON field in `task.json`.
