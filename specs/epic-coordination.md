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

## Dependencies on Other Specs

This spec is **M0** — it precedes and enables all other milestones. No dependencies on M1–M8. Can be implemented against the current codebase.

The planner system is what makes M1–M8 executable at scale: instead of manually creating tasks for each milestone, create a planner task pointing at the spec file.
