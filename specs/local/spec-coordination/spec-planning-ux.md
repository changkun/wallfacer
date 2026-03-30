---
title: Spec Planning UX
status: drafted
depends_on:
  - specs/local/spec-coordination.md
  - specs/foundations/file-explorer.md
  - specs/foundations/host-terminal.md
affects:
  - ui/js/
  - ui/index.html
  - internal/handler/explorer.go
effort: xlarge
created: 2026-03-29
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Spec Planning UX

Depends on [spec-document-model.md](spec-document-model.md).

---

## Core Workflow

The user's workflow is a loop:

```
1. Propose an idea (natural language, any level of vagueness)
2. Agent drafts a spec
3. Review and iterate ("this is wrong", "add X", "break this down")
4. When small enough → dispatch leaf specs to the kanban board
5. Monitor execution, feed results back into specs
6. Repeat for the next piece
```

The UX must support every step of this loop with minimal friction.

---

## Two-Mode UI

The system has two views of the same underlying work:

```
┌────────┐           ┌────────┐
│  Spec  │  ◀──────▶ │ Board  │
│  Mode  │           │  Mode  │
└────────┘           └────────┘
 Planning              Execution
 Iteration             Monitoring
 Tree navigation       Flat kanban
```

### Spec Mode

A split-pane view for planning work:

```
+--header------------------------------------------------------+
| [Board] [Specs]   workspace-group-tabs   [search] [settings] |
+----------+---------------------------+-----------------------+
|          |                           |                       |
| Spec     |  Focused Markdown View    |  Chat Stream          |
| Explorer |                           |                       |
|          |  # Sandbox Backends       |  > Break this into    |
| specs/   |                           |    sub-specs, each    |
|  foundn/ |  ## Problem               |    touching 2-5 files |
|    sandbo|  Wallfacer uses os/exec   |                       |
|      defe|  directly in the runner.  |  Agent: I'll create   |
|      loca|  This couples container   |  3 sub-specs...       |
|      refa|  lifecycle to the runner  |                       |
|    storag|  package...               |  [spec tree updated,  |
|  local/  |                           |   3 new files shown   |
|    spec-c|  ## Design                |   in explorer]        |
|    deskto|  Extract a SandboxBackend |                       |
|           |  interface...             |  > The refactor-runner|
|           |                           |    spec is too big,   |
|           |  ## Children              |    split it further   |
|           |  - ✓ define-interface     |                       |
|           |  - ✓ local-backend       |  Agent: Split into    |
|           |  - ○ refactor-runner     |  two sub-specs...     |
|           |                           |                       |
+----------+---------------------------+-----------------------+
```

**Left pane — Spec Explorer:** Tree rooted at `specs/`. Shows spec files with status badges and recursive progress indicators (e.g., "4/6" counting all leaves in the subtree, not just direct children). Clicking opens in the focused view. Collapsible subtrees at any depth. Reuses file explorer infrastructure.

**Center pane — Focused Markdown View:** Renders the selected spec as formatted markdown. Live updates when the agent modifies it. Children listed with status. If it's a leaf spec, shows dispatch button.

**Right pane — Chat Stream:** Conversation for iterating on the focused spec. The user types directives; the agent reads the spec + codebase and proposes changes. Changes appear as diffs in the center pane.

### Board Mode

The existing kanban board, unchanged. Shows dispatched leaf specs as tasks. The board stays flat — all structure lives in the spec tree.

When clicking a task that was dispatched from a spec, the task detail shows a link back to its source spec. Clicking it switches to Spec Mode focused on that spec.

### Mode Switching

Zero-cost: single click or keyboard shortcut, context preserved. If viewing a spec in Spec Mode and switching to Board Mode, the board highlights tasks dispatched from that spec's subtree. If viewing a task in Board Mode and switching to Spec Mode, the explorer navigates to that task's source spec.

---

## Chat-Driven Iteration

The chat stream is the primary interaction channel. Examples of what the user can say:

