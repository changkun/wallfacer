---
title: Spec Drift Detection
status: drafted
depends_on:
  - specs/local/spec-coordination.md
affects:
  - internal/runner/drift.go
  - internal/store/
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

## Current State

The following infrastructure is already in place and does not need to change:

- **`internal/spec/write.go`** — `UpdateFrontmatter()` atomically writes YAML frontmatter fields (status, updated, arbitrary keys) back to spec files. Shared with the dispatch workflow.
- **Archive exemptions in `internal/spec/`** — `impact.go`, `progress.go`, and `validate.go` all skip `StatusArchived` specs. They are invisible to the live dependency graph, contribute 0 to progress counts, and are exempt from validation rules. The drift subsystem builds on the same invariant.
- **Task completion hook** — `SpecCompletionHook` (in `internal/handler/specs_dispatch.go`) is registered as `store.OnDone` at server startup (`internal/cli/server.go`). When a dispatched task reaches `done`, the hook fires in a background goroutine and calls `UpdateFrontmatter()` to set the spec's status to `complete`. The post-task drift check (§1 below) must be inserted **before** that write so it can set `stale` instead of `complete` for significantly drifted specs.

---

## What is Drift?

Drift occurs when the actual state of the codebase no longer matches what a spec describes.

| Drift type | Cause | Example |
|------------|-------|---------|
| **Implementation drift** | A dispatched leaf spec's task diverges from its spec | Spec says "add 3 methods", agent added 4 because it discovered a missing capability |
| **Design drift** | A non-leaf spec's subtree (children at any depth) collectively diverges from the parent's design | Parent describes a two-method interface, but leaf specs' implementations needed five |
| **Cross-tree drift** | A spec's assumptions are invalidated by work in a different subtree | `container-reuse.md` assumes `Launch()` returns synchronously, but `sandbox-backends/` implemented it as async |

---

## Archived Specs Are Fully Excluded

**Archived specs (`status: archived`) are invisible to the drift subsystem in every channel.** This is both a policy decision and an invariant already enforced by the existing `internal/spec/` infrastructure:

- `impact.go` returns nil edges for archived specs in both directions — they neither propagate drift to dependents nor receive staleness warnings from dependencies
- `progress.go` returns 0/0 for archived specs and masks entire archived subtrees
- `validate.go` exempts archived specs from most rules, including stale-propagation checks

The drift detection code must apply the same filter consistently across all four detection channels:

| Channel | Rule |
|---------|------|
| Post-task drift check (§1) | Skip if spec status is `archived`; archived specs cannot be dispatched so this is a defensive guard |
| Upward propagation (§2) | Stop propagation at any ancestor whose status is `archived`; do not flag archived ancestors |
| Cross-tree staleness (§3) | Exclude archived specs from the periodic scan — do not check their `affects` files or emit badges |
| DAG forward propagation (§4) | Archived specs emit no forward drift warnings; `impact.go`'s adjacency function already omits their edges |

---

## Detection Mechanisms

### 1. Post-task drift check (automatic)

Triggered by the task completion hook in `internal/handler/specs_dispatch.go`
(`SpecCompletionHook`). When a dispatched leaf spec's task reaches `done`, the hook
currently writes `status: complete` immediately. The drift check must be inserted
**before** that write so it can conditionally set `stale` instead.

