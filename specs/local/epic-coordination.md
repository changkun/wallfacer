# Epic Coordination Layer

**Date:** 2026-03-23

## Problem

Wallfacer's flat kanban board (backlog → in_progress → waiting → done) works well for independent tasks but cannot coordinate large, multi-milestone engineering work. A single "implement M1" task is too large for one agent turn, but splitting into many small tasks loses coherence — each agent starts fresh with no knowledge of what previous agents decided.

Concrete gaps:

1. **No spec-to-task decomposition.** Humans must manually break specs into tasks, wire dependencies, and size them correctly.
2. **No cross-task context.** Board.json truncates sibling prompts to 500 chars and results to 1000 chars. Task B doesn't know what task A decided about interface shapes, naming, or architecture.
3. **No milestone gates.** Nothing verifies that a phase is coherent before the next phase begins. A broken interface extraction in M1 propagates silently into M3.
4. **No epic progress tracking.** No way to see "M1: 3/7 done, 1 in progress, 3 backlog" at a glance.

## Design Principle

**Epic is a tag convention, not a first-class entity.** The tag `epic:<slug>` groups tasks. The tag `phase:<N>` sub-groups them. All epic behavior emerges from existing primitives: tags, dependencies, batch creation, board.json, auto-promoter. This avoids duplicating the task lifecycle with a parallel persistence model.

---

## Current Architecture

### What agents see today

| Context source | Content | Limitation |
|---|---|---|
| `board.json` | Sibling tasks: prompt (500 chars), result (1000 chars), status | Truncated — insufficient for coordinated work |
| Sibling worktrees | Read-only mounts of waiting/done/failed tasks' code | Code is visible but decisions/rationale are not |
| `AGENTS.md` | Workspace-level instructions | Same for all tasks, no task-specific context |
| Session resumption | `--resume <sessionID>` continues multi-turn within one task | No cross-task session sharing |

### What coordinates tasks today

| Mechanism | How | Limitation |
|---|---|---|
| `DependsOn` | Blocks auto-promotion until all deps are `done` | Only ordering — no context propagation |
| `CriticalPathScore` | Prioritizes tasks with longest downstream chain | Scheduling only — no decomposition guidance |
| Batch creation | Creates multiple tasks with dependency wiring + cycle detection | Manual — user must define the batch |
| Ideation (`TaskKindIdeaAgent`) | Brainstorm agent creates independent backlog tasks | No dependencies between ideated tasks |

---

## Design

### P1: Planner Task Kind — Spec-to-Task Decomposition

A new `TaskKind = "planner"` follows the established `TaskKindIdeaAgent` pattern: a task that runs a sandbox agent and creates backlog tasks from the output.

**Flow:**
```
User creates planner task (prompt = spec content or file path)
  → Runner dispatches to runPlannerTask()
  → Sandbox agent reads spec, explores codebase, reads board.json
  → Agent outputs structured JSON: phases, tasks, dependencies, gates
  → Runner parses output, calls batch creation logic
  → Backlog tasks created with epic:<slug>, phase:<N> tags + DependsOn wiring
  → Planner task moves to done
```

**Planner output schema:**
```json
{
  "epic_slug": "sandbox-backends",
  "phases": [
    {
      "phase": 1,
      "name": "Interface extraction",
      "tasks": [
        {
          "ref": "define-interface",
          "title": "Define SandboxBackend and SandboxHandle interfaces",
          "prompt": "... (full implementation prompt with acceptance criteria) ...",
          "depends_on_refs": []
        },
        {
          "ref": "local-backend",
          "title": "Implement LocalBackend wrapping os/exec",
          "prompt": "...",
          "depends_on_refs": ["define-interface"]
        }
      ],
      "gate": true
    },
    {
      "phase": 2,
      "name": "Runner migration",
      "tasks": [...]
    }
  ]
}
```

The runner translates this into a batch creation call:
- Each task gets tags: `["epic:sandbox-backends", "phase:1"]`
- `depends_on_refs` resolved to UUIDs via the batch ref system
- When `gate: true`, a gate task is inserted between phases (see P3)

**Planner prompt template** (`prompts/planner.tmpl`) encodes task sizing heuristics:
- Target 5-8 tasks per phase, each touching 2-5 files
- Each task must have a clear acceptance criterion
- Each task should be independently verifiable (tests pass after just this task)
- Include examples of good vs bad granularity

**Implementation:**

| File | Change |
|---|---|
| `internal/store/models.go` | Add `TaskKindPlanner TaskKind = "planner"` |
| `internal/runner/planner.go` (new) | `runPlannerTask()`, `parsePlannerOutput()`, `createPlannerBatchTasks()` |
| `prompts/planner.tmpl` (new) | System prompt for decomposition agent |
| `internal/runner/execute.go` | Add planner dispatch alongside ideation dispatch (~line 191) |
| `internal/handler/tasks.go` | Extract `BatchCreateTasks` logic into shared function callable from runner |

### P2: Cross-Task Context — Dependency-Aware Board.json

The truncation limits in `board.json` should be tiered based on dependency proximity. Tasks that directly depend on a completed task need full context from that dependency. Same-epic siblings need moderate context. Unrelated tasks keep current limits.

**Tiered truncation policy:**

