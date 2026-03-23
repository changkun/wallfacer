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
