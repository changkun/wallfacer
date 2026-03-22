# Task Goal Field — Separating Human-Readable Goal from Agent Spec

**Status:** Done
**Date:** 2026-03-21
**Completed:** 2026-03-22

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
Goal           string `json:"goal,omitempty"`            // 1-3 sentence human-readable summary
GoalManuallySet bool  `json:"goal_manually_set,omitempty"` // true when user explicitly edited goal
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

### Card rendering

`cardDisplayPrompt(t)` prefers `t.goal` when present:

```javascript
function cardDisplayPrompt(t) {
  if (typeof taskDisplayPrompt === 'function') return taskDisplayPrompt(t);
  if (t && t.kind === 'idea-agent' && t.execution_prompt) return t.execution_prompt;
  if (t && t.goal) return t.goal;
  return t ? t.prompt : '';
}
```

Fully backward-compatible: tasks without a goal fall back to showing the prompt.

### Manual editing

The goal is editable through the PATCH endpoint alongside prompt edits:

```go
// In UpdateTask request struct:
Goal *string `json:"goal"`
```

When the user edits the goal via the UI, PATCH updates it and sets `GoalManuallySet = true`.

### Refinement interaction

The refinement agent produces both a spec and a goal:

- The refinement prompt template instructs the agent to return a `# Goal` section followed by the full spec.
- `extractGoalFromRefinement` parses the output to separate goal from spec.
- `ApplyRefinement` writes the spec to `t.Prompt` and the goal to `t.Goal`.
- If `GoalManuallySet == true`, the refinement-derived goal is ignored and the manual goal is preserved.

### ExecutionPrompt (idea-agent) interaction

No change. `ExecutionPrompt` remains the agent-facing override for idea-agent tasks. Priority chain for card display:

1. `taskDisplayPrompt` custom function (if defined)
2. Idea-agent with `execution_prompt` → show `execution_prompt`
3. `goal` (if present) → show `goal`
4. Fall back to `prompt`

### Search integration

The `goal` field is included in `indexedTaskText` and `buildIndexEntry`. `matchTask` searches goal with priority: title → goal → prompt → tags → oversight.

### History

`Goal` does not need its own history array. The prompt history captures spec evolution. The goal at each refinement point is captured in the refinement session output.

---

## Implementation Summary

All phases are complete.

| Phase | Scope | Status |
|-------|-------|--------|
| Phase 1 — Data model + API | `Goal` and `GoalManuallySet` fields on `Task`; creation defaults `Goal = Prompt`; PATCH accepts `goal`; search index includes goal | Done |
| Phase 2 — Refinement goal output | Refinement prompt produces `# Goal` section; `extractGoalFromRefinement` parses it; `ApplyRefinement` respects `GoalManuallySet` | Done |
| Phase 3 — UI | `cardDisplayPrompt` prefers goal; goal textarea in task detail modal and refinement review panel; auto-save on input | Done |
| Phase 4 — Backfill | Tasks without goal default `Goal = Prompt` at creation time; no startup migration needed since `omitempty` handles empty gracefully | Done |

---

## Migration & Backward Compatibility

- `Goal` is `omitempty` — old task JSON files without the field decode cleanly as `""`.
- `cardDisplayPrompt` falls back to `prompt` when `goal` is empty, so no UI regression.
- The API accepts but does not require `goal` on create/update — existing clients work unchanged.
- No schema version bump needed (additive field).

---

## Resolved Questions

1. **Goal length limit:** No hard limit enforced; CSS `max-height` truncation handles card compactness.
2. **Refinement output format:** Markdown heading-based — `# Goal` section followed by implementation spec sections.
3. **Idea-agent card display:** Idea-agent tasks keep `execution_prompt` priority over `goal` for card display.
