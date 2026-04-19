# 🤖 Agents & Flows

Wallfacer's execution model builds on four primitives: **agents**, **flows**, **tasks**, and **routines**. Understanding how they compose is the single biggest lever for customising what Wallfacer does on your behalf, picking a different coding harness per step, tightening an agent's system prompt, adding a security-review pass to every implementation, or scheduling a nightly brainstorm.

This guide explains each primitive, how they plug into each other, and works through common recipes.

---

## Mental model

```
┌────────────────────────────────────────────────────────────────┐
│  Task                                                           │
│    prompt + metadata + a flow slug                              │
│       │                                                          │
│       ▼                                                          │
│  Flow          (implement | brainstorm | refine-only | custom)   │
│    ordered chain of agent slugs                                  │
│       │                                                          │
│       ▼                                                          │
│  Agent         (impl | test | refine | title | …)                │
│    title, system prompt, harness, capabilities                   │
│       │                                                          │
│       ▼                                                          │
│  Sandbox       (Claude | Codex)                                  │
│    a CLI running in an ephemeral container                       │
└────────────────────────────────────────────────────────────────┘
```

The four primitives, bottom up:

- **Agent**, the smallest unit. A descriptor saying "this is the role of the impl step, it runs on Claude, it has workspace-write capability, its system prompt starts with these instructions."
- **Flow**, an ordered chain of agents. Some steps can run in parallel; some can be optional. A flow says "run refine, then impl, then test, then commit-msg, title, and oversight in parallel."
- **Task**, the unit of work shown on the board. Every task references a flow by slug.
- **Routine**, a board card with a schedule. When its timer fires it spawns a fresh task against the flow you picked.

All four are backed by a merged registry: **built-in** definitions shipped with the binary plus **user-authored** YAML files under `~/.wallfacer/agents/` and `~/.wallfacer/flows/`.

---

## Agents

### What an agent is

An agent is a descriptor, not a running process. It tells the runner how a particular sub-agent role should behave when a flow step references it.

| Field | Description |
|-------|-------------|
| `slug` | Kebab-case identifier (`impl`, `impl-codex`, `security-review`). Referenced from flow steps and task records. |
| `title` | Human-readable name rendered in UI. |
| `description` | One-line summary for the Agents tab. |
| `harness` | `claude`, `codex`, or empty. Pins this agent to a specific CLI. |
| `capabilities` | Declarative list: `workspace.read`, `workspace.write`, `board.context`. |
| `multiturn` | Advisory; indicates the agent participates in a multi-turn session. |
| `prompt_tmpl` | Optional system-prompt preamble. Prepended to the agent's input by the runner. |

### The built-in catalog

Seven agents ship with Wallfacer, each mapped to a specific sub-agent role the runner knows how to dispatch:

| Slug | Purpose |
|------|---------|
| `impl` | Main implementation turn loop. Writes code, runs tests, iterates until end_turn. |
| `test` | Verification agent. Runs the task's tests and reports pass/fail. |
| `refine` | Prompt refinement. Expands a thin prompt into a detailed implementation spec. |
| `title` | Generates a 2-5 word title for a task card. |
| `oversight` | Produces a high-level summary of what the task did. |
| `commit-msg` | Drafts a commit message from the worktree diff. |
| `ideate` | Scans the workspace and proposes task ideas. |

Browse them in the sidebar **Agents** tab. Clicking a row shows its full descriptor, including the rendered prompt template.

### Cloning a built-in

Built-in agents are read-only. To change anything, the harness, the system prompt, the capabilities, **clone** instead of edit:

1. Open **Agents** in the sidebar.
2. Select a built-in row (for example `impl`).
3. Click **Clone** in the detail pane.
4. The editor opens prefilled with a new slug (`impl-copy`). Tweak the fields you care about.
5. **Save**. The clone is written to `~/.wallfacer/agents/<slug>.yaml` and becomes immediately available in flows.

### Creating from scratch

Click **+ New Agent** at the top right of the tab. A blank editor appears. Fill in slug, title, and any other fields you want pinned, then Save.

### Harness pinning

The **Harness** field is a three-way segmented control: `Default`, `Claude`, `Codex`. It decides which coding CLI this agent runs on.