**File-level drift** (deterministic, server-side):
- Extract the file list from the spec's `affects` field
- Compare against the files actually modified (from the task's `git diff`)
- Flag discrepancies: unexpected files modified, expected files not touched

**Semantic drift** (non-deterministic, agent-assisted):
- For each acceptance criterion in the spec body, classify as satisfied, diverged, not implemented, or superseded
- Compute drift level: minimal (>90% satisfied), moderate (70–90%), significant (<70%)

Based on drift level:
- **Minimal** → spec transitions to `complete` (existing hook behaviour, unchanged)
- **Moderate** → spec transitions to `complete`, drift report propagated to parents and dependents (§2 and §3)
- **Significant** → spec transitions to `stale` instead of `complete`, re-enters the iteration loop: `/wf-spec-refine` → `/wf-spec-dispatch` for a follow-up task

Produces a **drift report** appended as an `## Outcome` section on the spec file. The report feeds into upward propagation and cross-tree staleness checks.

```
Drift Report — refactor-runner.md
  Expected files: runner.go, execute.go, container.go (3 files)
  Actual files:   runner.go, execute.go, container.go, board.go, models.go (5 files)
  Unexpected:     board.go, models.go
  Criteria:       5/6 satisfied, 1 diverged
  Assessment:     Moderate drift — 2 unexpected files, 1 divergence
```

### 2. Upward propagation (children → parent)

When leaves in a non-leaf spec's subtree complete with moderate or significant drift,
the system flags non-archived ancestors as candidates for review. Propagation stops at
any ancestor whose status is `archived`.

Recursive aggregation rules:
- If any leaf in a spec's subtree has `status: stale` (significant drift), the nearest non-archived ancestor gets a **"drift: review required"** indicator
- If 2+ leaves have moderate drift reports, the nearest non-archived ancestor gets a **"drift: review suggested"** indicator
- These indicators bubble up through every non-archived ancestor

**Iteration at the parent level**: when a non-leaf spec accumulates enough child drift, the user runs `/wf-spec-refine` on the parent to update its design description. The refinement clears the drift indicators by advancing the parent's `updated` timestamp.

### 3. Cross-tree staleness (periodic)

A background check (triggered on workspace load or manually) scans `complete` specs,
**skipping archived specs entirely**:

- For each non-archived `complete` spec, check if the files in its `affects` field have been modified since `updated`
- If changes are detected (via `git log --since`), flag as a staleness candidate
- Surface in the spec explorer with a stale badge

---

## Propagation Rules

Drift propagates through two channels:

### Through the filesystem tree (upward)

Leaf drift bubbles up to non-archived ancestors:

```
sandbox-backends.md          ← flagged: 2 leaves in subtree drifted
  sandbox-backends/
    define-interface.md      ← no drift
    runner-migration.md      ← flagged: 1 leaf drifted
      runner-migration/
        refactor-launch.md   ← drift detected (touched unexpected files)
```

### Along the dependency DAG (forward)

Drift on a completed dependency warns non-archived specs that depend on it:

```
sandbox-backends/update-registry.md (complete, drift detected)
  │
  │ depends_on edge (can cross any boundary)
  ▼
cloud/container-reuse.md (validated, not archived)
  → "upstream drift" banner: "update-registry changed, review before proceeding"
```

### Rules

- **Tree propagation**: upward only — leaf drift flags all non-archived non-leaf ancestors
- **DAG propagation**: forward only — a completed spec with drift warns all non-archived specs that `depends_on` it, regardless of filesystem position
- **Archived specs are not in the live graph** in either channel; `impact.go`'s adjacency function already enforces this for the DAG — the tree propagation code must mirror it
- Warnings are advisory — they appear in the spec explorer but do not block dispatch
- A human acknowledges drift by updating the affected spec (warning clears, `updated` advances)
- **Significant drift blocks completion**: a spec with significant drift transitions to `stale`, not `complete`. It must be refined and optionally re-dispatched before reaching `complete`.
- **The iteration loop is the normal path**: most specs will go through at least one dispatch → drift → refine cycle.

---

## The `affects` Field

The `affects` field in spec frontmatter maps specs to code:

```yaml
affects:
  - internal/sandbox/
  - internal/runner/execute.go
  - internal/runner/container.go
```

This serves as the edge for targeted drift checks: when a task modifies files listed in another spec's `affects`, that spec is a candidate for staleness review. Archived specs' `affects` fields are not checked.

**Bootstrap:** At current scale, populate manually. As spec count grows, the agent can propose `affects` values during spec creation and validate against actual diffs.

---

## UI for Drift

In the spec explorer tree:

```
specs/
  ✅ sandbox-backends.md          ⚠ drift detected
  ✅ storage-backends.md
  ✔ container-reuse.md           ⚠ upstream drift (sandbox-backends)
  📝 k8s-sandbox.md
  🗄 archived-spec.md            (no badge — archived)
```

Status icons: ✅ complete, ⏳ in-progress children, ✔ validated, 📝 drafted, 💭 vague, ⚠ stale. The ⚠ icon is already defined in `ui/js/spec-explorer.js` (line 27, mapped to `stale`); drift badges reuse the same visual treatment. Archived specs receive no drift or staleness badge regardless of `affects` file changes.

In the spec detail view, drift warnings appear inline for non-archived specs:

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
| `internal/runner/drift.go` (new) | `CheckTaskDrift(taskID, spec)` — file-level drift: compare spec `affects` vs actual `git diff`; returns `DriftReport` |
| `internal/store/` | `SaveDriftReport(taskID, report)` / `GetDriftReport(taskID)` — persist drift reports alongside task data |
| `internal/handler/specs_dispatch.go` | Extend `SpecCompletionHook` to call `CheckTaskDrift` before writing status; write `stale` instead of `complete` on significant drift; skip archived specs |
| `internal/handler/specs.go` | Drift report API: `GET /api/specs/{path}/drift` — return drift report for a spec |
| `internal/handler/explorer.go` | Surface drift badges in spec tree response; propagate indicators to non-archived ancestors only |
| `ui/js/spec-explorer.js` | Render drift indicators (warning badge, "review suggested" / "review required") using existing `stale` icon; suppress for archived specs |
| `ui/js/spec-mode.js` | Inline drift warning in focused view with "Refine" and "Accept" actions; no badge for archived specs |

### Hook integration point

Drift is inserted into `SpecCompletionHook` in `internal/handler/specs_dispatch.go`,
which is called by `store.OnDone` as a background goroutine when a task reaches `done`:

1. **File-level drift check** (`internal/runner/drift.go`) runs first — deterministic, server-side
2. **Semantic drift** runs as a follow-up agent task — non-deterministic
3. **Propagation** (upward tree + forward DAG) runs after the drift report is stored; archived specs are skipped at every step
4. **Status transition** happens last: `complete` (minimal/moderate drift) or `stale` (significant drift)

The `/wf-spec-diff` command provides manual access to the same drift assessment for cases where the automatic hook isn't available or the user wants to re-run it.