| User says | Agent does |
|-----------|-----------|
| "I want to refactor the sandbox layer" | Drafts a new spec: problem statement, proposed approach, key decisions |
| "This section is too vague" | Expands the section with specifics from the codebase |
| "Break this into sub-specs" | Proposes child specs with acceptance criteria and dependencies |
| "The interface needs a fourth method" | Updates the spec, flags affected children as potentially stale |
| "Dispatch the first two sub-specs" | Creates kanban tasks from the leaf specs, links them back |
| "What's the status of this spec?" | Summarizes: children progress, drift warnings, dispatched tasks |

The agent always has context: the focused spec, the spec tree, the codebase, and board state. It can read sibling specs, check existing implementations, and propose changes that account for the broader picture.

### Spec File Conventions

For the agent to read and update specs, leaf specs follow a light convention:

```markdown
---
title: Define SandboxBackend interface
status: validated
depends_on: []
affects:
  - internal/sandbox/backend.go
effort: small
---

## Goal

Define `SandboxBackend` and `SandboxHandle` interfaces in a new
`internal/sandbox/` package.

## What to Change

- Create `internal/sandbox/backend.go` with interface definitions
- Create `internal/sandbox/handle.go` with handle interface
- Add doc comments on all exported methods

## Acceptance Criteria

- Interfaces compile
- No existing code is modified (pure addition)
- Doc comments explain the contract for each method

## Dependencies

None — this is the first spec in the tree.
```

Non-leaf specs are less structured — they contain whatever the human and agent have iterated to: problem statements, design decisions, diagrams, open questions, links to children.

---

## Dispatch Workflow

### Dispatching a Leaf Spec

The focused view for a `validated` leaf spec shows a dispatch button:

```
┌──────────────────────────────────────────────────┐
│ Define SandboxBackend interface         [Dispatch]│
│ Status: validated · Effort: small                 │
│ Depends on: —                                     │
│                                                   │
│ ## Goal                                           │
│ Define SandboxBackend and SandboxHandle...        │
└──────────────────────────────────────────────────┘
```

**Dispatch** creates a kanban task:
- Prompt = spec content (the full markdown body)
- `DependsOn` = resolved from the spec's `depends_on` field (matching other dispatched specs' `dispatched_task_id`)
- The spec's `dispatched_task_id` is set to the new task's UUID
- The spec's status stays `validated` until the task completes, then moves to `complete`

### Dispatching Multiple Specs

The spec explorer supports multi-select. Select several leaf specs and click "Dispatch Selected." This creates a batch of kanban tasks with proper dependency wiring.

Alternatively, in the chat: "Dispatch all validated leaf specs under sandbox-backends." The agent does the multi-dispatch.

### Undispatching

If a dispatched task is cancelled, the spec's `dispatched_task_id` is cleared and it returns to `validated`. The user can revise the spec and re-dispatch.

---

## Progress Tracking

Progress is visible at every level of the spec tree.

### In the Spec Explorer

```
specs/
  foundations/
    ✅ sandbox-backends.md              6/6 ✓
      ✅ define-interface.md
      ✅ local-backend.md
      ✅ runner-migration.md            3/3 ✓
        ✅ refactor-launch.md
        ✅ refactor-listing.md
        ✅ retire-executor.md
    ✅ storage-backends.md              3/3 ✓
  local/
    📝 spec-coordination.md             0/3
      ✔ spec-document-model.md
      📝 spec-planning-ux.md
      💭 spec-drift-detection.md
```

Non-leaf specs show `done/total` counts that recursively aggregate all leaves in their subtree. `sandbox-backends.md` shows 6/6 (all leaves), and `runner-migration.md` shows 3/3 (its own leaves). Status icons reflect the spec's own status, not children.

### In the Focused View

Non-leaf specs show a children summary section:

```
## Children                                    4/6 leaves done

✅ define-interface — complete ($0.42)
✅ local-backend — complete ($0.89)
  runner-migration — 2/3 leaves done
    ✅ refactor-launch — complete ($0.67)
    ✅ refactor-listing — complete ($0.56)
    ○  retire-executor — validated, not dispatched
○  update-registry — drafted

Total cost: $2.54
```

### On the Board