- `Default` defers to the runner's resolver: task-level sandbox, then workspace default (`WALLFACER_DEFAULT_SANDBOX`), then Claude.
- `Claude` or `Codex` forces this agent to that harness regardless of task-level or workspace-level settings.

This is the single most common reason to clone an agent: give the clone a specific harness pin and reference it from a flow. For example `impl-codex` is `impl` with `harness: codex`.

### System prompt

The **System Prompt** textarea is optional. When non-empty, its contents are **prepended** to the caller's prompt by the runner, separated by a blank line:

```
<system prompt body>

<runtime prompt>
```

No template substitution happens, what you type is what the agent sees. Use this for persona ("You are a conservative reviewer"), style ("reply in bullet points"), or hard rules ("never delete files without asking").

**Important runtime scope.** The preamble takes effect only for agents dispatched through the **flow engine** (non-implement flows, or any custom flow). The built-in `implement` flow's turn loop calls `GenerateCommitMessage`, `GenerateTitle`, and `GenerateOversight` directly with their embedded templates and does not apply the preamble. If you want a custom commit message style for implement-style tasks, clone both the agent and the `implement` flow so the clone gets routed through the engine.

### On-disk format

User-authored agents are YAML at `~/.wallfacer/agents/<slug>.yaml`:

```yaml
slug: impl-codex
title: Implementation (Codex)
description: Implementation pinned to Codex for Rust-heavy workspaces.
harness: codex
capabilities:
  - workspace.read
  - workspace.write
  - board.context
multiturn: true
prompt_tmpl: |
  You are implementing features in a production Rust codebase.
  Prefer small incremental commits over large rewrites.
```

The directory is watched: drop a new file in or edit an existing one, and the runner picks up the change within a couple hundred milliseconds. No restart required.

Override the directory with `WALLFACER_AGENTS_DIR`.

---

## Flows

### What a flow is

A flow is an ordered list of steps, where each step references an agent by slug. At dispatch time the runner walks the chain.

| Field | Description |
|-------|-------------|
| `slug` | Kebab-case identifier referenced from tasks and routines. |
| `name` | Human-readable label shown in the composer dropdown. |
| `description` | One-line summary. |
| `steps` | Ordered list. Each step has `agent_slug`, `optional`, `input_from`, `run_in_parallel_with`. |

### The built-in catalog

| Slug | Chain | What it's for |
|------|-------|---------------|
| `implement` | refine? → impl → test → (commit-msg ‖ title ‖ oversight) | The standard task pipeline. Refine is optional; the three terminal steps run in parallel. |
| `brainstorm` | ideate | Workspace scan that proposes new tasks. Used by the ideation routine. |
| `refine-only` | refine | Expand a prompt into a detailed spec, without implementing it. |
| `test-only` | test | Run the test agent against the current worktree state. |

### Reading a flow row

In the **Flows** tab, each built-in row renders its step chain as pills separated by `→`. Parallel groups appear inside a dashed blue box:

```
refine?  →  impl  →  test  →  ┌ commit-msg ‖ title ‖ oversight ┐
```

- A trailing `?` on a chip marks an optional step (flow skips it on failure).
- `‖` between chips inside a box means they run concurrently via an errgroup.

### Cloning a built-in flow

Same pattern as agents:

1. Open **Flows** in the sidebar, pick a built-in row.
2. Click **Clone**. The editor opens with a new slug (`implement-copy`) and a copy of the step list.
3. Reorder steps by dragging the `⋮⋮` handle, mark steps optional, drop steps, add new ones from the **+ Add step** button.
4. **Save**. The clone lives at `~/.wallfacer/flows/<slug>.yaml`.

### Creating from scratch

Click **+ New Flow**. A blank editor appears with a single empty step; fill in the agent dropdown (populated from the merged Agents catalog), add more steps as needed, save.

### The step editor

Each step row:

- `⋮⋮` drag handle, reorders the step via drag-and-drop.
- `1.` index, the step number.
- agent dropdown, populated from `/api/agents` (built-in + user-authored).
- `optional` checkbox, flow engine logs a warning and continues when this step fails.
- `✕`, removes the step.

**Parallel siblings** (`run_in_parallel_with`) aren't yet editable inline; for now, edit the flow's YAML directly to set up parallel groups. The built-in `implement` flow is the canonical example:

