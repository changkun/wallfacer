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

Specs and the tasks derived from them serve different purposes and should live at different abstraction levels:

| Layer | Focus | Content | Audience |
|-------|-------|---------|----------|
| **Spec** (strategy) | Vision, goals, constraints, architecture decisions | Why we're building this, what success looks like, key design choices, cross-cutting concerns, risk areas | Human planner |
| **Task spec** (implementation) | Concrete implementation steps | Which files to change, what functions to add, acceptance criteria, test plan, dependency wiring | Agent executor |

A spec like `01-sandbox-backends.md` says *"extract a `SandboxBackend` interface so we can swap container runtimes"* — it explains the motivation, the interface shape, and the migration strategy. A task spec derived from it says *"create `internal/sandbox/backend.go` with `Launch`, `ListContainers`, `Stop` methods; ensure backward compat with existing `os/exec` path; add tests in `backend_test.go`"* — it's an actionable work order.

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

The human's role is **editorial, not operational**: they write specs, review plans, provide directional feedback, and decide when to proceed. The system handles decomposition, scheduling, execution, and verification.

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

This means the file explorer panel from [04-file-explorer.md](04-file-explorer.md) (at minimum Phase 1: read-only browsing + preview) is a prerequisite for the full epic coordination UX. The planner task creation dialog should allow selecting a spec file from the explorer rather than requiring the user to type a file path.

The envisioned workflow: the user opens a spec in the focused markdown view, iterates on it via a chat stream (the planner agent proposes changes, the user reviews in the markdown view), then breaks the finalized spec into kanban tasks that appear in the existing board. The spec file itself gets updated as tasks execute and reveal new information — closing the loop between planning and execution.

**Implementation order:** File explorer Phase 1 (read-only) → Epic coordination P1-P2 (planner + board context) → File explorer Phase 2 (editing) → Epic coordination UX (chat-driven spec iteration).
