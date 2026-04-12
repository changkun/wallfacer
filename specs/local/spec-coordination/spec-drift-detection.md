---
title: Spec Drift Detection
status: drafted
depends_on:
  - specs/local/spec-coordination.md
affects:
  - internal/runner/drift.go
  - internal/store/
  - internal/spec/
  - internal/handler/explorer.go
  - internal/handler/specs.go
  - ui/js/spec-explorer.js
  - ui/js/spec-mode.js
effort: large
created: 2026-03-29
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Spec Drift Detection

Depends on the lifecycle model in [spec-document-model.md](spec-document-model.md).

---

## What is Drift?

Drift occurs when the actual state of the codebase no longer matches what a spec describes.

| Drift type | Cause | Example |
|------------|-------|---------|
| **Implementation drift** | A dispatched leaf spec's task diverges from its spec | Spec says "add 3 methods", agent added 4 because it discovered a missing capability |
| **Design drift** | A non-leaf spec's subtree (children at any depth) collectively diverges from the parent's design | Parent describes a two-method interface, but leaf specs' implementations needed five |
| **Cross-tree drift** | A spec's assumptions are invalidated by work in a different subtree | `container-reuse.md` assumes `Launch()` returns synchronously, but `sandbox-backends/` implemented it as async |

---

## Detection Mechanisms

### 1. Post-task drift check (automatic)

Triggered by the dispatch workflow's task completion hook (see [dispatch-workflow.md](spec-planning-ux/dispatch-workflow.md), layer 2). When a dispatched leaf spec's task reaches `done`, the server-side hook sets the spec to `done` (not `complete`) and records the commit range. Then drift assessment runs:

