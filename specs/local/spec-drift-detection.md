# Spec Drift Detection

**Parent spec:** [spec-coordination.md](spec-coordination.md)
**Date:** 2026-03-29

Depends on the lifecycle model in [spec-document-model.md](spec-document-model.md).

---

## What is Drift?

Drift occurs when the actual state of the codebase no longer matches what a spec describes.

| Drift type | Cause | Example |
|------------|-------|---------|
| **Implementation drift** | A dispatched leaf spec's task diverges from its spec | Spec says "add 3 methods", agent added 4 because it discovered a missing capability |
| **Design drift** | A non-leaf spec's children collectively diverge from the parent's design | Parent describes a two-method interface, but children's implementations needed five |
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

When enough children of a non-leaf spec complete with drift reports, the system flags the parent as a candidate for review. The parent may need updating to reflect what was actually built.

This is simple aggregation: if 2+ children have drift reports, the parent gets a "review suggested" indicator.

### 3. Cross-tree staleness (periodic)

A background check (triggered on workspace load or manually) scans `complete` specs:

- For each spec, check if the files in its `affects` field have been modified since `updated`
- If changes are detected (via `git log --since`), flag as a staleness candidate
- Surface in the spec explorer with a stale badge

---

## Propagation Rules

Drift propagates through the spec tree:

```
sandbox-backends.md (complete, drift detected)
  │
  │ cross-tree depends_on edge
  ▼
container-reuse.md (validated)
  → "upstream drift" banner: "sandbox-backends changed, review before proceeding"
```

- **Upward**: children with drift → parent flagged for review
- **Cross-tree**: completed spec with drift → specs that `depends_on` it get warnings
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
