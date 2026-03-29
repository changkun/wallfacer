# Spec Document Model

**Parent spec:** [spec-coordination.md](spec-coordination.md)
**Date:** 2026-03-29

Defines the properties, lifecycle, and tree structure of spec documents.

---

## Spec Properties

Every spec document carries structured frontmatter:

```yaml
---
title: Sandbox Backends
status: validated          # vague | drafted | validated | complete | stale
track: foundations         # foundations | local | cloud | shared
depends_on:                # specs this one requires (DAG edges — can point anywhere)
  - specs/foundations/storage-backends.md
affects:                   # packages and files this spec describes
  - internal/sandbox/
  - internal/runner/execute.go
effort: large              # small | medium | large | xlarge
created: 2026-01-15
updated: 2026-03-28
author: changkun
dispatched_task_id: null   # UUID of kanban task (leaf specs only, set on dispatch)
---
```

**No `parent` or `children` fields.** Parent-child relationships are derived from the filesystem: a spec's parent is the spec file in its containing directory; a spec's children are the specs in its subdirectory. This avoids maintaining redundant lists that duplicate what the directory structure already provides.

### Two Relationship Types

| Relationship | Structure | Source | Purpose |
|---|---|---|---|
| **Parent-child** | Tree | Filesystem (directory nesting) | Organization, browsing, progress aggregation |
| **depends_on** | DAG | Frontmatter | Ordering, blocking, context propagation |

These are independent. A spec can depend on any other spec regardless of where they sit in the directory tree. `depends_on` is not constrained to siblings — it can cross subtrees, tracks, and depths.

### Leaf vs Non-Leaf Specs

| Property | Non-leaf spec | Leaf spec |
|----------|---------------|-----------|
| Subdirectory | Has a `<name>/` directory with child specs | No subdirectory (or empty) |
| `dispatched_task_id` | Always null | Set when dispatched to kanban board |
| Content focus | Problem, motivation, design decisions, cross-cutting concerns | Which files to change, acceptance criteria, test plan |
| Granularity | Any size | Small enough for one agent task (2-5 files, one clear goal) |

A spec is a leaf if it has no child specs in a corresponding subdirectory. The distinction is **dynamic**: any leaf spec can gain children at any time (create a subdirectory, add child specs), and this works at any depth. There is no limit on tree depth.

This means the tree grows organically:
1. Start with a single spec (leaf).
2. If it's too big to execute, break it down → create a subdirectory, add child specs. The original becomes non-leaf.
3. If a child is still too big, break *that* down the same way.
4. Repeat until every leaf is small enough to dispatch as a single task.

### File Organization

Specs live in `specs/<track>/`. When a spec is broken down, its children live in a subdirectory named after the parent. This nests to arbitrary depth:

```
specs/
  foundations/
    sandbox-backends.md                    # depth 0: non-leaf
    sandbox-backends/                      # children of sandbox-backends
      define-interface.md                  #   depth 1: leaf
      local-backend.md                     #   depth 1: leaf
      runner-migration.md                  #   depth 1: non-leaf (too big, broken down further)
      runner-migration/                    #   children of runner-migration
        refactor-launch.md                 #     depth 2: leaf
        refactor-listing.md                #     depth 2: leaf
        retire-executor.md                 #     depth 2: leaf
  local/
    spec-coordination.md                   # depth 0: non-leaf
    spec-coordination/
      spec-document-model.md              #   depth 1: leaf (or non-leaf if broken down later)
      spec-planning-ux.md
```

Child specs are named descriptively without numeric prefixes. Execution order is determined by `depends_on`, not filename order. The directory nesting mirrors the tree structure — you can always tell a spec's depth from its file path.

---

## Spec Lifecycle

A spec has one status dimension that covers both design maturity and readiness:

```
vague ──▶ drafted ──▶ validated ──▶ complete
            │          │    ▲          │
            │          │    │          │
            ▼          ▼    │          ▼
          stale      stale  └───── stale
```

| State | Meaning | Transitions |
|-------|---------|-------------|
| **vague** | Initial idea. Problem statement exists but design is incomplete. | → `drafted` (design details added) |
| **drafted** | Enough detail for review. May have open questions. | → `validated` (reviewed and approved) · → `stale` (superseded) |
| **validated** | Reviewed, approved, ready to break down or dispatch. | → `complete` (all work done) · → `stale` (invalidated) |
| **complete** | All children done (non-leaf) or task done (leaf). Spec updated to reflect reality. | → `stale` (if later work modifies what this spec describes) |
| **stale** | Spec no longer matches reality. Needs human review. | → `drafted` (refreshed) · → `validated` (re-validated) |