**File-level drift** (deterministic, server-side):
- Extract the file list from the spec's `affects` field
- Compare against the files actually modified (from the task's `git diff`)
- Flag discrepancies: unexpected files modified, expected files not touched

**Semantic drift** (non-deterministic, agent-assisted):
- For each acceptance criterion in the spec body, classify as satisfied, diverged, not implemented, or superseded
- Compute drift level: minimal (>90% satisfied), moderate (70-90%), significant (<70%)

Based on drift level:
- **Minimal** → spec transitions `done` → `complete`
- **Moderate** → spec transitions to `complete`, drift report propagated to parents and dependents (section 2 and 3 below)
- **Significant** → spec transitions to `stale`, re-enters the iteration loop: `/wf-spec-refine` → `/wf-spec-dispatch` for a follow-up task

Produces a **drift report** stored as an `## Outcome` section on the spec. The report feeds into upward propagation and cross-tree staleness checks.

```
Drift Report — refactor-runner.md
  Expected files: runner.go, execute.go, container.go (3 files)
  Actual files:   runner.go, execute.go, container.go, board.go, models.go (5 files)
  Unexpected:     board.go, models.go
  Criteria:       5/6 satisfied, 1 diverged
  Assessment:     Moderate drift — 2 unexpected files, 1 divergence
```

### 2. Upward propagation (children → parent)

When leaves in a non-leaf spec's subtree complete with moderate or significant drift, the system flags ancestors as candidates for review. Drift propagates upward through all levels — if a deeply nested leaf drifts, every ancestor up to the root is a candidate.

Recursive aggregation rules:
- If any leaf in a spec's subtree has `status: stale` (significant drift), the parent gets a **"drift: review required"** indicator
- If 2+ leaves have moderate drift reports, the parent gets a **"drift: review suggested"** indicator
- These indicators bubble up through every non-leaf ancestor

The parent spec's design may no longer match what its children implemented. For example, a parent describes a two-method interface, but leaf implementations needed five methods. The parent should be refined to document the actual design.

**Iteration at the parent level**: When a non-leaf spec accumulates enough child drift, the user runs `/wf-spec-refine` on the parent to update its design description. This doesn't require re-dispatching — the parent is a design document, not an implementation task. The refinement clears the drift indicators by advancing the parent's `updated` timestamp and updating its content to match reality.

### 3. Cross-tree staleness (periodic)

A background check (triggered on workspace load or manually) scans `complete` specs:

- For each spec, check if the files in its `affects` field have been modified since `updated`
- If changes are detected (via `git log --since`), flag as a staleness candidate
- Surface in the spec explorer with a stale badge

**Archived specs are exempt.** The periodic scan, post-task drift check, upward propagation, and DAG forward propagation all skip archived specs (see [spec-archival.md](spec-archival.md)). Archival is an explicit "stop surfacing this" signal — the drift subsystem treats archived specs as outside the live graph in every channel.

---

## Propagation Rules

Drift propagates through two channels:

### Through the filesystem tree (upward)

Leaf drift bubbles up to ancestors:

```
sandbox-backends.md          ← flagged: 2 leaves in subtree drifted
  sandbox-backends/
    define-interface.md      ← no drift
    runner-migration.md      ← flagged: 1 leaf drifted
      runner-migration/
        refactor-launch.md   ← drift detected (touched unexpected files)
```

### Along the dependency DAG (forward)

Drift on a completed dependency warns specs that depend on it:

```
sandbox-backends/update-registry.md (complete, drift detected)
  │
  │ depends_on edge (can cross any boundary)
  ▼
cloud/container-reuse.md (validated)
  → "upstream drift" banner: "update-registry changed, review before proceeding"
```

### Rules

- **Tree propagation**: upward only — leaf drift flags all non-leaf ancestors
- **DAG propagation**: forward only — completed spec with drift warns all specs that `depends_on` it, regardless of filesystem position
- Warnings are advisory — they appear in the spec explorer but do not block dispatch
- A human acknowledges drift by updating the affected spec (warning clears, `updated` advances)
- **Significant drift blocks completion**: a spec with significant drift transitions to `stale`, not `complete`. It must be refined and optionally re-dispatched before it can reach `complete`. This prevents the spec tree from accumulating "complete" specs that don't match reality.
- **The iteration loop is the normal path**: most specs will go through at least one dispatch → drift → refine cycle. The system should make this loop lightweight, not penalize it.

---

## The `affects` Field

The `affects` field in spec frontmatter maps specs to code:

```yaml
affects:
  - internal/sandbox/
  - internal/runner/execute.go
  - internal/runner/container.go
```

This serves as the edge for targeted drift checks: when a task modifies files listed in another spec's `affects`, that spec is a candidate for staleness review.

**Bootstrap:** At current scale (~20 specs), populate manually. As spec count grows, the agent can propose `affects` values during spec creation and validate against actual diffs.

---

## UI for Drift

In the spec explorer tree:

```
specs/
  ✅ sandbox-backends.md          ⚠ drift detected
  ✅ storage-backends.md
  ✔ container-reuse.md           ⚠ upstream drift (sandbox-backends)
  📝 k8s-sandbox.md
```

Status icons: ✅ complete, ⏳ in-progress children, ✔ validated, 📝 drafted, 💭 vague, ⚠ stale

In the spec detail view, drift warnings appear inline:

```
⚠ This spec may be stale. sandbox-backends.md (which this spec depends on)
  completed with implementation drift. Review assumptions before dispatching.
  [Review Changes] [Dismiss]
```

---

## Codebase Index Strategy

Three approaches for semantic drift assessment:

| Approach | Description | Tradeoff |
|----------|-------------|----------|
| **A: Full dump** | Feed everything to agent | Simple but doesn't scale |
| **B: `affects` field** | Targeted file-level mapping | Cheap, precise, useful beyond drift |
| **C: Model capability** | Agent reads spec + source, judges semantically | Zero infrastructure, improves with models |

**Recommendation:** Use the `affects` field (B) for targeted file-change detection. Use model capability (C) for semantic assessment when drift is flagged. Defer structural indexing until codebase exceeds ~500K LOC.

---

## Implementation

| File | Change |
|---|---|
| `internal/runner/drift.go` (new) | `CheckTaskDrift(taskID)` — file-level drift: compare spec `affects` vs actual `git diff` |
| `internal/store/` | `SaveDriftReport(taskID, report)` / `GetDriftReport(taskID)` — persist drift reports |
| `internal/spec/` | `UpdateFrontmatter()` — write `status`, `updated`, drift metadata back to spec files (shared with dispatch-workflow) |
| `internal/handler/explorer.go` | Surface drift badges in spec tree view, propagate indicators to ancestors |
| `internal/handler/specs.go` | Drift report API: `GET /api/specs/{path}/drift` — return drift report for a spec |
| `ui/js/spec-explorer.js` | Render drift indicators (warning badge, "review suggested" / "review required") |
| `ui/js/spec-mode.js` | Inline drift warning in focused view with "Refine" and "Accept" actions |

### Hooks into dispatch-workflow

This spec's detection mechanisms are triggered by the dispatch-workflow's task completion hook:

1. **Layer 1** (dispatch-workflow item 5) sets `status: done` and records commit range
2. **File-level drift** (this spec, section 1) runs immediately after layer 1 — deterministic, server-side
3. **Semantic drift** (this spec, section 1) runs as a follow-up agent task — non-deterministic
4. **Propagation** (this spec, sections 2-3) runs after the drift report is stored
5. **Status transition** happens after drift assessment: `done` → `complete` or `done` → `stale`

The `/diff` and `/wf-spec-diff` commands provide manual access to the same drift assessment for cases where the automatic hook isn't available or the user wants to re-run it.
