---
title: Spec Drift Detection
status: drafted
depends_on:
  - specs/local/spec-coordination.md
affects:
  - internal/runner/drift.go
  - internal/store/
  - internal/handler/explorer.go
effort: large
created: 2026-03-29
updated: 2026-03-30
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

When a dispatched leaf spec's task reaches `done`:

- Extract the file list from the spec's `affects` field and content
- Compare against the files actually modified (from `git diff`)
- Flag discrepancies: unexpected files modified, expected files not touched

Produces a **drift report** stored alongside the task. Informational — surfaces as a warning but does not block.

```
Drift Report — refactor-runner.md
  Expected files: runner.go, execute.go, container.go (3 files)
  Actual files:   runner.go, execute.go, container.go, board.go, models.go (5 files)
  Unexpected:     board.go, models.go
  Assessment:     Moderate drift — 2 unexpected files touched
```

### 2. Upward propagation (children → parent)

When enough leaves in a non-leaf spec's subtree complete with drift reports, the system flags ancestors as candidates for review. Drift propagates upward through all levels — if a deeply nested leaf drifts, every ancestor up to the root is a candidate.

This is recursive aggregation: if 2+ leaves anywhere in a spec's subtree have drift reports, that spec gets a "review suggested" indicator. The indicator bubbles up through every non-leaf ancestor.

### 3. Cross-tree staleness (periodic)

A background check (triggered on workspace load or manually) scans `complete` specs:

- For each spec, check if the files in its `affects` field have been modified since `updated`
- If changes are detected (via `git log --since`), flag as a staleness candidate
- Surface in the spec explorer with a stale badge

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
| `internal/runner/drift.go` (new) | `CheckTaskDrift(taskID)` — compare spec expectations vs actual diff |
| `internal/store/` | `SaveDriftReport(taskID, report)` / `GetDriftReport(taskID)` |
| `internal/handler/explorer.go` | Parse spec frontmatter, surface status and drift badges in tree view |