| Relationship to self | Prompt limit | Result limit | Extra fields |
|---|---|---|---|
| Direct dependency (in self's `DependsOn`) | Full | Full | `diff_summary` |
| Same epic (shared `epic:*` tag) | 2000 chars | 2000 chars | — |
| Other siblings | 500 chars | 1000 chars | — (current behavior) |

**New fields on `BoardTask`:**

```go
type BoardTask struct {
    // ... existing fields ...
    Tags         []string `json:"tags,omitempty"`
    DependsOn    []string `json:"depends_on,omitempty"`
    DiffSummary  *string  `json:"diff_summary,omitempty"` // only for direct deps with done status
    IsDependency bool     `json:"is_dependency,omitempty"`
}
```

**Diff summary generation:** When a task transitions to `done`, generate a ~500-char summary of what changed (file list + key changes). Store in `data/<uuid>/diff_summary.txt`. The board context generator reads this for dependency tasks.

**Scaling:** 7 direct dependencies with full results (~2-4KB each) = ~28KB. Same-epic siblings at 2KB each for a 15-task epic = ~30KB. Total well within the 64KB warning threshold. For very large epics, the tiered approach keeps it bounded.

**Implementation:**

| File | Change |
|---|---|
| `internal/runner/board.go` | Tiered truncation in `generateBoardContextAndMounts`; add `Tags`, `DependsOn`, `DiffSummary`, `IsDependency` to `BoardTask`; look up self task's `DependsOn` and tags to classify relationships |
| `internal/runner/commit.go` | Generate diff summary at task completion |
| `internal/store/` | Add `SaveDiffSummary(id, summary)` / `GetDiffSummary(id)` |

### P3: Gate Tasks — Milestone Verification

A new `TaskKind = "gate"`. Created by the planner between phases. A gate task is a regular task that:

- Has `DependsOn` listing all tasks in the preceding phase
- Has all next-phase tasks depending on it
- Runs test suite + lint + vet via `prompts/gate.tmpl`
- Always mounts sibling worktrees (all phase tasks are done, so worktrees are available)
- Uses the test sandbox routing
- Has a shorter default timeout (15 minutes)

**Why explicit tasks, not implicit system behavior:**
- Visible on the board as a card — the user sees "M1 Gate: verify"
- Can be cancelled (skip verification), retried, or edited
- Produces results and oversight like any other task
- Uses the existing dependency mechanism — zero new auto-promotion logic
- Created by the planner agent as part of decomposition — no special system code

**Implementation:**

| File | Change |
|---|---|
| `internal/store/models.go` | Add `TaskKindGate TaskKind = "gate"` |
| `prompts/gate.tmpl` (new) | System prompt: run full test suite, lint, vet, report pass/fail |
| `internal/runner/execute.go` | Gate dispatch: force `MountWorktrees = true`, use test sandbox, shorter timeout |

### P4: Task Sizing — Prompt Engineering

Handled entirely by `prompts/planner.tmpl`. No code changes beyond the template.

The template instructs the decomposition agent:

```
## Task Sizing Guidelines

Each task should:
- Touch 2-5 files (if it touches 10+ files, split it)
- Have a single clear acceptance criterion
- Be independently verifiable (tests should pass after just this task)
- NOT be a micro-task ("rename variable X") — fold those into the task that uses the new name

Target 5-8 tasks per milestone phase. If a phase has 12+ tasks, split into sub-phases.

Bad: "Implement the entire storage layer" (too large)
Bad: "Add import statement for package X" (too small)
Good: "Define StorageBackend interface and implement FilesystemBackend" (3-4 files)
Good: "Migrate Store.CreateTask to delegate persistence to StorageBackend" (4-5 files)
```

### P5: Epic Progress Tracking

**API endpoint:** `GET /api/epics`

Scans all non-archived tasks, groups by `epic:*` tag, sub-groups by `phase:*` tag, counts statuses.

```json
[
  {
    "slug": "sandbox-backends",
    "phases": [
      {
        "phase": 1,
        "name": "Interface extraction",
        "total": 7, "done": 3, "in_progress": 1, "backlog": 3,
        "gate_status": "backlog",
        "cost_usd": 1.23
      }
    ],
    "total_tasks": 14,
    "completed_tasks": 3,
    "total_cost_usd": 1.23
  }
]
```

**Board manifest enhancement:** Add `EpicProgress` section so executing agents know where their epic stands:

```go
type BoardManifest struct {
    GeneratedAt  time.Time      `json:"generated_at"`
    SelfTaskID   string         `json:"self_task_id"`
    Tasks        []BoardTask    `json:"tasks"`
    EpicProgress []EpicProgress `json:"epic_progress,omitempty"`
}

type EpicProgress struct {
    Slug         string         `json:"slug"`
    CurrentPhase int            `json:"current_phase"`
    TotalPhases  int            `json:"total_phases"`
    PhaseDone    map[int]int    `json:"phase_done"`    // phase → done count
    PhaseTotal   map[int]int    `json:"phase_total"`   // phase → total count
}
```

This gives the agent a compact overview ("I'm in phase 2 of 4, phase 1 is fully complete") without listing every task.

**UI:** Epic progress view with per-phase progress bars, clickable to filter board by epic tag.

**Implementation:**

| File | Change |
|---|---|
| `internal/handler/epics.go` (new) | `GET /api/epics` handler |
| `internal/apicontract/routes.go` | Register route |
| `internal/runner/board.go` | Add `EpicProgress` to `BoardManifest` |
| `ui/js/` | Epic progress view component |

---

## Spec Document Model

Specs are the primary planning artifacts in Wallfacer. This section formalizes their properties, lifecycle, layering, and cross-spec consistency guarantees.

### Spec Properties

Every spec document (design spec or implementation task spec) carries structured frontmatter. Derived from real usage patterns in `specs/local/desktop-app/` and `specs/foundations/file-explorer/`:

**Design spec properties:**

```yaml
---
title: Sandbox Backends
status: validated          # vague | drafted | validated | implementing | complete | stale
track: foundations         # foundations | local | cloud | shared
depends_on:                # list of spec slugs this spec requires
  - storage-backends
blocks:                    # specs that cannot proceed until this is complete
  - container-reuse
  - k8s-sandbox
effort: large              # small | medium | large | xlarge
created: 2026-01-15
updated: 2026-03-28
author: changkun
implementation_spec_dir: specs/foundations/sandbox-backends/  # path to task specs (if any)
---
```

**Implementation task spec properties:**

```yaml
---
title: "Define SandboxBackend interface"
status: done               # draft | ready | dispatched | done | stale
parent_spec: specs/foundations/sandbox-backends.md
phase: 1
depends_on:                # task spec slugs within the same parent
  - []
effort: small              # small | medium | large
created: 2026-02-10
updated: 2026-03-01
dispatched_task_id: null   # UUID of the kanban task, set when dispatched
---
```

**Property semantics:**

| Property | Design spec | Task spec | Purpose |
|----------|-------------|-----------|---------|
| `status` | Lifecycle state (see below) | Task-level state | Tracks progress at appropriate granularity |
| `depends_on` | Other design specs by slug | Sibling task specs | Ordering and blocking |
| `blocks` | Downstream specs | — | Impact propagation (drift detection) |
| `effort` | T-shirt size for the whole spec | T-shirt size for one task | Planning and capacity estimation |
| `created` / `updated` | When written / last modified | When written / last modified | Staleness detection |
| `parent_spec` | — | Link to design spec | Traceability from task to motivation |
| `dispatched_task_id` | — | UUID link to kanban board | Connects spec world to execution world |

### Spec Lifecycle

A spec's state has **two independent dimensions**: design maturity (how well-understood the design is) and implementation progress (how much has been built). These are orthogonal — a spec can be `validated` in design while its implementation is `not_started`, or `implementing` while the design has gone `stale` due to upstream changes.

#### Dimension 1: Design Maturity

```
                  ┌──────────┐
                  │          │
                  ▼          │
vague ──▶ drafted ──▶ validated ──▶ complete
            │          │    ▲          │
            │          │    │          │
            ▼          ▼    │          ▼
          stale      stale  └───── stale
```

| State | Meaning | Transitions |
|-------|---------|-------------|
| **vague** | Initial idea. Problem statement exists but design is incomplete or hand-wavy. No implementation tasks defined. | → `drafted` (design details added) |
| **drafted** | Design is written with enough detail for review: motivation, high-level approach, key decisions, UX sketches. May still have open questions. | → `validated` (reviewed and approved for implementation) · → `stale` (superseded or abandoned) |
| **validated** | Design is reviewed, approved, and ready for implementation task decomposition. Open questions resolved. Cross-spec impacts assessed. | → `complete` (all tasks done, design matches reality) · → `stale` (invalidated by external change or implementation delta) |
| **complete** | All implementation tasks are done. The spec has been updated to describe what was *actually* built, not just what was *planned*. | → `stale` (if a later spec modifies the interfaces this spec established) |
| **stale** | The spec's design no longer matches reality. Either the codebase has diverged, a dependency changed, or implementation revealed a significant delta from the design. Requires human review before being trusted. | → `drafted` (refreshed with current state) · → `validated` (re-validated after review) |

#### Dimension 2: Implementation State

| State | Meaning |
|-------|---------|
| **not_started** | No task specs dispatched. Design may still be in progress. |
| **in_progress** | At least one task spec dispatched to the kanban board. Execution underway. |
| **done** | All task specs completed and verified (gate passed). |

These two dimensions combine in a matrix:

```
                    not_started     in_progress      done
                 ┌───────────────┬────────────────┬────────────┐
  vague          │ typical start │ —              │ —          │
  drafted        │ design phase  │ premature      │ —          │
  validated      │ ready to go   │ normal exec    │ wrapping up│
  complete       │ —             │ —              │ finished   │
  stale          │ needs review  │ ⚠ exec on      │ ⚠ done but │
                 │               │   stale design │   drifted  │
                 └───────────────┴────────────────┴────────────┘
```

The dangerous cell is `stale × in_progress`: tasks are executing against a design that no longer matches reality. The system should surface this prominently (see Drift Detection).

#### Stale: the critical state

`stale` is the most important state in the model because it's the one that catches real problems:

- A spec that was `validated` but then an upstream dependency changed its interfaces → `stale`
- A spec that was `complete` but a later spec modified the code it describes → `stale`
- A spec where implementation diverged significantly from the design → `stale`

**Stale granularity** is an open design question:

| Granularity | Pros | Cons |
|-------------|------|------|
| **Whole spec** | Simple to implement and reason about. One badge, one action needed. | Information loss — the user doesn't know *which part* is stale. May trigger unnecessary re-review of sections that are fine. |
| **Per-section** | Precise — "the interface section is stale but the UX section is fine." Lets the user focus review effort. | Requires parsing spec structure, maintaining section-level checksums or mappings. Higher maintenance cost. |
| **Per-assertion** | Most precise — "the assumption that `Launch()` returns synchronously is stale." | Impractical to automate. Requires semantic understanding of natural language claims. |

**Recommended approach:** Start with whole-spec staleness (mark the entire document). Add per-section granularity later if the number of specs grows large enough that whole-spec review becomes burdensome. Per-assertion granularity is deferred to model capability improvements.

#### Lifecycle rules

- A spec should not enter `in_progress` implementation without its design being `validated`. This prevents executing against a half-baked design. The system enforces this by blocking task dispatch from specs that are `vague` or `drafted`.
- When a spec's implementation reaches `done`, the design dimension should transition to `complete` — but only after the spec document is updated to reflect what was actually built (not just what was planned). If there's a significant delta, the spec goes to `stale` first.
- `stale` is not a dead end — it signals "this document needs human attention before being trusted."
- A `stale × in_progress` combination triggers a prominent warning in both the spec explorer and the epic progress panel.

### Operation Regimes

Not all specs need the same level of human involvement. The system supports two operation regimes, determined by the **certainty level** of the design — not by who physically writes the text.

| Regime | Certainty | Human role | Agent role | Transition signal |
|--------|-----------|------------|------------|-------------------|
| **Human-driven** | Low. The idea is vague, the approach is uncertain, there are open design questions. | Idea provider + steering. Gives fuzzy directives ("I want something like X"), reviews agent output, clarifies ambiguity, makes judgment calls. | Expander + structurer. Takes vague input, proposes structure, drafts sections, asks clarifying questions. | Design maturity reaches `validated` |
| **Agent-driven** | High. The design is clear, acceptance criteria are defined, interfaces are specified. | Reviewer. Monitors execution, provides feedback on waiting tasks, intervenes when drift is detected. | Executor. Decomposes validated spec into tasks, executes them, reports results. | Manual override, or design goes `stale` (drops back to human-driven) |

**Key insight:** Even in the human-driven regime, the human is not writing specs line-by-line. The human provides ideas and direction; the agent drafts, structures, and expands. The difference is the *frequency of human steering* — in human-driven mode, the agent proposes and the human accepts/rejects/redirects at every step. In agent-driven mode, the human approves once and monitors.

**Regime transitions:**

```
Human-driven ──▶ Agent-driven
  when: spec reaches "validated" and human explicitly approves for execution
  signal: human clicks "Approve for implementation" or dispatches first task

Agent-driven ──▶ Human-driven
  when: drift detected, or execution reveals the design was wrong
  signal: spec goes stale, or human manually pauses the epic
```

The regime is not a hard system mode — it's an emergent property of how the human and agent interact. But the system can infer it from spec maturity state and surface appropriate UX: in human-driven mode, the chat stream is primary; in agent-driven mode, the board and progress panel are primary.

**Open question:** Whether a third intermediate regime is needed (e.g., "supervised execution" where the agent executes but pauses for human approval at each phase gate). Currently the per-phase approval mechanism in P3/Stage 3 effectively provides this, so it may not need a separate regime label.

### Two-Layer Document Model

Specs operate at two abstraction layers that serve different audiences and evolve at different rates:

| Layer | Document type | Focus | Content | Audience | Lifecycle |
|-------|--------------|-------|---------|----------|-----------|
| **Design** | `specs/<track>/<name>.md` | Strategy, motivation, architecture | Why we're building this, problem statement, design decisions, UX, cross-cutting concerns, risk areas | Human planner | vague → drafted → validated → complete |
| **Implementation** | `specs/<track>/<name>/task-NN-<slug>.md` | Concrete execution steps | Which files to change, functions to add, acceptance criteria, test plan, dependency wiring | Agent executor | draft → ready → dispatched → done |

**Relationship between layers:**

```
Design Spec                          Implementation Task Specs
┌─────────────────────────┐          ┌──────────────────────────────┐
│ sandbox-backends.md      │          │ sandbox-backends/             │
│                          │  breaks  │   task-01-define-interface.md │
│ ## Problem               │  down    │   task-02-local-backend.md   │
│ ## Design                │ ──────▶  │   task-03-refactor-runner.md │
│ ## UX                    │          │   task-04-move-listing.md    │
│ ## Cross-Cutting         │          │   task-05-retire-executor.md │
│                          │  feeds   │   task-06-gate-verify.md     │
│ (updated with impl notes)│ ◀──────  │   (completion notes)         │
└─────────────────────────┘          └──────────────────────────────┘
```

- **Break down** (design → implementation): The planner agent or human decomposes a design spec into task specs. Each task spec references its `parent_spec`. The design spec's `implementation_spec_dir` points to the task spec directory.
- **Feed back** (implementation → design): When task execution reveals unexpected constraints (e.g., an interface needs an extra method), the design spec is updated with implementation notes. This keeps the design spec as a living record of what was *actually* built, not just what was *planned*.

**Conventions for task spec files:**

Task specs live in a subdirectory named after their parent design spec. Files are named `task-NN-<slug>.md` where NN is a sequence number for reading order (not execution order — execution order is determined by `depends_on`). This matches the existing convention in `specs/local/desktop-app/` and `specs/foundations/file-explorer/`.

#### Spec Relationship Graph: Tree vs DAG

The parent-child relationship between design specs and their task specs forms a tree. But design specs relate to *each other* via `depends_on` and `blocks` edges, forming a **DAG** (directed acyclic graph).

```
                    Design Spec DAG
    ┌─────────────────┐        ┌─────────────────┐
    │ sandbox-backends │───────▶│ container-reuse  │
    └────────┬────────┘        └────────┬────────┘
             │ breaks down               │ breaks down
    ┌────────┴────────┐        ┌────────┴────────┐
    │ task-01-iface   │        │ task-01-workers  │
    │ task-02-local   │        │ task-02-pool     │
    │ task-03-runner  │        └─────────────────┘
    └─────────────────┘
         │
         │ cross-spec impact: task-03 changed
         │ board.go, which container-reuse assumes
         ▼
    container-reuse goes stale
```

**Why this matters for drift propagation:** In a strict tree, drift only flows parent ↔ child. In the DAG, drift propagates across `blocks` edges between design specs. When `sandbox-backends` completes and its implementation differs from the plan, `container-reuse` (which depends on it) needs to be checked — even though they're siblings, not parent-child.

**Bidirectional propagation within the tree:**

- **Downward** (design → tasks): Parent spec modified → child task specs may be invalidated. If the parent's interface section changes, task specs that reference those interfaces become stale.
- **Upward** (tasks → design): Task execution reveals unexpected constraints → parent spec should be updated. If three out of five tasks needed extra methods on the interface, the design spec's interface description is wrong and should be corrected.

**Forward propagation across the DAG:**

- **Through `blocks` edges**: Completed spec A's actual implementation differs from spec B's assumptions (where B depends on A) → B goes stale.

**Open question:** Can a task spec belong to multiple design specs? In practice this is rare (a task usually serves one purpose), but cross-cutting implementation work (e.g., "add metrics to all handlers") could span multiple epics. The current model doesn't support this — a task spec has one `parent_spec`. If needed, the workaround is to create a separate cross-cutting design spec that owns the cross-cutting tasks.

### Drift Detection and Propagation

When implementing a spec reveals divergence from the original design — or when a completed spec's interfaces are modified by a later spec — the system needs to detect and surface this.

#### What is drift?

Drift occurs when the actual state of the codebase no longer matches what a spec describes. Three categories:

| Drift type | Cause | Example |
|------------|-------|---------|
| **Implementation drift** | Task execution diverges from its task spec | Task spec says "add 3 methods to SandboxBackend", but the agent added 4 because it discovered a missing capability |
| **Design drift** | Completed implementation doesn't match the design spec | Design spec describes a two-method interface, but implementation needed five methods |
| **Cascade drift** | A downstream spec's assumptions are invalidated by an upstream spec's implementation | `container-reuse.md` assumes `SandboxBackend.Launch()` returns a handle synchronously, but the actual implementation returns a channel |

#### Detection mechanisms

**1. Post-task drift check (automatic)**

When a task reaches `done`, the system compares the task's diff against its task spec:

- Extract the file list from the task spec's "What to do" section
- Compare against the files actually modified (from `git diff`)
- Flag discrepancies: files modified that weren't mentioned, files mentioned that weren't touched, significantly more changes than expected

This produces a **drift report** stored alongside the task's oversight summary. The drift report is informational — it surfaces in the epic progress panel as a warning icon (⚠) but does not block execution.

```
Drift Report — task-03-refactor-runner
  Expected files: runner.go, execute.go, container.go (3 files)
  Actual files:   runner.go, execute.go, container.go, board.go, models.go (5 files)
  Unexpected:     board.go (board context generation changed), models.go (new field added)
  Assessment:     Moderate drift — 2 unexpected files touched
```

**2. Cross-spec impact analysis (on completion)**

When a design spec enters `complete`, scan its `blocks` list. For each downstream spec:

- Check if the downstream spec references interfaces, types, or behaviors defined by the completed spec
- Compare the completed spec's actual implementation (from task results and current codebase) against the downstream spec's assumptions
- If assumptions are invalidated, mark the downstream spec as potentially stale

This is a lightweight heuristic, not a proof — it surfaces candidates for human review rather than making automated decisions.

**3. Staleness detection (periodic)**

A background check (triggered on workspace load or manually via the spec explorer) scans all `complete` specs:

- For each spec, check if the files it describes have been modified since `updated`
- If significant changes are detected (via `git log --since`), flag the spec as a staleness candidate
- Surface in the spec explorer with a stale indicator badge

#### Propagation chain

When drift is detected, the system propagates awareness through the dependency graph:

```
sandbox-backends.md (complete, drift detected)
  │
  ├─ blocks: container-reuse.md (validated)
  │    → Mark as "upstream drift" — banner: "sandbox-backends changed,
  │      review assumptions before implementing"
  │
  └─ blocks: k8s-sandbox.md (drafted)
       → Mark as "upstream drift" — same banner
```

**Propagation rules:**

- Drift warnings propagate forward through `blocks` edges only (not backward through `depends_on`)
- A `complete` spec with drift triggers warnings on all specs that list it in `depends_on`
- Warnings are advisory — they appear in the spec explorer and epic progress panel but do not block task dispatch
- A human must explicitly acknowledge drift (by updating the affected spec or dismissing the warning) before the warning clears
- When a human updates a downstream spec to account for upstream drift, the warning clears and the downstream spec's `updated` timestamp advances

**UI for drift:**

In the spec explorer:
```
specs/
  ✅ sandbox-backends.md          ⚠ drift detected
  ✅ storage-backends.md
  ⏳ container-reuse.md           ⚠ upstream drift (sandbox-backends)
  📝 k8s-sandbox.md               ⚠ upstream drift (sandbox-backends)
  📝 epic-coordination.md
```

Status icons: ✅ complete, ⏳ implementing, ✔ validated, 📝 drafted, 💭 vague, ⚠ stale

In the epic progress panel, drift warnings appear as a section within the phase card:

```
── Phase 1: Interface Extraction ──    5/7 tasks done
⚠ Drift: task-03-refactor-runner modified 2 unexpected files (board.go, models.go)
⚠ Downstream impact: container-reuse.md may need review
```

#### Implementation

| File | Change |
|---|---|
| `internal/runner/drift.go` (new) | `CheckTaskDrift(taskID)` — compare task spec expectations vs actual diff; `CheckSpecStaleness(specPath)` — git log check |
| `internal/store/` | `SaveDriftReport(taskID, report)` / `GetDriftReport(taskID)` |
| `internal/handler/explorer.go` | Extend tree listing to parse spec frontmatter, surface status and drift badges |
| `prompts/drift.tmpl` (new) | System prompt for the drift analysis agent (optional: use an agent to assess semantic drift beyond file-list comparison) |

#### Codebase Index Strategy for Drift Detection

Drift detection requires understanding whether code changes semantically affect a spec's assumptions — not just whether files were modified. Three approaches, in order of infrastructure investment:

| Approach | Description | Pros | Cons |
|----------|-------------|------|------|
| **A: Full dump** | Dump file tree + file summaries into agent context. Agent does semantic comparison. | Simple to implement. No new infrastructure. | Doesn't scale. Most info is noise for any given spec. Context window waste. |
| **B: Two-layer index** | Layer 1: static structural index (file tree + module responsibility summaries), incrementally updated after each task. Layer 2: spec-to-code mapping (`affects: [internal/sandbox/, internal/runner/execute.go]` in spec frontmatter). Drift detection follows layer 2 edges. | Precise. Enables targeted checks. Spec-to-code mapping is useful beyond drift (impact analysis, spec navigation). | Requires building and maintaining index infrastructure. Layer 1 needs update logic. |
| **C: Model capability** | With 1M+ context windows, feed the agent the full spec + all relevant source files and let it assess drift semantically. No custom index. | Zero infrastructure. Leverages improving model capability. Quality improves automatically as models improve. | Expensive per invocation. May hit context limits on very large codebases. Relies on model quality for correctness. |

**Current recommendation:** Start with approach C for drift *assessment* (the agent reads spec + affected files and judges whether drift is meaningful). Add approach B's layer 2 (spec-to-code mapping via `affects` field in frontmatter) regardless — it's cheap to maintain and valuable for multiple purposes:

```yaml
---
title: Sandbox Backends
affects:                    # packages and files this spec describes
  - internal/sandbox/
  - internal/runner/execute.go
  - internal/runner/container.go
---
```

The `affects` field serves as the "edge" for targeted drift checks: when a task modifies files listed in another spec's `affects`, that spec is a candidate for staleness review. This is the minimal index that makes drift detection tractable without building full infrastructure.

**Bootstrap strategy:** At current scale (~20 specs, ~140K LOC), `affects` fields can be populated manually. As spec count grows, the planner agent can propose `affects` values during spec creation, and the system can validate them against actual task diffs.

**Deferred:** Layer 1 of approach B (structural index with module summaries). This becomes valuable when the codebase exceeds what a model can hold in context, but at current scale approach C is sufficient.

---

## Implementation Order

Each phase is independently shippable:

```
Phase A: Board Enhancement (P2 + P5 foundation)
  → Agents immediately get richer cross-task context

Phase B: Planner Task Kind (P1 + P4)
  → Specs can be automatically decomposed into tasks

Phase C: Gate Tasks (P3)
  → Milestones are verified before proceeding

Phase D: UI (P5 completion)
  → Visual epic progress tracking
```

Phase A delivers value without B/C/D — even manually-created tasks with dependency wiring benefit from tiered board.json context. Phase B is the biggest win (automated decomposition). Phase C is a refinement. Phase D is polish.

---

## When to Use Spec-Driven vs Task-Driven Mode

Spec-driven mode (epics with planner decomposition) and task-driven mode (manually creating individual tasks) serve different situations. The wrong choice wastes effort: over-structuring a small change adds ceremony, while under-structuring a large change leads to incoherent results.

### Codebase Size and Maturity Heuristics

| Scenario | Recommended mode | Why |
|---|---|---|
| **Very large codebase** (100k+ LOC, many packages) | Spec-driven | Agents cannot hold the full codebase in context. A spec decomposes work into scoped tasks that each touch a manageable slice. Cross-task context (P2) propagates interface decisions across boundaries. |
| **Near-empty / greenfield codebase** | Spec-driven | No existing code to orient agents. A spec provides the architectural skeleton — agents need explicit phase ordering and interface contracts to build coherently from scratch. |
| **Medium codebase with established patterns** (10k–100k LOC) | Task-driven (default) | Agents can infer conventions from existing code. Individual tasks with clear prompts are sufficient. Epic overhead adds coordination cost without proportional benefit. |
| **Small, well-structured codebase** (<10k LOC) | Task-driven | The whole codebase fits in agent context. Decomposition is unnecessary — a single task can implement a feature end-to-end. |

### Decision Metrics

The UI should surface these metrics (in the planner creation dialog or a "mode recommendation" tooltip) to help users choose:

| Metric | How to measure | Threshold guidance |
|---|---|---|
| **Files touched** | Estimate from spec scope or past similar changes | >15 files → spec-driven; <5 files → task-driven |
| **Cross-package dependencies** | Count distinct packages/directories affected | >3 packages → spec-driven |
| **Interface changes** | Does the change define or modify shared interfaces/contracts? | Any shared interface change → spec-driven (gates verify coherence) |
| **Sequential ordering required** | Must some work complete before other work can start? | >2 sequential phases → spec-driven (dependency wiring + gates) |
| **Estimated agent turns** | Rough count of independent implementation units | >5 turns → spec-driven; 1-3 turns → task-driven |
| **Existing test coverage** | Are there tests that verify integration between affected areas? | Low coverage in affected area → spec-driven (gates add verification) |

### Surfacing in the UI

When a user creates a new task, the dialog could show a lightweight recommendation based on workspace metrics:

- **Codebase size indicator**: LOC count and package count from the workspace (computed once, cached).
- **Spec complexity indicator**: If the prompt references a spec file, scan it for phase/milestone markers and estimate task count.
- **Suggestion banner**: A non-blocking hint like "This spec touches 6 packages across 3 phases — consider using a planner task for structured decomposition" or "This looks like a single-file change — a regular task should work well."

The recommendation is advisory only — users always choose. The goal is to reduce the "should I use epics for this?" decision fatigue.

---

## UX & UI Design

### Design Philosophy

The core challenge: epics introduce a second layer of structure (groups of tasks with phases and gates) into a board that is currently flat. The design must surface epic context without overwhelming the kanban simplicity that makes wallfacer usable.

**Principle: epics are a lens, not a mode.** The board always shows individual tasks. Epic context is overlaid — filtering, grouping, progress indicators — but the fundamental interaction unit remains the task card. Users who don't use epics see zero change.

### Board Integration

#### Epic Filter Bar

When any tasks have `epic:*` tags, a horizontal bar appears below the header tabs:

```
┌─header──────────────────────────────────────────────────┐
│ [workspace-group-tabs]     [search] [auto] [⌘] [stats]  │
├─epic-bar────────────────────────────────────────────────┤
│ All │ sandbox-backends (3/7) │ storage-backends (0/5) │  │
├─────┴───────────────────────────────────────────────────┤
│ Backlog      │ In Progress │ Waiting    │ Done          │
│              │             │            │               │
│ [card]       │ [card]      │ [card]     │ [card]        │
│ [card]       │             │            │ [card]        │
│ ...          │             │            │ ...           │
└─────────────────────────────────────────────────────────┘
```

- **"All"** (default) shows every task, no filtering. Epic bar is informational only.
- **Clicking an epic slug** filters the board to show only tasks with that `epic:*` tag. The parenthetical shows `(done/total)`.
- **Active filter** is visually distinguished (accent underline, bolder weight, `var(--accent)` text).
- **Bar auto-hides** when no epics exist (zero tasks with `epic:*` tags).
- Style: same height/rhythm as workspace group tabs (11px font, 4px 10px padding, 6px top radius). Uses `var(--bg-raised)` background, `var(--border)` bottom border.

#### Epic-Grouped Card View

When an epic filter is active, the board columns gain **phase group dividers**:

```
Backlog                    │ In Progress
                           │
── Phase 1: Interface ─────│── Phase 1: Interface ─────
 [define-interface]        │ [local-backend] ▶ running
 [retire-executor]  🔒     │
                           │
── Phase 1 Gate ───────────│
 [verify-m1] 🔒            │
                           │
── Phase 2: Migration ─────│
 [migrate-runner] 🔒       │
 [migrate-listing] 🔒      │
```

- **Phase dividers**: thin horizontal rule with phase name, 10px uppercase text, `var(--text-muted)`, letter-spacing 0.05em. Matches the existing column header style.
- **Lock icon (🔒)**: shown on cards whose dependencies are not yet met. Subtle `var(--text-muted)` opacity. Tooltip on hover: "Blocked by: define-interface, local-backend".
- **Phase dividers are only visible when an epic filter is active.** In "All" view, cards appear in their normal board position without phase grouping.

#### Task Card Enhancements

Task cards gain two small additions when they belong to an epic:

1. **Epic pill badge**: small tag-style badge showing `epic:sandbox-backends` using the existing tag color cycling system. Appears in the tag row alongside other tags.

2. **Dependency indicator**: if the task has `DependsOn` entries, show a small chain-link icon with count (e.g., `🔗 2`). Clicking opens a tooltip listing dependency task titles and their statuses (done ✓, in-progress ◉, backlog ○).

Both additions use existing badge/tag styling. No new visual concepts.

#### Gate Task Card

Gate tasks look like regular task cards with visual distinction:

- **Badge**: `badge-gate` — uses the existing test/purple palette (`#ede8f7` bg, `#5a3fa0` text) with a shield or checkmark icon.
- **Card border**: left border accent in purple (2px solid, like the existing priority card pattern).
- **Title**: auto-generated "Phase 1 Gate: verify" — shown in the card title slot.
- **Status display**: when done, shows pass/fail prominently. Pass: green background tint. Fail: red background tint (same pattern as `badge-testing` pass/fail).

#### Planner Task Card

Planner tasks also look like regular task cards:

- **Badge**: `badge-planner` — uses the existing idea-agent/blue palette with a blueprint/plan icon.
- **When done**: the card shows a summary line: "Created 7 tasks in epic:sandbox-backends".
- **When in progress**: shows "Decomposing spec..." with spinner.

### Epic Progress Panel

Accessible via the epic filter bar or a dedicated button in the header action row. Opens as a **modal** (reusing `.modal-wide` pattern) showing all epics:

```
┌──────────────────────────────────────────────────────────┐
│ Epic Progress                                        [×] │
├──────────────────────────────────────────────────────────┤
│                                                          │
│  sandbox-backends                            $2.34 total │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Phase 1: Interface extraction          5/7 tasks    │ │
│  │ ████████████████████░░░░░░ 71%         $1.82        │ │
│  │ ◉ in-progress: local-backend                        │ │
│  │ ○ backlog: retire-executor, update-registry         │ │
│  │ Gate: backlog 🔒                                    │ │
│  ├─────────────────────────────────────────────────────┤ │
│  │ Phase 2: Migration                     0/4 tasks    │ │
│  │ ░░░░░░░░░░░░░░░░░░░░░░░░░░ 0%         —            │ │
│  │ Blocked by Phase 1 Gate                             │ │
│  └─────────────────────────────────────────────────────┘ │
│                                                          │
│  storage-backends                            $0.00 total │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Phase 1: Interface extraction          0/5 tasks    │ │
│  │ ░░░░░░░░░░░░░░░░░░░░░░░░░░ 0%         —            │ │
│  └─────────────────────────────────────────────────────┘ │
│                                                          │
└──────────────────────────────────────────────────────────┘
```

**Progress bar**: uses `var(--accent)` for filled portion, `var(--bg-raised)` for empty. 8px height, 4px border-radius (pill).

**Phase card**: uses `.settings-card` pattern — bordered card with subtle gradient background, 12px border-radius. Collapsible via `<details>` element (same as settings cards).

**Task status icons**: ✓ done (green), ◉ in-progress (blue), ○ backlog (gray), ✕ failed (red), ⏸ waiting (orange). Inline with task titles, 11px text.

**Cost column**: right-aligned, `var(--text-muted)`, 11px monospace. Accumulated from task summaries.

**Gate status**: shown as a special row within each phase card. Status badge matches gate task status. "Blocked by Phase N Gate" shown in muted text for subsequent phases.

**Clicking a task title** in the progress panel opens the task detail modal (existing behavior). **Clicking "Phase 1"** filters the board to `epic:sandbox-backends` + `phase:1`.

### Planner Creation UX

Two entry points for creating a planner task:

#### 1. From the board (new task dialog)

The existing "New Task" dialog gains a **Kind** selector (dropdown or toggle) when the user clicks the + button:

```
┌─────────────────────────────────────────────┐
│ New Task                                [×] │
├─────────────────────────────────────────────┤
│ Kind:  [Task ▾]  [Planner]  [Idea Agent]   │
│                                             │
│ Prompt:                                     │
│ ┌─────────────────────────────────────────┐ │
│ │ Plan implementation of                  │ │
│ │ specs/01-sandbox-backends.md Phase 1    │ │
│ └─────────────────────────────────────────┘ │
│                                             │
│                           [Create Planner]  │
└─────────────────────────────────────────────┘
```

When "Planner" is selected:
- The prompt field label changes to "Spec reference or content"
- A hint appears: "Reference a spec file path (e.g. `specs/01-sandbox-backends.md`) or paste spec content directly."
- The submit button says "Create Planner" instead of "Create Task"
- Timeout defaults to 30 minutes (planners are fast)

#### 2. From the epic progress panel

If no epics exist yet, the epic progress panel shows an empty state with a CTA:

```
┌──────────────────────────────────────────────────────┐
│ Epic Progress                                    [×] │
├──────────────────────────────────────────────────────┤
│                                                      │
│  No epics yet.                                       │
│                                                      │
│  Create a planner task to decompose a spec into      │
│  an epic with phased tasks, dependencies, and gates. │
│                                                      │
│  [+ Create Planner Task]                             │
│                                                      │
└──────────────────────────────────────────────────────┘
```

### Epic Oversight View

When viewing a task that belongs to an epic, the **task detail modal** gains an "Epic Context" section in the left panel:

```
┌──────────────────────┬──────────────────────────────────┐
│ Task Detail          │ Results                          │
│                      │                                  │
│ ── Epic Context ──   │ (existing result/log view)       │
│ epic:sandbox-backends│                                  │
│ Phase 1 of 2         │                                  │
│ 5/7 tasks done       │                                  │
│                      │                                  │
│ Dependencies:        │                                  │
│  ✓ define-interface  │                                  │
│  ✓ local-backend     │                                  │
│                      │                                  │
│ Dependents:          │                                  │
│  ○ retire-executor   │                                  │
│  ○ update-registry   │                                  │
│                      │                                  │
│ ── Specs & Goals ──  │                                  │
│ (existing prompt/    │                                  │
│  goal view)          │                                  │
└──────────────────────┴──────────────────────────────────┘
```

- **Epic Context section**: appears above the existing Specs & Goals section in the left panel. Only shown for tasks with `epic:*` tags.
- **Dependencies/Dependents**: clickable task titles that navigate to those tasks' detail modals.
- **Phase progress**: compact "5/7 tasks done" with mini progress bar (same style as the epic progress panel but inline).

### Gate Oversight

When a gate task completes, its **oversight summary** includes a structured verification report:

```
── Gate: Phase 1 Verification ──

Test Suite:     ✓ 142 passed, 0 failed, 0 skipped
Lint:           ✓ no issues
Vet:            ✓ no issues
Compilation:    ✓ clean build

Phase 1 tasks verified:
  ✓ define-interface — SandboxBackend/SandboxHandle defined
  ✓ local-backend — LocalBackend wraps os/exec with state tracking
  ✓ runner-migration — Runner uses backend.Launch() instead of executor.RunArgs()
  ...

Verdict: PASS — Phase 2 tasks are now unblocked.
```

This is generated by the gate agent (via `prompts/gate.tmpl`) and displayed in the standard oversight panel. The gate template instructs the agent to produce this structured format.

### Re-Planning UX

When a planner's decomposition needs adjustment:

1. **Cancel epic tasks**: The epic progress panel shows a **"Cancel All"** button (`.btn-danger`) per phase. Clicking cancels all backlog/waiting tasks in that phase (not in-progress or done tasks). Confirmation dialog: "Cancel 4 backlog tasks in Phase 2? Done tasks will be preserved."

2. **Re-plan**: After cancellation, the user creates a new planner task. The planner agent sees the completed tasks in board.json and skips already-done work, creating only the remaining tasks.

3. **Edit individual tasks**: Tasks created by the planner are regular tasks. The user can edit prompts, add/remove dependencies, change timeouts — all via the existing task detail modal.

### Notification & Status

The **status bar** (bottom of the page) gains epic awareness:

```
● Connected · repo-a, repo-b  │  2 in progress · 1 waiting  │  epic: sandbox-backends 5/7
```

When an epic filter is active, the status bar shows the filtered epic's progress (`5/7`). Uses `var(--text-muted)` for the label, `var(--text)` for the count. Clicking navigates to the epic progress panel.

### Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| `E` | Toggle epic progress panel |
| `1`–`9` (when epic bar visible) | Switch to epic filter by position |
| `0` or `Esc` | Clear epic filter (show all) |

These follow the existing shortcut pattern (single-key when no input is focused).

### Design Option: Spec-Centric Planning View

The board integration above (epic filter bar, phase dividers, progress panel) overlays epic awareness onto the existing kanban. This works for *execution monitoring*, but the harder problem is *planning itself* — the iterative process of refining specs and breaking them into tasks. An alternative design puts **spec documents** at the center of the UX rather than task cards.

#### Motivation

Observation from real usage: managing large changes requires constant iteration on spec markdown files. Specs need a filesystem structure to organize, a focused view to read, and a way to update them as execution reveals new information. The current design treats specs as opaque inputs to a planner task. A spec-centric design treats them as **living documents** that evolve alongside execution.

#### Layout: Split-Pane Spec Workspace

A dedicated planning mode with three panes — spec explorer, focused document view, and chat stream:

```
+--header------------------------------------------------------+
| [Board] [Specs]   workspace-group-tabs   [search] [settings] |
+----------+---------------------------+-----------------------+
|          |                           |                       |
| Spec     |  Focused Markdown View    |  Chat Stream          |
| Explorer |                           |                       |
|          |  # Sandbox Backends       |  > Break Phase 1 into |
| specs/   |                           |    tasks that each    |
|  01-sand |  ## Phase 1: Interface    |    touch 2-5 files    |
|  01a-nat |                           |                       |
|  01b-nat |  Define SandboxBackend    |  Agent: I'll split    |
|  01c-nat |  and SandboxHandle as     |  into 6 tasks...      |
|  01d-win*|  interfaces in a new      |                       |
|  02-stor |  `internal/sandbox/`      |  [updated spec with   |
|  02a-mul |  package. The interface   |   task breakdown       |
|  03-cont |  has three methods:       |   highlighted]         |
|  04-file |  - Launch(spec) Handle    |                       |
|  epic-co |  - ListContainers() []C   |  > Make tasks 4 and 5 |
|          |  - Stop(name) error       |    one task, they're   |
|          |                           |    too small           |
|          |  ## Tasks (auto-generated) |                       |
|          |  - [ ] Define interfaces  |  Agent: Combined into  |
|          |  - [ ] Implement Local    |  task "Migrate listing |
|          |  - [ ] Refactor Runner    |  and retire executor"  |
|          |  ...                       |                       |
|          |                           |  [Send to Board ▶]    |
+----------+---------------------------+-----------------------+
+--status-bar--------------------------------------------------+
```

**Left pane — Spec Explorer:** A file tree rooted at `specs/` (reuses file explorer infrastructure from M4). Shows spec files with status badges (complete, in-progress, not started). Clicking a spec opens it in the focused view.

**Center pane — Focused Markdown View:** Renders the selected spec as formatted markdown with live updates. When the chat agent modifies the spec file, the view refreshes automatically. Supports inline editing — the user can click to edit sections directly, or let the agent do it via the chat. Task checklists within the spec are interactive (click to mark done). Diff highlighting shows what the agent changed since the last user review.

**Right pane — Chat Stream:** A conversation interface for iterating on the focused spec. The user types directives ("break Phase 1 into tasks", "this section is too vague, expand it", "add acceptance criteria to each task"). The agent reads the spec, explores the codebase, and proposes changes — which appear as highlighted diffs in the center pane. The user can accept, reject, or refine.

#### Chat-Driven Spec Iteration Workflow

```
1. User opens specs/01-sandbox-backends.md in the focused view
2. User types in chat: "Plan implementation of Phase 1"
3. Agent reads the spec + codebase, proposes a task breakdown
   → Spec file updated with a "## Tasks" section containing the breakdown
   → Focused view shows the diff (new section highlighted)
4. User reviews, types: "Task 3 is too large, split the runner refactor"
   → Agent revises the spec's task section
   → Focused view updates, diff shows the change
5. User types: "Looks good. Send Phase 1 tasks to the board"
   → Agent extracts tasks from the spec and creates them as kanban cards
   → Board view shows new backlog cards with epic tags and dependencies
6. Tasks execute on the kanban board (existing auto-promoter)
7. As tasks complete, agent updates the spec:
   → Marks completed items, adds implementation notes
   → Focused view reflects changes in real-time
8. After Phase 1 gate passes, user returns to spec view:
   → "Now plan Phase 2, accounting for what Phase 1 actually built"
   → Agent reads completed task results + current codebase
   → Proposes Phase 2 tasks, updates spec
```

The key difference from the task-centric design: **the spec file is the source of truth** for the plan, not structured JSON in a planner task's output. The spec evolves continuously. Tasks are materialized *from* the spec when the user is ready, not produced as a one-shot batch.

#### Spec File Conventions

For the chat agent to read and update specs programmatically, specs follow a light convention for task sections:

```markdown
## Phase 1: Interface Extraction

### Tasks

- [ ] **Define SandboxBackend interface** — `internal/sandbox/backend.go` (new)
  Acceptance: interface compiles, doc comments on all methods
  Depends on: —

- [ ] **Implement LocalBackend** — `internal/sandbox/backend_local.go` (new), `executor.go`
  Acceptance: existing tests pass with LocalBackend wired in
  Depends on: Define SandboxBackend interface

- [x] **Refactor Runner** — `runner.go`, `execute.go`, `container.go`
  Acceptance: runContainer uses backend.Launch()
  Depends on: Implement LocalBackend
  Completed: 2026-03-28, cost $0.89
```

The agent parses this format to extract tasks for board creation and updates checkboxes + completion notes as tasks finish. The format is human-readable markdown — no JSON schemas to maintain.

#### Comparison: Task-Centric vs Spec-Centric

| Aspect | Task-Centric (current design) | Spec-Centric (this option) |
|--------|-------------------------------|---------------------------|
| Primary object | Planner task card | Spec markdown file |
| Plan lives in | Planner task output (JSON) | Spec file (markdown) |
| Iteration medium | Task feedback loop | Chat stream + live doc |
| Review location | Task detail modal | Dedicated split-pane view |
| Plan persistence | Task data directory | Git-tracked spec file |
| Spec updates during execution | Manual | Agent updates automatically |
| UI complexity | Overlay on kanban | New view mode alongside kanban |
| M4 dependency | None (file path in prompt) | Required (explorer + editing) |

**The two designs are complementary, not mutually exclusive.** The task-centric backend (P1-P5: planner task kind, board.json context, gate tasks, epic tags, progress tracking) is needed regardless. The spec-centric UI is an alternative *frontend* for the planning workflow that can be built on top of the same backend primitives.

**Recommended approach:** Implement the task-centric backend first (P1-P5). Then build the spec-centric UI as the planning frontend, which provides a better UX for the iterative planning loop while the kanban board remains the execution monitoring view.

#### Two-Layer Document Model: Specs vs Task Specs

See **[Spec Document Model → Two-Layer Document Model](#two-layer-document-model)** for the formal definition of the two layers, their properties, lifecycle states, and the conventions for task spec files.

In short: a design spec (`specs/<track>/<name>.md`) captures strategy and motivation; implementation task specs (`specs/<track>/<name>/task-NN-<slug>.md`) capture concrete execution steps. The design spec feeds downward via decomposition; task completion feeds back upward via implementation notes and drift detection.

**The UI should support decomposition as a first-class operation:**

1. **Break down** — Select a spec (or a section of a spec) in the focused view. The chat agent analyzes it against the codebase and proposes task specs: concrete implementation units with file lists, acceptance criteria, and estimated scope. Each task spec becomes a collapsible child node under the parent spec in the explorer.

2. **Identify dependencies** — The agent infers dependencies from the task specs' file lists and interface references. The focused view shows a dependency graph overlay: which tasks must complete before others can start, which can run in parallel. The user can adjust by dragging edges or clicking to add/remove dependencies.

3. **Dispatch** — Two dispatch granularities:
   - **Dispatch a single task spec** — Creates one kanban card from the task spec and places it in the backlog. Useful for incremental execution or when the user wants to run a specific piece first.
   - **Dispatch an entire epic/spec** — Creates all task specs as kanban cards with dependency wiring in one batch. The auto-promoter runs them in dependency order. Equivalent to "approve all phases" but triggered from the spec view.

```
Spec Explorer (with task specs)     Focused View

specs/                              # 01: Sandbox Backends
  01-sandbox-backends.md
    task-define-interface.md  ●     ## Vision
    task-local-backend.md     ●     Extract SandboxBackend interface...
    task-refactor-runner.md   ○
    task-retire-executor.md   ○     ## Tasks  [Dispatch All ▶]
  02-storage-backends.md
    task-storage-iface.md     ○     ● define-interface  [Dispatch ▶]
    task-fs-backend.md        ○       Files: backend.go (new)
                                      Depends on: —
● = dispatched (on board)
○ = draft (not yet dispatched)      ● local-backend  [Dispatched]
                                      Files: backend_local.go, exec.go
                                      Depends on: define-interface

                                    ○ refactor-runner  [Dispatch ▶]
                                      Files: runner.go, execute.go
                                      Depends on: local-backend

                                    ── Dependency Graph ──
                                    define-iface ──▶ local-backend ──▶ refactor
                                         └──────────▶ retire-executor ◀── refactor
```

**Task spec files** live alongside their parent spec, either as child markdown files in a subdirectory or as sections within the spec itself. The chat agent generates them, but the user can edit them directly in the focused view before dispatching. Once dispatched, the task spec content becomes the kanban card's prompt — the agent executing the task sees the full implementation detail.

**Feedback loop:** When a dispatched task completes, the agent can update both the task spec (mark complete, add implementation notes) and the parent spec (update the strategy section if the implementation revealed something new). The focused view shows these updates live. This closes the loop between high-level planning and ground-level execution.

### Human-in-the-Loop Planning Workflow

The UX above handles visualization. But the harder UX problem is *planning itself* — the iterative, multi-step process where a human and AI collaborate to turn a vague goal ("move to cloud") into a concrete, executable plan. This is not a one-shot operation. It requires drafting, reviewing, adjusting, approving, monitoring, and re-planning.

#### The Planning Loop

A large initiative goes through this cycle, potentially multiple times:

```
  ┌─────────────────────────────────────────────────────────────┐
  │                                                             │
  ▼                                                             │
Draft ──▶ Review ──▶ Approve ──▶ Execute ──▶ Assess ──┬──▶ Done
  ▲          │                      │          │       │
  │          ▼                      ▼          ▼       │
  └──── Revise                  Intervene   Re-plan ───┘
```

Each stage needs explicit UX support. The current spec only covers Execute and a bit of Assess. Here's the full design:

#### Stage 1: Draft — Planner Task as a Conversation

The planner task currently runs once and produces a batch of tasks. But for a complex initiative, the first plan is rarely right. The planner should support **iterative refinement before committing**.

**Design: Planner tasks produce a draft, not tasks.**

When a planner task completes, it moves to `waiting` (not `done`), and its result contains the proposed plan as structured JSON. The plan is not yet materialized into tasks. The user reviews it first.

**Planner result display** (in the task detail modal):

```
┌──────────────────────┬──────────────────────────────────────┐
│ Planner: M1 Sandbox  │ Proposed Plan                        │
│                      │                                      │
│ ── Status ──         │ epic: sandbox-backends                │
│ ⏸ Waiting for review │ 2 phases · 7 tasks · 1 gate          │
│                      │ Est. cost: ~$3-5                      │
│ ── Spec Source ──    │                                      │
│ 01-sandbox-backends  │ ── Phase 1: Interface Extraction ──  │
│ Phase 1              │                                      │
│                      │ 1. Define SandboxBackend interface    │
│ ── Feedback ──       │    Files: backend.go (new)            │
│ (input area for      │    Depends on: —                      │
│  revision notes)     │    Acceptance: interface compiles     │
│                      │                                      │
│                      │ 2. Implement LocalBackend             │
│                      │    Files: backend_local.go (new),     │
│                      │           executor.go                 │
│                      │    Depends on: #1                     │
│                      │    Acceptance: existing tests pass    │
│                      │                                      │
│                      │ 3. Refactor Runner                    │
│                      │    Files: runner.go, execute.go,      │
│                      │           container.go                │
│                      │    Depends on: #2                     │
│                      │    Acceptance: runContainer uses      │
│                      │                backend.Launch()       │
│                      │                                      │
│                      │ ... (4 more tasks)                    │
│                      │                                      │
│                      │ ── Phase 1 Gate ──                    │
│                      │ Run: go test ./..., go vet ./...      │
│                      │                                      │
│ [Revise]             │ [Approve & Create Tasks]  [Discard]  │
└──────────────────────┴──────────────────────────────────────┘
```

**Key interactions:**

- **Approve & Create Tasks**: materializes the plan into actual backlog tasks with dependency wiring. Planner task moves to `done`. This is the point of no return (though tasks can be cancelled later).

- **Revise**: user types feedback in the left panel input ("Split task 3 into runner.go and execute.go separately", "Add a task for updating mock tests", "Move listing into phase 2"). Submits feedback. Planner task resumes with the feedback, produces a revised plan. Returns to `waiting` with the new proposal. The user can revise as many times as needed.

- **Discard**: cancels the planner task. No tasks created.

**Why this works:** The existing `waiting` → feedback → resume loop already exists for regular tasks. The planner reuses it. The only new UI is the structured plan rendering in the right panel (instead of raw text output).

#### Stage 2: Review — Plan Visualization Before Commitment

Before approving, the user needs to understand the plan's structure. The proposed plan rendering (above) shows a flat list. For complex plans, add a **dependency graph visualization**:

```
┌─ Plan Graph ─────────────────────────────────────────────────┐
│                                                              │
│  ┌───────────┐                                               │
│  │ 1. Define │──┬──▶ ┌──────────┐──▶ ┌──────────────┐       │
│  │ interface │  │    │ 2. Local │    │ 3. Refactor  │       │
│  └───────────┘  │    │ backend  │    │ Runner       │──┐    │
│                 │    └──────────┘    └──────────────┘  │    │
│                 │                                      │    │
│                 └──▶ ┌──────────┐                      ▼    │
│                      │ 5. Move  │──▶ ┌──────────────┐       │
│                      │ listing  │    │ 6. Retire    │       │
│                      └──────────┘    │ executor     │       │
│                                      └──────────────┘       │
│                                            │                │
│                                            ▼                │
│                                      ┌──────────┐          │
│                                      │ Gate:    │          │
│                                      │ verify   │          │
│                                      └──────────┘          │
│                                                              │
│  Legend: ■ backlog  ◉ in-progress  ✓ done  ✕ failed         │
└──────────────────────────────────────────────────────────────┘
```

This is rendered as an ASCII/SVG DAG in the plan review panel. Nodes are colored by status. The graph makes parallelism visible — tasks 2 and 5 can run concurrently; task 6 waits for both 3 and 5.

**Implementation:** Topological layout with Sugiyama-style layering. The dependency data is already available (the planner output has `depends_on_refs`). Render as positioned `<div>` nodes with CSS-drawn connectors (SVG lines or CSS border tricks). No external library needed for small graphs (<20 nodes).

#### Stage 3: Approve — Selective Materialization

Instead of all-or-nothing approval, support **per-phase approval**:

```
── Phase 1: Interface Extraction ──    [Approve Phase 1]
  7 tasks + 1 gate

── Phase 2: Migration ──               [Approve Phase 2] (disabled)
  4 tasks                              "Approve Phase 1 first"
```

Approving Phase 1 creates those 7 tasks + gate in the backlog. Phase 2 remains in the planner's plan, not yet materialized. When Phase 1 completes (gate passes), the user can then approve Phase 2 — potentially after revising it based on what Phase 1 revealed.

This prevents creating 40 backlog tasks for a 5-phase epic upfront. The plan adapts as earlier phases complete and reveal new information.

**Implementation:** The planner task stays in `waiting` after Phase 1 approval. Its result is updated to mark Phase 1 as "materialized." The user can re-enter feedback to revise Phase 2 before approving it. The planner only moves to `done` when all phases are materialized or the user discards remaining phases.

#### Stage 4: Execute — Active Monitoring Dashboard

During execution, the human needs to spot problems early. The epic progress panel gains a **live activity feed**:

```
┌─ sandbox-backends — Live Activity ──────────────────────────┐
│                                                              │
│ 14:32  ✓ define-interface → done (2 turns, $0.42)           │
│ 14:33  ▶ local-backend started                               │
│ 14:33  ▶ move-listing started                                │
│ 14:38  ⚠ local-backend: 3 test failures in runner_test.go    │
│ 14:41  ✓ local-backend → done (4 turns, $0.89)              │
│ 14:42  ▶ refactor-runner started                             │
│ 14:45  ⚠ refactor-runner: modified 12 files (expected 3-5)   │
│                                                              │
│ [Pause Epic]  [View Board]                                   │
└──────────────────────────────────────────────────────────────┘
```

**Warning indicators** (⚠) surface when:
- A task modifies significantly more files than expected (based on the plan's file list)
- A task takes more turns than typical (e.g., >5 turns for a task sized at 2-3 files)
- A task's cost exceeds the per-task budget estimate
- Test failures are detected in the output

**"Pause Epic"** button: sets all backlog tasks in the epic to `ScheduledAt = far future`, effectively freezing auto-promotion. The user can investigate, intervene, then "Resume Epic" to clear the scheduled dates.

The activity feed is built from task events (state_change, span_end) filtered by epic tag. Rendered as a reverse-chronological list with timestamps, task titles, and status icons. Uses the existing SSE task stream — no new endpoint needed.

#### Stage 5: Intervene — Mid-Execution Course Correction

When an agent makes a wrong architectural decision mid-epic, the human needs to correct course without restarting everything.

**Intervention options in the task detail modal:**

1. **Edit prompt of a backlog task**: the task hasn't run yet. The user can revise its prompt to account for decisions made by earlier tasks. Example: "Task 3 defined the interface differently than expected — update task 5's prompt to use the new method signatures."

2. **Provide feedback on a waiting task**: existing mechanism. The agent resumes with the correction.

3. **Cancel and re-create**: cancel a backlog task, create a replacement with an updated prompt. The replacement inherits the original's position in the dependency graph (manually re-wire dependencies).

4. **Insert a new task**: add a task mid-epic that wasn't in the original plan. Tag it with the epic and phase. Wire dependencies to the appropriate predecessors. The dependency graph updates and the auto-promoter schedules it correctly.

For (3) and (4), the UI should make dependency wiring easy. The task creation dialog, when an epic filter is active, shows a **dependency picker**:

```
Dependencies:
  ✓ define-interface (done)
  ✓ local-backend (done)
  ☐ refactor-runner (in-progress)
  ☐ move-listing (backlog)

Select tasks this new task should wait for.
```

Checkboxes with task titles and status badges. Pre-populated based on the task's position in the phase.

#### Stage 6: Assess & Re-Plan — Learning from Execution

After a phase or epic completes, the human needs to assess what happened and decide whether the remaining plan is still valid.

**Post-phase assessment prompt**: When a gate task completes, the epic progress panel shows an assessment section:

```
┌─ Phase 1 Complete ──────────────────────────────────────────┐
│                                                              │
│ Gate: ✓ PASS — all tests, lint, vet clean                   │
│                                                              │
│ Summary:                                                     │
│ • 6 tasks completed in 47 minutes, $3.21 total              │
│ • 2 tasks needed auto-retry (agent_error)                    │
│ • refactor-runner modified 8 files (plan estimated 3)        │
│                                                              │
│ Phase 2 is ready for approval.                               │
│                                                              │
│ Before approving, consider:                                  │
│ • Does the interface from Phase 1 match Phase 2's           │
│   assumptions?                                               │
│ • Were there unexpected changes that affect Phase 2 tasks?   │
│                                                              │
│ [Review Phase 2 Plan]  [Revise Phase 2]  [Approve Phase 2]  │
└──────────────────────────────────────────────────────────────┘
```

**"Revise Phase 2"** sends feedback to the planner task (which is still in `waiting`), asking it to re-plan Phase 2 based on what actually happened in Phase 1. The planner agent reads the current codebase (post-Phase 1 changes) and board.json (with full results from Phase 1 tasks) and produces an updated Phase 2 plan.

This is the key re-planning loop: the plan evolves based on reality, not just the original spec.

#### Putting It Together: The Full Human Workflow

Here's how a human drives the cloud move using this system:

```
1. Human writes/refines spec files (specs/01-sandbox-backends.md, etc.)
   └─ This is creative work. No automation.

2. Human creates a planner task for M1 Phase 1
   └─ "Plan implementation of specs/01-sandbox-backends.md Phase 1"

3. Planner agent proposes 7 tasks + 1 gate
   └─ Human reviews the plan graph, task prompts, file lists
   └─ Human provides feedback: "Combine tasks 4 and 5, they're too small"
   └─ Planner revises: 6 tasks + 1 gate
   └─ Human approves Phase 1

4. Auto-promoter runs tasks in dependency order
   └─ Human monitors live activity feed
   └─ Task 3 makes an unexpected interface choice
   └─ Human provides feedback on Task 3 (waiting state)
   └─ Task 3 resumes and corrects course

5. Phase 1 gate runs and passes
   └─ Human reviews post-phase assessment
   └─ Human asks planner to revise Phase 2 based on Phase 1 results
   └─ Planner produces updated Phase 2 plan
   └─ Human approves Phase 2

6. Repeat 4-5 for Phase 2

7. Human creates planner for M2, M3, etc.
   └─ Each milestone builds on the stable foundation of completed milestones
```

The human's role is **idea provider and steering**, not spec writer or operator. The human describes what they want; the system (via agents) drafts, structures, and decomposes. The human reviews, redirects, and approves. The system handles scheduling, execution, and verification. Specs are the system's intermediate representation — visible and editable by the human, but not produced by them.

---

## Example: Using the System

**Step 1:** Create a planner task:
```
POST /api/tasks
{
  "prompt": "Plan implementation of specs/01-sandbox-backends.md Phase 1",
  "kind": "planner"
}
```

**Step 2:** Planner agent reads the spec, explores the codebase, outputs:
```json
{
  "epic_slug": "sandbox-backends",
  "phases": [
    {
      "phase": 1,
      "tasks": [
        {"ref": "interface", "title": "Define SandboxBackend/SandboxHandle interfaces", ...},
        {"ref": "local", "title": "Implement LocalBackend", "depends_on_refs": ["interface"], ...},
        {"ref": "runner", "title": "Refactor Runner to use SandboxBackend", "depends_on_refs": ["local"], ...},
        {"ref": "registry", "title": "Update containerRegistry for handles", "depends_on_refs": ["runner"], ...},
        {"ref": "listing", "title": "Move ListContainers into LocalBackend", "depends_on_refs": ["local"], ...},
        {"ref": "retire", "title": "Remove ContainerExecutor and osContainerExecutor", "depends_on_refs": ["runner", "listing"], ...}
      ],
      "gate": true
    }
  ]
}
```

**Step 3:** Runner creates 6 tasks + 1 gate, all tagged `epic:sandbox-backends phase:1`, with proper dependency wiring. Auto-promoter picks up "interface" first (no deps), then "local" and "listing" in parallel (both depend only on "interface"), etc.

**Step 4:** Each task agent sees full results from its dependencies in board.json. Task "runner" knows exactly what `SandboxBackend` interface was defined because "interface" task's full result is in its board context.

**Step 5:** After all 6 tasks complete, the gate task runs: full test suite, lint, vet. If it passes, phase 2 tasks (if any) become promotable.

---

## Cross-Cutting Concerns

### Planner Re-Planning

If a planner's decomposition is wrong (too many tasks, wrong dependencies), the user can:
- Cancel the planner task's created tasks (batch cancel via epic tag)
- Edit the spec
- Create a new planner task

The system doesn't try to "amend" a plan. It's cheaper to cancel and re-plan.

### Epic Tag Discovery

The `GET /api/epics` endpoint discovers epics by scanning tags. No registration step needed. Creating tasks with `epic:foo` tags automatically creates the "foo" epic in the progress view.

### Interaction with Existing Features

| Feature | Impact |
|---|---|
| Auto-promoter | No change — already respects `DependsOn` and `CriticalPathScore` |
| Auto-retry | Works per-task as today — a failed epic task retries independently |
| Batch creation | Planner reuses the same logic; handler extracts it into shared function |
| Ideation | Independent — ideation creates independent tasks, planner creates coordinated ones |
| Oversight | Per-task as today — gate tasks get their own oversight |
| Refinement | Per-task — but planner tasks produce refined prompts as part of decomposition |

### Board.json Size Budget

Worst case: 40-task epic, 7 direct deps with full context, 33 same-epic siblings at 2KB each.
- Direct deps: 7 × 4KB = 28KB
- Same-epic siblings: 33 × 4KB = 132KB → exceeds 64KB threshold

Mitigation: cap same-epic sibling count. Include the N nearest (by dependency distance) same-epic siblings, where N = 15. Remaining same-epic siblings use the 500/1000 limits.

---

## Relationship to Other Specs

This spec is independent of the cloud/platform milestones (M1–M8). It can be implemented against the current codebase at any time. Once in place, it enables automated task decomposition for any spec — create a planner task pointing at a spec file and the system handles the rest.

### Dependency on File Explorer (M4)

The epic coordination UX is fundamentally about **managing and iterating on spec markdown files**. The planning loop — draft, review, revise, approve — requires:

1. **A file explorer** to browse and select spec files from the workspace
2. **A focused markdown viewer/editor** for reading and updating specs inline
3. **Chat-driven spec iteration** where a background agent updates spec files based on conversation, and the focused view reflects changes live

This means the file explorer panel from [file-explorer.md](../foundations/file-explorer.md) (at minimum Phase 1: read-only browsing + preview) is a prerequisite for the full epic coordination UX. The planner task creation dialog should allow selecting a spec file from the explorer rather than requiring the user to type a file path.

The envisioned workflow: the user opens a spec in the focused markdown view, iterates on it via a chat stream (the planner agent proposes changes, the user reviews in the markdown view), then breaks the finalized spec into kanban tasks that appear in the existing board. The spec file itself gets updated as tasks execute and reveal new information — closing the loop between planning and execution.

**Implementation order:** File explorer Phase 1 (read-only) → Epic coordination P1-P2 (planner + board context) → File explorer Phase 2 (editing) → Epic coordination UX (chat-driven spec iteration).

---

## Design Philosophy: Spec-Centric, Not Spec-Writing

A critical distinction: the system is **spec-centric** (specs are the central organizing artifact) but **not spec-writing** (the human is not expected to write specs). The human's role is idea provider and steering; the agent drafts, structures, and iterates on specs based on the human's direction.

This means:
- The user never faces a blank spec template. They describe an idea in natural language, and the agent proposes a structured spec.
- The spec is the system's **intermediate representation** — a shared artifact that both human and agent can read, understand, and modify.
- The spec is visible and editable by the human, but the human is not responsible for producing it from scratch.

**Comparison with spec-writing tools:** Tools like Kiro require the user to write specs in a prescribed format. Wallfacer inverts this — the user provides ideas, the system produces specs. The user's cognitive load is review and steering, not authoring.

**Trust and familiarity tension:** If the human doesn't write the spec, they have lower familiarity with its contents. But the spec still serves as the "design document" that the human needs to understand and trust before approving execution. This creates a tension:

- **Lower authorship** → lower familiarity → harder to review
- **Structured format** → easier to scan → partially compensates
- **Iterative chat refinement** → human steers the content → builds familiarity through dialogue rather than writing

The system mitigates this by making spec review an active dialogue (the chat stream) rather than passive reading. The human doesn't read a 500-line spec top to bottom — they ask questions, request changes, and build understanding through interaction.

**Open question:** When the agent generates a spec with errors, is the human's review burden higher or lower compared to a spec they wrote themselves? Writing builds deep familiarity but is slow; reviewing builds surface familiarity but is fast. The answer likely depends on spec complexity and the human's domain expertise. This may need real usage data to resolve.

---

## Information Input as Core Value

Model capabilities are a variable — they improve over time. Orchestration logic is replaceable — better patterns emerge. But **information acquisition and injection is a permanent bottleneck.** Regardless of how capable the model becomes, garbage in, garbage out doesn't disappear.

Two categories of information input:

### Internal Information (Human → System)

New ideas, brainstorming insights, product direction changes, domain knowledge that isn't in the codebase. This information is:
- **Unstructured**: arrives as natural language, often vague or incomplete
- **Context-dependent**: its meaning depends on the current state of the project
- **High-impact**: a single idea can invalidate multiple specs

The system needs to help the human do **contextualization** (where does this idea fit in the spec graph?) and **impact assessment** (what existing work does this affect?).

**UX challenge:** The friction of capturing ideas must be extremely low (otherwise the human doesn't bother), but the contextualization quality must be high (otherwise the idea lands in the wrong place or its implications are missed).

**Possible input entry points:**

| Entry point | Friction | Contextualization | Best for |
|-------------|----------|-------------------|----------|
| **Chat stream** (in spec mode) | Low — type and send | High — agent has spec context | Ideas related to the current spec |
| **Quick capture / inbox** | Very low — one-line input, no context selection | Low — agent must infer context | Drive-by thoughts, ideas that don't fit the current focus |
| **Structured form** (new spec dialog) | Medium — fill in fields | Medium — fields provide structure | Well-formed feature requests |
| **Annotation** (highlight text + comment) | Low — inline with reading | High — attached to specific content | Reactions to existing spec content |

The chat stream is the primary input channel. Quick capture is a stretch goal for ideas that arrive outside of a focused spec session. The system should route captured ideas to the appropriate spec (or flag them as needing a new spec) based on semantic similarity.

### External Information (World → System)

API documentation, library references, competitor analysis, standards. This is relatively well-served by existing tool ecosystems (MCP, web search, file reading). The system should make it easy to attach external references to specs and tasks, but the hard problem is internal information, not external.

### Why This Matters for Design

Information input shapes every other design decision:
- **Drift detection** is ultimately about noticing that internal information (the human's latest thinking) has diverged from the system's model (the spec). The better the system captures evolving ideas, the less drift accumulates.
- **Operation regimes** are driven by information quality: when the human's input is vague, the system needs more interactive clarification (human-driven). When the input is precise, the system can execute autonomously (agent-driven).
- **Spec lifecycle transitions** are triggered by information events: a new idea moves a spec from `complete` to `stale`; a validation conversation moves it from `drafted` to `validated`.

---

## UI: Three-Mode Framework

The epic coordination UX naturally organizes into three observation modes of the same underlying spec DAG. Each mode serves a distinct cognitive task:

```
┌────────┐      ┌────────┐      ┌────────┐
│  Map   │ ◀──▶ │  Spec  │ ◀──▶ │ Board  │
│  Mode  │      │  Mode  │      │  Mode  │
└────────┘      └────────┘      └────────┘
 Global view     Single spec     Task execution
 Dependencies    Iteration       Monitoring
 Impact analysis Chat + edit     Kanban
```

### Map Mode (Global Dependency Graph)

A zoomed-out view of the entire spec DAG. Nodes are design specs, edges are `depends_on`/`blocks` relationships. Node color/icon reflects design maturity and implementation state. Drift warnings appear as edge decorations.

**Cognitive task:** "Where are we? What's blocked? What's at risk?"

**Key interactions:**
- Click a node → switch to Spec Mode for that spec
- Hover a node → tooltip with status summary (design maturity, implementation state, cost, task count)
- Highlight a node → show its upstream and downstream dependencies
- Filter by track (foundations, local, cloud, shared)
- Filter by state (show only stale, show only implementing)

### Spec Mode (Single Spec Iteration + Chat)

The split-pane view described in [Design Option: Spec-Centric Planning View](#design-option-spec-centric-planning-view). Spec explorer on the left, focused markdown view in the center, chat stream on the right.

**Cognitive task:** "What does this spec say? Is it right? What should change?"

This is where both operation regimes live:
- **Human-driven**: frequent chat interaction, agent proposes changes, human steers
- **Agent-driven**: human reviews completed work, provides feedback only when needed

### Board Mode (Task Execution)

The existing kanban board with epic filter bar, phase dividers, and progress panel from the [Board Integration](#board-integration) section.

**Cognitive task:** "What's running? What's stuck? What needs attention?"

### Mode Switching

Switching between modes should be **zero-cost** — a single click or keyboard shortcut, with the context preserved. If the user is viewing `sandbox-backends.md` in Spec Mode and switches to Board Mode, the board should auto-filter to the `epic:sandbox-backends` tag. If they click a task in Board Mode and switch to Spec Mode, the spec explorer should navigate to that task's parent spec.

**Implementation:** The three modes share state (selected spec, selected epic, selected task). Mode switching changes the view but not the selection. The header bar has three mode tabs: `[Map] [Spec] [Board]`.

### Open Problem: Cross-Spec Cognitive Management

The three-mode framework handles single-spec focus well, but struggles with **cross-spec awareness**. When spec count exceeds what a human can hold in working memory (~7-10 specs), the human loses global coherence:

- Map Mode shows the graph structure but not the content
- Spec Mode shows one spec's content but not the global picture
- Board Mode shows task-level progress but not design-level coherence

**No clean solution exists for this.** Possible mitigations:

1. **Context panel** anchored to the current spec, showing summaries of related specs (dependencies, blocked-by, recently changed). Risk: becomes VS Code-style tab overload.
2. **Digest generation**: periodic agent-generated summary of global spec state ("3 specs are stale, 2 have active drift warnings, sandbox-backends completed but container-reuse hasn't been updated yet"). Risk: summary quality depends on agent capability.
3. **Notification feed**: surface important state changes (staleness, drift, completion) as a timeline. The user stays in Spec Mode but sees a stream of "things you should know about other specs." Risk: alert fatigue.
4. **Rely on human strategy**: accept that the human can't hold everything in mind. The system's job is to *surface problems when they matter* (e.g., warn about stale dependencies when the user is about to dispatch tasks), not to maintain global awareness continuously.

Option 4 is the most realistic for now. The system should be **reactive** (warn when it matters) rather than **proactive** (maintain continuous global awareness). The human can always switch to Map Mode for a global check, but the default is focused work with targeted warnings.

---

## Open Problems

Three fundamental tensions that this spec does not fully resolve:

### 1. Drift Detection Investment Timing

Building drift detection infrastructure now may be premature — model capabilities are improving rapidly and may make custom index infrastructure obsolete. But deferring entirely risks losing control as spec count grows.

**Current stance:** Invest minimally in structure (the `affects` field in spec frontmatter for spec-to-code mapping) and rely on model capability (approach C) for semantic assessment. Re-evaluate when spec count exceeds ~50 or codebase exceeds ~500K LOC. The `affects` field is cheap to maintain and useful beyond drift detection (impact analysis, spec navigation), so it's a safe investment regardless of model trajectory.

### 2. Cross-Spec Cognitive Management

When the number of specs exceeds human working memory capacity, how does the human maintain correct global understanding without reading every spec? Map Mode helps with structure but not content. Digest summaries help with content but may be unreliable.

**Current stance:** Reactive warnings (surface problems when they become actionable) rather than proactive awareness (maintain continuous global picture). The system warns you about stale dependencies when you're about to dispatch tasks, not as a background notification. Accept that the human will periodically need to do a "global review" pass in Map Mode, and make that pass efficient with good filtering and status indicators.

### 3. Agent-Generated Spec Trust

If the human doesn't write specs, they have lower familiarity with spec content. But specs serve as the authoritative design document — the human needs to understand and trust them to make good approval decisions.

**Current stance:** Build familiarity through interactive dialogue (the chat stream) rather than authoring. The human doesn't read specs passively — they interrogate them ("why did you choose this interface shape?", "what happens if we change X?"). This active review builds understanding without requiring the human to have written the content.

**Risk:** If the agent generates a spec with a subtle error (e.g., an incorrect assumption about an existing interface), the human may not catch it during review because they didn't write it. The mitigation is gate tasks: even if a spec is wrong, the gate catches the failure at implementation time. The cost is wasted execution, not shipped bugs.

**Validation needed:** Whether review burden for agent-generated specs is higher or lower than for human-written specs in practice. This likely varies by spec complexity and human domain expertise. Real usage data is needed.
