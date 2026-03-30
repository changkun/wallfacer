---
title: Spec Coordination Layer
status: drafted
depends_on:
  - specs/foundations/file-explorer.md
affects:
  - internal/handler/explorer.go
  - internal/store/
effort: xlarge
created: 2026-03-29
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Spec Coordination Layer

## Problem

Wallfacer's flat kanban board (backlog → in_progress → waiting → done) works well for independent tasks but cannot coordinate large, multi-step engineering work. A single "implement feature X" task is too large for one agent turn, but splitting into many small tasks loses coherence — each agent starts fresh with no knowledge of what previous agents decided.

Concrete gaps:

1. **No structured decomposition.** Humans must manually break work into tasks, wire dependencies, and size them correctly.
2. **No cross-task context.** Board.json truncates sibling prompts to 500 chars and results to 1000 chars. Task B doesn't know what task A decided about interface shapes, naming, or architecture.
3. **No iterative planning.** No way to draft a plan, review it, revise it, and then execute — it's "create tasks and go."
4. **No progress tracking for grouped work.** No way to see "3/7 done, 1 in progress, 3 backlog" at a glance for related tasks.

## Design Principle

**Specs are the only organizing artifact.** There are no epics, phases, or gates as separate concepts. All planning, decomposition, and coordination happens through a recursive tree of spec documents. Leaf specs are dispatched directly as kanban tasks. Non-leaf specs are organizational — they contain motivation, design, and links to children.

The workflow mirrors what humans naturally do: start with a vague idea, write it down, break it into smaller pieces, iterate on each piece, and eventually execute the pieces that are small enough.

---

## How It Works

### The Recursive Spec Tree

```
Idea (vague)
  ↓  agent drafts a spec
Spec (too big to execute directly)
  ↓  break down into sub-specs
  ├── Sub-spec A (leaf — dispatchable as a task)
  ├── Sub-spec B (still too big)
  │     ├── Sub-spec B1 (leaf — dispatchable)
  │     └── Sub-spec B2 (leaf — dispatchable)
  └── Sub-spec C (leaf — dispatchable)
```

- **Non-leaf specs** describe *what* and *why*: problem, motivation, design decisions, cross-cutting concerns. They point to child specs via a children list.
- **Leaf specs** describe *how*: which files to change, acceptance criteria, dependencies on sibling leaves. They are granular enough for a single agent task (2-5 files, one clear goal).
- **Depth is arbitrary.** A small feature might be a single leaf spec. A large initiative might be many levels deep. The user decides when a spec is small enough to execute directly — if it's not, break it down further. There is no limit on nesting depth.

### The Workflow

```
1. Human proposes an idea (natural language, as vague as they want)
2. Agent drafts a spec: structures the idea, identifies sub-problems
3. Human reviews, iterates via chat ("this section is wrong", "add X")
4. When a spec is too big → break down into sub-specs (agent proposes, human reviews)
5. When a spec is small enough → dispatch as a kanban task
6. Task executes, results feed back into the spec (implementation notes)
7. Repeat until the tree is fully executed
```

The human's role is **idea provider and steering** — they describe what they want, review what the agent proposes, and decide when to break down vs execute. They never need to write specs from scratch.

### Dispatch and Execution

A leaf spec is dispatched to the kanban board as a regular task. The spec's content becomes the task prompt. Dependencies between leaf specs (declared in frontmatter) become `DependsOn` on the kanban board. The auto-promoter runs them in dependency order.

When a dispatched task completes, the leaf spec is updated:
- Status moves to `done`
- The `dispatched_task_id` links to the completed kanban task
- Implementation notes are added if the result diverged from the plan

Non-leaf specs track progress by recursively aggregating all leaves in their subtree: "4/6 leaves done." A non-leaf child contributes its own subtree's leaf counts, not a single count. When all leaves in a subtree are done, the root of that subtree can be marked complete (after reviewing whether the implementation matches the design).

### Cross-Task Context

The spec DAG provides relationship information for richer board context. The board context generator uses two signals:
- **Dependency edges** (`depends_on`): direct dependencies get full context (prompt + result + diff summary), regardless of where they sit in the filesystem tree
- **Filesystem proximity** (same parent directory): sibling leaf specs get higher truncation limits in board.json
- **Unrelated tasks** keep current limits

This replaces the tiered truncation policy from the previous design without needing epic tags — the dependency DAG and filesystem tree provide the relationship information.

---

## What This Eliminates

| Previous concept | Replaced by |
|---|---|
| Epic tags (`epic:<slug>`) | Filesystem tree (directory nesting) |
| Phase tags (`phase:<N>`) | Dependency DAG (`depends_on` edges) |
| Planner task kind | Agent-assisted spec decomposition via chat |
| Gate task kind | Verification as a leaf spec (user decides if/when to add one) |
| Epic filter bar | Spec explorer with tree navigation |
| Epic progress panel | Spec tree progress view (aggregate children) |

The kanban board stays flat — it shows dispatched leaf specs as tasks. All structure lives in the spec tree, visible in the spec explorer.

---

## Child Specs

| Spec | Focus |
|------|-------|
| [spec-document-model.md](spec-document-model.md) | Spec properties, lifecycle, tree structure, leaf vs non-leaf semantics |
| [spec-drift-detection.md](spec-drift-detection.md) | Drift detection and propagation through the spec tree |
| [spec-planning-ux.md](spec-planning-ux.md) | Planning UX: spec explorer, chat-driven iteration, dispatch workflow |

---

## Implementation Order

Each step is independently shippable:

```
Step 1: Spec Document Model
  → Define frontmatter schema, lifecycle states, tree relationships
  → Specs become structured, machine-readable documents

Step 2: Spec Explorer + Chat Iteration
  → Browse specs in a tree view, open in focused markdown view
  → Iterate via chat: agent proposes changes, user reviews
  → Builds on the existing file explorer infrastructure

Step 3: Dispatch Workflow
  → Dispatch leaf specs as kanban tasks
  → Link dispatched tasks back to their spec
  → Progress tracking via tree aggregation

Step 4: Cross-Task Context
  → Use spec tree relationships for richer board.json context
  → Sibling leaves get more context than unrelated tasks

Step 5: Drift Detection
  → Detect when implementation diverges from spec
  → Propagate staleness through the tree
```

Step 1 is foundational. Steps 2-3 deliver the core planning workflow. Steps 4-5 are refinements.

---

## Interaction with Existing Features

| Feature | Impact |
|---|---|
| Auto-promoter | No change — dispatched leaf specs become regular tasks with `DependsOn` |
| Auto-retry | Per-task as today |
| Batch creation | Dispatching multiple leaf specs at once uses the same batch logic |
| Ideation | Independent — ideation creates standalone tasks, not specs |
| Oversight | Per-task as today |
| Refinement | Per-task — or per-spec via the chat iteration workflow |

---

## Relationship to Other Specs

### Dependency on File Explorer

The spec coordination UX depends on the file explorer (complete) for browsing and editing spec files. The spec explorer is a specialized view on top of the file explorer infrastructure.

**Implementation order:** File explorer (done) → Spec document model → Spec explorer + chat → Dispatch workflow.