```yaml
steps:
  - agent_slug: refine
    optional: true
  - agent_slug: impl
  - agent_slug: test
  - agent_slug: commit-msg
    run_in_parallel_with: [title, oversight]
  - agent_slug: title
    run_in_parallel_with: [commit-msg, oversight]
  - agent_slug: oversight
    run_in_parallel_with: [commit-msg, title]
```

Each step in the parallel group lists the other members. The runner closes the group via transitive closure, so you don't need a dedicated group ID.

### `input_from` for chained prompts

A step with `input_from: <earlier-slug>` receives that earlier step's parsed output as its prompt. Example: refine-only-then-impl:

```yaml
steps:
  - agent_slug: refine
  - agent_slug: impl
    input_from: refine
```

Step `impl` runs with the text `refine` produced. If `input_from` is omitted, the step receives the task's original prompt.

### How flows route tasks

The runner's `Run` method resolves the task's flow slug, then:

- **`implement`** → the legacy turn loop in `execute.go`. This path is kept because the implement pipeline has multi-turn / session-recovery semantics the linear engine does not express.
- **`brainstorm`** (or legacy `Kind=idea-agent`) → the ideation fast path (`runIdeationTask`), which knows how to parse idea-agent output and create backlog tasks.
- **anything else** → the flow engine in `internal/flow/engine.go`. The engine walks steps, fans out parallel groups through an errgroup, and drives each step via `Runner.RunAgent(slug, task, prompt)`.

### On-disk format

```yaml
slug: tdd-loop
name: TDD Loop
description: Test first, then implement, then re-run tests.
steps:
  - agent_slug: test
  - agent_slug: impl
    input_from: test
  - agent_slug: test-again
```

Watched the same way as agents. Override the directory with `WALLFACER_FLOWS_DIR`.

---

## Tasks pick flows

### The composer Flow picker

The **+ New Task** form in the Backlog column has a **Flow** dropdown populated from `/api/flows`. Default is `implement`. Switch to any built-in or user-authored flow to run the task against that chain.

```
┌─────────────────────────────────────────┐
│  Flow: [Implement ▼]       [Templates]  │
├─────────────────────────────────────────┤
│  Describe the task...                   │
│                                         │
├─────────────────────────────────────────┤
│  Agent: Claude   Timeout: 1 hour        │
│  ...                                    │
└─────────────────────────────────────────┘
```

When the flow is `brainstorm`, the prompt field becomes optional (the ideate agent derives the topic from the workspace itself).

### What the task record stores

Every task carries a `flow_id` field. Legacy records written before the flow-data-model task (or via back-compat code paths) may instead carry `kind: "idea-agent"`, which resolves to `brainstorm` via the legacy-kind mapper. You don't need to migrate anything, the runner reads either.

### Switching a task's flow

Via the UI today you pick the flow at creation and cannot change it after. Via the API you can issue a `PATCH /api/tasks/{id}` but `flow_id` is immutable on a running task; cancel + recreate if you need a different flow.

---

## Routines spawn tasks against a flow

A **routine** is a board card (`Kind=routine`) with a schedule. When its timer fires, the routine spawns a fresh task against the flow you picked.

```
┌─────────────────────────────────────────┐
│  Routine: daily dependency check        │
│  every 24h   •   flow: refine-only      │
│  next fire: tomorrow 09:00              │
└─────────────────────────────────────────┘
```

The `spawn_flow` field replaces the legacy `spawn_kind`. Create one via the composer's "Repeat on a schedule" toggle or directly via `POST /api/routines`.

The runner uses the routine's flow on every fire, so changing the routine's `spawn_flow` affects all future instances. The ideation routine is the canonical example: it's a routine card tagged `system:ideation` with `spawn_flow: brainstorm`.

