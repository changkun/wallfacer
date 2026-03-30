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
updated: 2026-03-31
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

## Sandbox Execution Model

Spec mode runs inside a sandbox container, same as task execution on the board. Entering spec mode launches (or attaches to) a long-lived planning sandbox. The spec explorer and the chat agent both operate within this sandbox.

**Zero permission prompts.** The chat agent has full read/write access to the specs folder inside the sandbox — no "allow file edit?" dialogs, no approval gates. The agent creates, renames, splits, and edits spec files autonomously. The user steers via chat; the agent executes immediately. This is safe because:

- The sandbox is scoped to spec documents, not production code.
- Spec files are version-controlled; any change is a `git diff` away from reversal.
- The agent only modifies files under the specs folder. It can *read* the full workspace (to understand the codebase when drafting specs), but *writes* are confined to specs.

**Lifecycle.** The planning sandbox starts when the user enters spec mode and stays alive across spec mode sessions (same container reuse model as task workers). It is destroyed on explicit teardown or workspace switch.

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

**Left pane — Spec Explorer:** Shows *only* the specs folder — not the full workspace file tree. Tree rooted at the specs directory with status badges and recursive progress indicators (e.g., "4/6" counting all leaves in the subtree, not just direct children). Clicking opens in the focused view. Collapsible subtrees at any depth. Reuses file explorer infrastructure but with a fixed root.

**Center pane — Focused Markdown View:** Renders the selected spec as formatted markdown. Live updates when the agent modifies it in the sandbox. Children listed with status. If it's a leaf spec, shows dispatch button.

**Right pane — Chat Stream:** Conversation for iterating on the focused spec. The user types directives; the agent executes immediately inside the planning sandbox — no permission prompts. It reads the spec tree and codebase, then writes spec files directly. Changes appear live in the explorer and focused view.

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

### Specs Folder Location and Conflicts

The spec system assumes a `specs/` directory exists in the workspace. Several unresolved questions:

**Where should the specs folder live?**

- **Option A — Workspace root (`<repo>/specs/`).** Specs are version-controlled alongside the code they describe. PRs can include both spec changes and implementation. Natural for single-repo projects.
- **Option B — Wallfacer data directory (`~/.wallfacer/specs/<fingerprint>/`).** Specs live outside the repo, keyed by workspace fingerprint (same scheme as AGENTS.md). Doesn't pollute the repo. Works for multi-repo workspace groups where no single repo owns the specs.
- **Option C — User-configurable.** A setting (`WALLFACER_SPECS_DIR` or per-workspace config) points to the specs root. Defaults to option A if the directory exists, otherwise option B.

**What if the project already has a `specs/` directory?**

Some projects use `specs/` for OpenAPI specs, RFCs, ADRs, or other purposes. Wallfacer's spec frontmatter schema (status, depends_on, affects, effort, etc.) would conflict with or pollute existing content.

- **Option A — Namespaced directory.** Use `.wallfacer/specs/` or `wallfacer-specs/` instead of bare `specs/`. Avoids all conflicts but looks foreign.
- **Option B — Coexist with detection.** Scan the existing `specs/` directory. Files with valid Wallfacer frontmatter are managed specs; everything else is ignored. Risk: false positives if existing files happen to have similar YAML fields.
- **Option C — Explicit init.** `wallfacer specs init` creates the specs directory (prompting for location if `specs/` already exists). The chosen path is stored in workspace config. No implicit detection.

**Multi-repo workspace groups — where do cross-repo specs live?**

A workspace group can mount multiple repos (e.g., `~/api`, `~/frontend`, `~/infra`). A spec may span repos ("add a new API endpoint and consume it in the frontend"). Questions:

- **One specs tree or many?** Each repo could have its own `specs/` with its own tree, or there could be a single unified tree that covers the whole workspace group. Per-repo trees are simpler but can't express cross-repo dependencies. A unified tree needs a home outside any single repo.
- **If unified, where?** The wallfacer data directory (`~/.wallfacer/specs/<fingerprint>/`) is the natural candidate — it's already keyed by workspace group. But then specs aren't version-controlled with the code they describe, and PRs can't include spec changes.
- **Hybrid?** Per-repo `specs/` for repo-scoped work, plus a wallfacer-managed cross-repo layer for specs that span boundaries. The spec explorer would merge both views. Adds complexity — two sources of truth, potential conflicts in `depends_on` paths.
- **`affects` paths across repos.** Currently `affects` lists paths relative to the repo root. With multiple repos, paths need qualifying (`api/internal/handler/foo.go` vs `frontend/src/api.ts`). The workspace mount names could serve as prefixes, but this couples specs to workspace config.

**Leaning toward `.wallfacer/` as canonical home.** This solves multi-repo cleanly and avoids polluting repos. But it detaches specs from the code they describe — specs aren't in PRs, aren't reviewed alongside implementation, and aren't portable if someone clones the repo without wallfacer.

A sync mechanism could bridge this: wallfacer writes a read-only `specs/` mirror into each repo (or a configurable subset), regenerated on spec change. The mirror could be full copies or stubs (frontmatter + link back to wallfacer). This gives repos a version-controlled snapshot without the repo owning the source of truth. Open sub-questions:

- Should sync be opt-in per repo, or automatic?
- Full spec copies vs summary stubs vs a single `specs.json` manifest?
- Should synced files be `.gitignore`d (pure local cache) or committed (visible in PRs)?
- If committed, who resolves conflicts when someone edits the synced copy directly?

No clear winner yet. The single-repo case (option A above) should ship first; multi-repo support can layer on top once the model is proven.

**What if the project has no specs folder?**

First time the user enters spec mode, the system needs to bootstrap. Options:

- **Auto-create on first use.** Entering spec mode creates the specs directory (at the configured location) with a minimal README. Low friction but surprising if the user was just exploring.
- **Prompt once.** "No specs directory found. Create one at `<repo>/specs/`?" with option to choose a different path. One-time friction, explicit consent.
- **Require explicit init.** Spec mode is grayed out until the user runs init. Most explicit, most friction.

### Agent-Generated Spec Trust

If the user doesn't write specs, they have lower familiarity with content. Mitigations:

- Build familiarity through interactive chat dialogue, not passive reading
- Verification specs catch implementation failures
- The spec is always editable — the user can modify anything before dispatching
