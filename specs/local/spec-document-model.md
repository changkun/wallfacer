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
parent: null               # path to parent spec (null for root specs)
children:                  # paths to child specs (empty for leaf specs)
  - specs/foundations/sandbox-backends/define-interface.md
  - specs/foundations/sandbox-backends/local-backend.md
  - specs/foundations/sandbox-backends/refactor-runner.md
depends_on:                # sibling specs this one requires (ordering)
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

### Leaf vs Non-Leaf Specs

| Property | Non-leaf spec | Leaf spec |
|----------|---------------|-----------|
| `children` | Non-empty list of child spec paths | Empty or absent |
| `dispatched_task_id` | Always null | Set when dispatched to kanban board |
| Content focus | Problem, motivation, design decisions, cross-cutting concerns | Which files to change, acceptance criteria, test plan |
| Granularity | Any size | Small enough for one agent task (2-5 files, one clear goal) |

A spec is a leaf if it has no children. The distinction is not a type flag — it's structural. A leaf spec can gain children later (if the user decides to break it down further instead of executing it directly).

### File Organization

Specs live in `specs/<track>/`. When a spec is broken down, its children live in a subdirectory named after the parent:

```
specs/
  foundations/
    sandbox-backends.md                    # non-leaf: design spec
    sandbox-backends/                      # children directory
      define-interface.md                  # leaf: dispatchable
      local-backend.md                     # leaf: dispatchable
      refactor-runner.md                   # leaf: dispatchable
  local/
    spec-coordination.md                   # non-leaf: this umbrella
    spec-coordination/
      spec-document-model.md              # could be leaf or non-leaf
      spec-planning-ux.md
```

Child specs are named descriptively without numeric prefixes. Execution order is determined by `depends_on`, not filename order.

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

Non-leaf specs track progress by aggregating children:

```
sandbox-backends.md — 3/5 children done
  ✓ define-interface.md (complete)
  ✓ local-backend.md (complete)
  ✓ refactor-runner.md (complete)
  ○ move-listing.md (validated, not dispatched)
  ○ retire-executor.md (drafted)
```

This aggregation is computed on the fly — no separate storage. The spec explorer and any progress views derive it from the tree.

---

## Spec Tree Structure

### Relationships

```
Root Spec (non-leaf)
├── Child A (leaf) ──depends_on──▶ Child B
├── Child B (leaf)
└── Child C (non-leaf)
    ├── Grandchild C1 (leaf)
    └── Grandchild C2 (leaf) ──depends_on──▶ Grandchild C1
```

- **Parent-child** (tree edges): structural decomposition. A parent contains its children.
- **depends_on** (DAG edges between siblings): ordering. Child A must complete before Child B can start. These become `DependsOn` on the kanban board when dispatched.
- **Cross-tree depends_on**: a spec can depend on a spec in a different subtree (e.g., `local/foo.md` depends on `foundations/bar.md`). This is how cross-cutting dependencies are expressed.

### Propagation

- **Downward** (parent → children): If a parent spec is modified, its children may be invalidated. See [spec-drift-detection.md](spec-drift-detection.md).
- **Upward** (children → parent): When children complete, the parent's progress updates. If implementation diverges from the parent's design, the parent should be updated.

---

## Operation Regimes

Not all specs need the same level of human involvement. Two regimes, determined by design certainty:

| Regime | When | Human role | Agent role |
|--------|------|------------|------------|
| **Human-driven** | Idea is vague, approach uncertain | Idea provider + steering. Reviews and redirects at every step. | Expander. Drafts, structures, asks clarifying questions. |
| **Agent-driven** | Design is clear, acceptance criteria defined | Reviewer. Monitors, provides feedback when needed. | Executor. Breaks down, dispatches, executes, reports. |

The regime is not a system mode — it's an emergent property of how the human and agent interact. The system infers it from spec maturity: `vague`/`drafted` specs are human-driven; `validated` specs can be agent-driven.

**Transition:** Human-driven → agent-driven when spec reaches `validated` and human approves. Agent-driven → human-driven when drift is detected or execution reveals the design was wrong.