See [Routine Tasks](board-and-tasks.md#routine-tasks) for the routine lifecycle and API surface.

---

## Workspace defaults

The **Agents** tab header shows a one-line info row:

```
Workspace default harness: [claude]   Change
```

That value is the workspace-level default the resolver falls back to when an agent has `harness: ""`. It reads from `WALLFACER_DEFAULT_SANDBOX` and is editable in **Settings → Sandbox**.

The full resolver order, highest to lowest:

1. `agents.Role.Harness`, the agent descriptor's pin (if set).
2. Task-level sandbox override (set via `PATCH /api/tasks/{id}`).
3. Env-file per-activity override (`WALLFACER_SANDBOX_IMPLEMENTATION`, etc., legacy, still honoured).
4. `WALLFACER_DEFAULT_SANDBOX`.
5. Claude (hardcoded fallback).

In practice most installs only touch tier 1 (when cloning agents) and tier 4 (the workspace default).

---

## Recipes

### Pin testing to Codex

You want the `test` step to run on Codex for every task while everything else stays on Claude.

1. Agents tab → select `test` → Clone.
2. Set **Harness = Codex**, slug to something like `test-codex`.
3. Flows tab → select `implement` → Clone to `implement-codex-test`.
4. In the step editor, the second `test` step stays but swap the agent dropdown to `test-codex`.
5. Save. New tasks that pick `implement-codex-test` from the composer dropdown run test on Codex; everything else on Claude.

### Add a security-review step to every implement

1. Agents tab → **+ New Agent** with slug `security-review`, harness empty (inherit), a description, and a system prompt like "Review the diff for injection / auth / secrets issues and flag anything risky."
2. Flows tab → clone `implement` to `implement-with-security`.
3. Add a new step after `impl` and before `test`: select `security-review` in the dropdown.
4. Save. Your security reviewer runs between implementation and verification on every task using the new flow.

### Make a TDD flow

1. Flows tab → **+ New Flow** with slug `tdd`.
2. Add step `test` first (implicitly fails because the feature doesn't exist yet, that's the point).
3. Add step `impl` with `input_from: test` so the implementer gets the failing test output as context.
4. Add step `test` again as a second verification pass.
5. Save.

(Today the step editor doesn't dedupe agent slugs within a flow, if you need two `test` steps you can clone `test` to `test-after` and use both variants.)

### Custom commit messages for a subset of tasks

Cloning `commit-msg` with a custom `prompt_tmpl` and dropping it into a cloned flow works for tasks that route through the **flow engine** (any flow slug other than built-in `implement`). For tasks on the built-in `implement` flow, the turn loop calls `GenerateCommitMessage` directly with the built-in `commit_message` template regardless of clones, see the [System prompt runtime scope](#system-prompt) note above.

If you really want custom commit messages for every implement task, override the template body in **Settings → Prompt Templates → commit_message** (the template-override surface predates agents/flows but still works).

---

## Troubleshooting

**"My custom prompt isn't being used."** Check the runtime scope described above. If the task is on the built-in `implement` flow, sub-agents like title / commit-msg / oversight use their embedded templates and ignore `prompt_tmpl` on clones. Build a custom flow and use your clone there.

**"Save failed: slug is not kebab-case."** Slugs must be 2–40 characters, lowercase letters / digits / hyphens, no leading or trailing hyphen. `impl-v2` is fine; `ImplV2` or `-impl` are not.

**"Save failed: agent X is not registered."** A step in the flow references an agent slug that doesn't exist in the merged registry. Check the dropdown options or the Agents tab; if your intended agent isn't there, create it first.

**"Save failed: slug is a built-in."** You can't create a user agent or flow with a built-in slug. Pick a different slug. If you want to completely replace a built-in's behaviour, override the prompt template in Settings instead of trying to shadow the slug.

**"The editor's agent dropdown is empty."** The Agents tab hasn't hydrated yet. Open the Agents tab once to load the catalog, then return to Flows.

---

## Where the code lives

For contributors:

- `internal/agents/`, `Role` descriptor, built-in catalog (`builtins.go`), YAML store + watcher.
- `internal/flow/`, `Flow` / `Step` data model, built-in catalog, YAML store + watcher, `Engine` sequencer.
- `internal/runner/agent.go`, `runAgent` dispatch, `RunAgent` flow-engine adapter.
- `internal/runner/execute.go`, `Run` selects the dispatch path (implement turn loop vs brainstorm fast path vs flow engine).
- `internal/handler/agents.go` + `flows.go`, HTTP API surface.
- `ui/js/agents.js` + `flows.js` + CSS, the split-pane tabs.

The design spec and breakdown live at [`specs/local/agents-and-flows.md`](../../specs/local/agents-and-flows.md). The post-completion refinements section records every follow-up made after the initial ship.
