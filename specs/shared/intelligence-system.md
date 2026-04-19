---
title: Intelligence System
status: vague
depends_on:
  - specs/shared/agent-abstraction.md
  - specs/shared/telemetry-observability.md
  - specs/oversight/multi-agent-consensus.md
  - specs/shared/agent-memory-identity.md
affects:
  - internal/runner/
  - internal/store/
  - internal/handler/
effort: xlarge
created: 2026-04-01
updated: 2026-04-01
author: changkun
dispatched_task_id: null
---

# Intelligence System

Design space exploration for evolving wallfacer from a task board into an intelligence system. Inspired by Block's organizational model ("From Hierarchy to Intelligence"), which replaces hierarchical coordination with shared world models, proactive composition, and human judgment only at system edges.

The core question: what would wallfacer look like if the user stopped being a project manager shuffling cards and became a DRI setting goals?

---

## Design Space

### 1. Project World Model

A continuously-updated representation of project state that all running task agents share. Today each task operates in near-isolation — the board manifest (`/api/debug/board`) is the closest thing to shared context, but it's shallow.

**What the world model could contain:**
- Git status, recent diffs, branch topology across all workspaces
- Test results and lint output (last known state per workspace)
- Spec progress and dependency graph (already computed by `internal/spec/`)
- Which files are currently being modified by which tasks (live lock map)
- Interface signatures that have changed since a task started

**Key tension:** Richer context means higher token cost per task. The world model must be queryable (agents pull what they need) rather than broadcast (every agent gets everything).

**Open questions:**
- How much context is worth the token cost? Need empirical data on task success rate vs context richness.
- Should the world model be a structured API or a natural-language summary agents can read?
- How frequently does it need updating? Per-commit? Per-turn? On-demand?

---

### 2. Cross-Task Awareness and Conflict Prediction

When multiple tasks run concurrently, they can step on each other. Today this surfaces as merge conflicts after the fact. The intelligence system could predict and prevent conflicts.

**Possible approaches:**
- **File-level lock map:** Track which files each running task has modified. Warn or pause when two tasks touch the same file.
- **Interface change propagation:** When task A modifies a function signature, notify task B if it imports that package.
- **Proactive rebase:** Automatically sync task worktrees when upstream changes affect their working set.

**Key tension:** Strict conflict prevention (locks) reduces parallelism. Loose prediction (warnings) may come too late. Finding the right granularity matters.

---

### 3. Proactive Task Composition (Auto-Decomposition)

Today the user manually creates tasks. An intelligence layer could compose tasks automatically from higher-level goals.

**What this could look like:**
- Given a validated spec, automatically generate leaf tasks with dependency wiring (the batch task API already supports this).
- When a task fails because a prerequisite doesn't exist (e.g., missing utility function), auto-create the prerequisite as a new task and wire the dependency.
- When tests break after a task completes, auto-create a fix task.
- "Customer reality generates the backlog" — failed compositions become development priorities.

**Key tension:** Auto-generated tasks may not match the user's intent or priorities. The system needs a way to propose tasks for approval rather than creating them silently. Over-decomposition creates noise.

---

### 4. Goal-Oriented Task Groups

Instead of flat task lists, allow defining goals that span multiple tasks. A goal has a description, success criteria, and a time horizon (Block's DRI model: own a problem for N days).

**What this adds beyond workspaces:**
- Progress tracking toward the goal, not just per-task completion
- Auto-escalation when a goal stalls (tasks keep failing, budget exhausted)
- Goal-level cost and token budgets (distributed across child tasks)
- Retrospectives: when a goal completes, summarize what worked and what didn't

**Relationship to specs:** Goals might be the runtime counterpart of specs. A validated spec becomes a goal when dispatched. The spec tree provides the decomposition; the goal tracks execution.

---

### 5. Smarter Human-in-the-Loop (Edge Intelligence)

Block positions humans at the "edge" — handling what AI can't. Today wallfacer's waiting state is binary. The intelligence system could be more nuanced about when and why it needs human input.

**Possible classifications for waiting tasks:**
- **Architectural decision** — design choice with long-term implications
- **Ambiguous requirement** — spec is unclear, multiple valid interpretations
- **Security/ethics concern** — change touches auth, permissions, or user data
- **Confidence below threshold** — agent isn't sure the change is correct
- **External dependency** — needs information from outside the codebase

**What this enables:**
- Triage by urgency: architectural decisions block more downstream work than ambiguous requirements
- Auto-resolution for low-risk waits (e.g., "I wasn't sure which name to use" → pick the one matching existing conventions)
- Confidence scoring: high-confidence changes auto-proceed; low-confidence ones pause

---

### 6. Capability Registry (Reusable Task Outputs)

When a task produces a new utility, API endpoint, or abstraction, it's invisible to future tasks unless they happen to grep for it.

**What a capability registry could track:**
- New packages and exported symbols created by completed tasks
- API routes added or modified
- Reusable patterns (e.g., "there's now a generic DAG package at internal/pkg/dag/")

**How it feeds the world model:** Future tasks query the registry before implementing something new. "Does a pagination helper exist?" becomes answerable without grepping the entire codebase.

**Key tension:** Maintaining a registry is overhead. Could this be derived automatically from git diffs + static analysis rather than explicitly maintained?

---

### 7. Shared Context Bus (Replacing Coordination Overhead)

Block's core thesis: hierarchy exists to route information; replace the routing with shared models. In wallfacer terms: today the user is the information router between tasks.

**What a context bus could provide:**
- Tasks publish discoveries: "the auth middleware expects JWT in header X"
- Other tasks subscribe to relevant topics
- The system aggregates insights into the world model

**Implementation spectrum:**
- **Minimal:** Structured task output metadata (JSON alongside the git diff) that other tasks can query
- **Medium:** Event stream where tasks publish and subscribe to topic channels
- **Maximal:** Natural-language knowledge base that agents read and write, with semantic search

---

### 8. Learning from Failure Patterns

Today each task's failure is isolated. An intelligence system would aggregate patterns.

**What to aggregate:**
- Failure categories that cluster (e.g., container crashes around a specific package)
- Common retry patterns that succeed vs those that don't
- Token/cost patterns correlated with task complexity
- Oversight findings that repeat across tasks

**What to do with it:**
- Refine AGENTS.md automatically based on what worked
- Adjust task templates based on historical success rates
- Surface systemic issues (e.g., "tasks touching internal/runner/ fail 40% of the time — consider breaking it up")

---

## Ordering and Dependencies

These ideas are roughly ordered by feasibility and dependency:

1. **Project world model** (foundation — everything else builds on shared context)
2. **Cross-task awareness** (requires world model)
3. **Capability registry** (requires world model; can be derived from task outputs)
4. **Smarter human-in-the-loop** (independent; can ship incrementally)
5. **Goal-oriented task groups** (independent; extends existing workspace model)
6. **Learning from failure** (requires telemetry; builds on existing oversight)
7. **Shared context bus** (requires world model + agent abstraction)
8. **Proactive task composition** (requires all of the above to be useful; highest risk)

---

## Open Questions

- **Token economics:** How much shared context can agents consume before the cost outweighs the coordination benefit? Need benchmarks.
- **Autonomy spectrum:** Where on the spectrum from "propose everything for approval" to "act autonomously" should each capability default? Likely user-configurable.
- **Incremental path:** Which of these ideas deliver value standalone vs only as part of the full system? Prioritize standalone wins.
- **Evaluation:** How do we measure whether the intelligence system is actually better than manual task management? Task success rate? Time to goal completion? User satisfaction?
- **Scope creep risk:** This is an ambitious vision. Which subset is the minimum viable intelligence system?