### Lifecycle Rules

- **Leaf specs** should be `validated` before dispatch. Don't execute against a half-baked design.
- **Non-leaf specs** should be `validated` before breaking down into children.
- When all children of a non-leaf are `complete`, the parent can move to `complete` — after verifying the implementation matches the design. If there's a significant delta, go to `stale` first.
- `stale` signals "this document needs human attention." It's not a dead end.

### Progress Tracking

Non-leaf specs track progress by **recursively** aggregating all leaves in their subtree — not just direct children:

```
sandbox-backends.md — 4/6 leaves done
  ✓ define-interface.md (complete, leaf)
  ✓ local-backend.md (complete, leaf)
    runner-migration.md — 2/3 leaves done
      ✓ refactor-launch.md (complete, leaf)
      ✓ refactor-listing.md (complete, leaf)
      ○ retire-executor.md (validated, leaf)
  ○ update-registry.md (drafted, leaf)
```

Here `sandbox-backends.md` counts 4/6 (all leaves in the subtree), not 2/3 (direct children). `runner-migration.md` counts 2/3 (its own leaves). The counts compose upward through any number of levels.

This aggregation is computed on the fly — no separate storage. The spec explorer and any progress views derive it from the tree.

---

## Spec Relationships

### Filesystem Tree (Organization)

```
specs/
  foundations/
    sandbox-backends.md
    sandbox-backends/
      define-interface.md
      local-backend.md
      runner-migration.md
      runner-migration/
        refactor-launch.md
        refactor-listing.md
        retire-executor.md
```

The filesystem determines parent-child relationships. `sandbox-backends.md` is the parent of everything in `sandbox-backends/`. This is purely organizational — it determines how specs are browsed in the explorer and how progress aggregates upward.

### Dependency DAG (Ordering)

```
define-interface.md ──────────────▶ local-backend.md
        │                                  │
        │                                  ▼
        │                          runner-migration/
        │                            refactor-launch.md
        │                                  │
        │                                  ▼
        │                            refactor-listing.md
        │                                  │
        │                                  ▼
        │                            retire-executor.md
        │
        └──────────────────────────▶ update-registry.md
                                           │
                            (cross-tree)   ▼
                                    container-reuse.md
                                    (different subtree)
```

`depends_on` edges form a DAG that can cross any boundary:
- **Between siblings**: `local-backend.md` depends on `define-interface.md` (same directory)
- **Across depths**: `refactor-launch.md` depends on `local-backend.md` (child depends on parent's sibling)
- **Across subtrees**: `container-reuse.md` depends on `update-registry.md` (different track entirely)

When leaf specs are dispatched, `depends_on` becomes `DependsOn` on the kanban board.

**Only leaf specs are dispatched.** When a non-leaf spec has `depends_on`, it means "all leaves in this subtree are blocked until the dependency is complete." This allows expressing ordering between groups of work without listing every individual leaf dependency.

### Propagation

- **Upward through the tree** (children → parent): When children complete, the parent's progress updates. If implementation diverges from the parent's design, the parent should be updated.
- **Downward through the tree** (parent → children): If a parent spec is modified, its children may be invalidated. See [spec-drift-detection.md](spec-drift-detection.md).
- **Along DAG edges** (dependency → dependent): If a completed dependency drifts, specs that depend on it get warnings. This follows `depends_on` edges regardless of tree position.

---

## Operation Regimes

Not all specs need the same level of human involvement. Two regimes, determined by design certainty:

| Regime | When | Human role | Agent role |
|--------|------|------------|------------|
| **Human-driven** | Idea is vague, approach uncertain | Idea provider + steering. Reviews and redirects at every step. | Expander. Drafts, structures, asks clarifying questions. |
| **Agent-driven** | Design is clear, acceptance criteria defined | Reviewer. Monitors, provides feedback when needed. | Executor. Breaks down, dispatches, executes, reports. |

The regime is not a system mode — it's an emergent property of how the human and agent interact. The system infers it from spec maturity: `vague`/`drafted` specs are human-driven; `validated` specs can be agent-driven.

**Transition:** Human-driven → agent-driven when spec reaches `validated` and human approves. Agent-driven → human-driven when drift is detected or execution reveals the design was wrong.