Tasks dispatched from specs look like regular tasks. The task card shows a small spec badge linking back to the source spec. No other board changes.

---

## Verification

The previous design had dedicated "gate tasks" for milestone verification. In the spec-centric model, verification is just another leaf spec:

```
specs/foundations/sandbox-backends/
  define-interface.md
  local-backend.md
  refactor-runner.md
  move-listing.md
  retire-executor.md
  verify.md                ← leaf spec: "run tests, lint, vet"
```

The `verify.md` spec depends on all other siblings. When dispatched, it runs verification. The user decides whether to include a verification spec — it's not imposed by the system.

This is simpler and more flexible: the user can add verification at any tree level, make it as thorough or light as they want, and skip it entirely for low-risk work.

---

## Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| `S` | Toggle between Spec Mode and Board Mode |
| `Enter` (in explorer) | Open selected spec in focused view |
| `D` (in focused view) | Dispatch current leaf spec |
| `B` (in focused view) | Break down current spec (opens chat with "break this into sub-specs") |

---

## Open Questions

### Cross-Spec Cognitive Management

When spec count exceeds human working memory (~7-10 specs), the user loses global coherence. Mitigations:

1. **Tree collapsing** — only expand the subtree you're working on. Completed subtrees collapse to a single green checkmark.
2. **Status filtering** — show only specs in a particular state (stale, in-progress, not started).
3. **Reactive warnings** — surface problems when they matter (e.g., drift warnings when about to dispatch), not as background noise.

### Entry-Point Document Staleness

`specs/README.md` is a hand-maintained index: status table, dependency graph, ordering rationale. When a spec completes, the README silently drifts — wrong status, stale "Delivers" column, outdated rationale — until someone notices. This is the general problem of derived documents that summarize spec state but aren't part of the spec tree themselves.

**Option A — Generated README.** The README is fully generated from spec frontmatter and a template. `make specs-readme` or a post-completion hook rebuilds it. The template defines the table layout, dependency graph format, and rationale sections. Humans edit the template, not the output.

- Pro: Zero drift by construction. README always matches reality.
- Con: Loses free-form prose (ordering rationale, scaling strategy discussion). Those sections would need to live elsewhere or be template-embedded. Harder to review in PR diffs since the whole file regenerates.

**Option B — Generated sections, manual prose.** The README has fenced marker comments (`<!-- BEGIN status-table -->` / `<!-- END status-table -->`). A generator rewrites only the marked sections; prose outside markers is untouched. Runs as a post-completion hook or CI check.

- Pro: Keeps free-form rationale sections. Only structured sections (status quo, tables, dependency graph) are generated. Surgical diffs.
- Con: More complex generator. Marker discipline required — accidentally deleting a marker breaks the update.

**Option C — Staleness check with blocking.** No generation. Instead, when a spec changes status, the system computes whether any entry-point document references that spec with outdated information (status, deliverables). Surfaces a warning in the spec explorer and optionally blocks further dispatches from the same subtree until the entry-point is updated.

- Pro: Preserves full human authorship. No generator to maintain. Works for any document format.
- Con: Doesn't fix the problem, only nags. The human still does the manual update. "Blocking" may be too aggressive for a README update.

**Option D — Agent-assisted update.** On spec completion, the chat stream proposes a README diff: "spec-document-model is now complete. Here's an updated README reflecting the new status and deliverables." The user reviews and applies with one click, or edits before applying.

- Pro: Human stays in the loop but doesn't have to remember or do the work. The agent has full context (spec content, what shipped, design decisions) to write a good update. No rigid template — the agent adapts to whatever README format exists.
- Con: Requires the agent to understand README conventions. Quality depends on prompt engineering. Still manual (user must click "apply").

These options aren't mutually exclusive. A likely combination: **B + D** — generate the structured sections (tables, status quo block), and have the agent propose updates to free-form sections (rationale, dependency graph annotations) via chat.

### Agent-Generated Spec Trust

If the user doesn't write specs, they have lower familiarity with content. Mitigations:

- Build familiarity through interactive chat dialogue, not passive reading
- Verification specs catch implementation failures
- The spec is always editable — the user can modify anything before dispatching
