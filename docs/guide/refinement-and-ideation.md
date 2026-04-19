# Refinement and Ideation

Wallfacer includes two AI-powered features that help you work with tasks before they execute: **Prompt Refinement** lets you iterate on a task's prompt inside the Plan task-mode chat, and **Ideation** proposes entirely new task ideas by exploring your workspace for improvement opportunities. Ideation runs inside a read-only sandbox container and never modifies your code; refinement is a chat thread whose agent writes back to the task prompt through a bounded tool endpoint.

---

## Essentials

### Prompt Refinement (Plan task-mode)

Prompt iteration now happens inside the Plan panel, pinned to a specific task. The agent reads your workspaces, discusses the prompt with you in chat, and writes rewrites straight to the task's prompt field through the `update_task_prompt` tool. There is no separate "Refine" modal and no before/apply preview step: every round is a commit that you can undo.

Task-mode planning is available for tasks in the **Backlog** or **Waiting** columns. Once a task moves to In Progress, locking kicks in (see [Automation → Task lock & cascade](automation.md)) and the thread shows the live state without offering further writes.

#### Opening Plan for a Task

Two entry points open the same task-mode thread:

1. **From the card** — click the **Plan** action on a Backlog or Waiting card. The Plan panel opens with the task pinned, its current prompt rendered, and a fresh chat thread ready for input.
2. **From the explorer** — open the Plan panel (press **P**), switch the left pane to the **Task Prompts** section, and pick the task. The section lists every Backlog task in the active workspace group (and, optionally, Waiting tasks) as virtual entries; selecting one opens or reuses its task-mode thread.

If a thread for that task already exists, it is re-opened; otherwise a new one is created. Thread tabs are persistent across sessions.

#### Iterating in Chat

Type normally to talk to the agent. When you ask for a rewrite ("Make this more specific about error handling", "Merge the last two goals", …) the agent responds with the new prompt and calls `update_task_prompt`. The edit lands atomically on the task and the Spec panel updates immediately.

Each round is numbered (round 1, round 2, …). The tool call records the round number and thread ID so the undo pipeline can rewind by exactly one commit. The agent never invokes `update_task_prompt` without the user asking; you can keep the conversation open-ended (exploring the codebase, asking questions) without producing a rewrite.

#### Undoing a Round

Click **Undo** above the chat input, or use the task-mode undo shortcut. The server runs `git revert` against the commit produced by the latest `update_task_prompt` call for that thread:

- The task prompt rolls back to its previous value.
- The git history keeps both the original commit and the revert — there is no force-push.
- If the reverted commit carried a `dispatched_task_id` that referred to a task added in that round, those tasks are cancelled.
- A revert conflict aborts cleanly and surfaces as a 409; the working tree stays clean.

Undo is scoped per thread: undoing in thread A leaves thread B's rounds intact.

### Ideation / Brainstorm Agent

The ideation agent analyzes your workspace -- reading source files, project manifests, recent git history, churn hotspots, TODO/FIXME comments, and failed task signals -- and proposes up to three high-impact improvement ideas as new backlog cards.

#### How it works

Ideation is one instance of the generic **routine task** primitive. The server materializes a `system:ideation`-tagged routine card on first boot; its schedule and enabled flag live on the card itself, and the scheduler engine fires it to spawn instance tasks against the built-in `brainstorm` flow. Legacy records written before the flow rewrite carry `Kind = "idea-agent"` and dispatch identically via the legacy-kind mapper.

Two control surfaces exist, and both edit the same underlying routine:

- **Settings → Automation**: the legacy Brainstorm toggle and interval selector still work. Writes land on the system routine.
- **Board**: the routine card appears in the backlog column with inline controls (interval picker, enabled toggle, Run now, countdown). See the *Routine tasks* section in *Board & Tasks* for the full UI.

#### Enabling Ideation

Ideation is **disabled by default**. To enable it:

1. Click the **Automation** menu (lightning bolt icon) in the header bar.
2. Toggle the **Brainstorm** checkbox on.

Or edit the system:ideation routine card directly on the board.

Once enabled, you can trigger runs manually or configure an automatic interval.

#### Triggering a Brainstorm

Click the **Ideate** button in the header toolbar. This immediately fires the system-ideation routine, which creates a task against the `brainstorm` flow and starts the ideate agent in a sandbox container. The card appears in the In Progress column with a title like "Brainstorm Mar 21, 2026 14:30".

You can also trigger ideation via the API:

```
POST /api/ideate
```

#### What Ideation Produces

Each brainstorm run creates up to three backlog task cards. The agent uses a generate-then-rank pipeline:

1. **Generate** 6 candidate improvements across diverse areas (features, performance, security, code quality, architecture, and more).
2. **Self-critique** each candidate against concrete impact criteria.
3. **Output** the top 3 ranked ideas.

Each idea card includes:

- A short **display title** (the card's `Prompt` field, shown on the board)
- A detailed **execution prompt** (`ExecutionPrompt`, passed to the container when the task runs)
- **Tags**: `idea-agent` plus the idea's category, priority level, and impact score

Ideas that score below the minimum impact threshold are filtered out. Previously rejected ideas are remembered and excluded from future runs.

#### Reviewing Brainstorm Results

When Auto-Submit is **disabled** (the default), the brainstorm task card moves to Waiting after proposing ideas, allowing you to review the suggestions before they become backlog tasks. Open the card to see the summary, then:

- **Mark as Done** to approve the ideas and create backlog cards from them
- **Cancel** to discard the ideas

When Auto-Submit is **enabled**, backlog cards are created immediately without waiting for approval.

#### Canceling a Running Ideation

Cancel a brainstorm in one of two ways:

- Click the **Ideate** button again while a brainstorm is running
- Use the API: `DELETE /api/ideate`

This kills the container and marks the brainstorm instance task as cancelled. The routine card itself is unaffected and fires again on its next scheduled tick.

---

## Advanced Topics

### Plan task-mode thread lifecycle

Task-mode threads are distinct from the general planning threads. They are pinned to a specific task ID, rendered with a dedicated system prompt (`task_prompt_refine.tmpl`), and surface the task's current metadata (prompt, status, age) to the agent on every turn.

Thread tabs persist across sessions; archive a thread to hide it while keeping its files on disk. Creating a thread from a card that already has an active task-mode thread reuses the existing thread rather than spawning a duplicate.

When the pinned task transitions out of Backlog / Waiting (for example, you drag it to In Progress), the thread keeps rendering but the `update_task_prompt` tool refuses with a 422 — task movement locks the prompt until the task returns to an editable state.

### Ideation Sandbox Configuration

The ideation agent defaults to the Claude sandbox. Configure it globally with the `WALLFACER_SANDBOX_IDEA_AGENT` environment variable, or clone the `ideate` agent from the Agents tab and pin a `Harness`. If the Claude sandbox hits a token limit, the agent automatically retries with the Codex sandbox.

### System Prompt Customization

Ideation and task-mode planning use built-in system prompt templates:

| Template | Purpose |
|---|---|
| `task_prompt_refine.tmpl` | Instructs the task-mode planning agent how to iterate on a pinned task's prompt, including when to call `update_task_prompt`. |
| `ideation.tmpl` | Instructs the brainstorm agent to explore the workspace, generate candidates, self-critique, and output ranked ideas. |

To customize either template:

1. Open **Settings > System Prompts**.
2. Select the template you want to modify.
3. Edit the template text and save.

Your override replaces the built-in default for all future runs. To restore the original, delete your override from the same settings page. Overrides are validated for correct template syntax before saving.

You can also manage overrides via the API:

- `GET /api/system-prompts` -- list all templates with override status
- `PUT /api/system-prompts/{name}` -- write an override
- `DELETE /api/system-prompts/{name}` -- restore the embedded default

For the full HTTP API reference, see [API & Transport](../internals/api-and-transport.md).

### Automatic Ideation Interval

When ideation is enabled, you can set an automatic repeat interval so brainstorm runs happen periodically without manual intervention. Available intervals:

| Interval | Behavior |
|---|---|
| 0 (default) | Run immediately when the previous brainstorm completes |
| 15 min | Schedule the next run 15 minutes after the previous one finishes |
| 30 min, 1h, 2h, 4h, 8h, 24h | Correspondingly longer intervals |

Configure the interval from the Automation menu or Settings > Execution. When an interval is set and a brainstorm is not currently running, the header displays a countdown showing when the next run is scheduled (for example, "Next brainstorm in 23m").

### Configuration Variables

Sandbox routing for the ideation agent is controlled by `WALLFACER_SANDBOX_IDEA_AGENT` (inheriting from `WALLFACER_DEFAULT_SANDBOX`). Task-mode planning inherits the sandbox pinned on the surrounding planning thread. See [Configuration → Sandbox Routing](configuration.md#sandbox-routing) for the full routing table.

Automation toggles (set via `PUT /api/config`):

| Config Field | Description |
|---|---|
| `ideation` | Enable periodic brainstorm runs |
| `ideation_interval` | Minutes between brainstorm runs (0 = run immediately on completion) |

There is no longer an `autorefine` field: task-mode planning is user-driven and does not have an auto-promoted refine step.

### Planning Chat for Spec Iteration

For spec-level iteration (as opposed to task-level), see the Planning Chat in Plan Mode (press **P** to switch, then **C** for chat). The planning agent supports slash commands like `/summarize`, `/break-down`, `/create`, `/status`, `/validate`, `/impact`, and `/dispatch`.

---

## See Also

- [Usage Guide](usage.md) -- full board operations, task lifecycle, and automation overview
- [Getting Started](getting-started.md) -- initial setup and configuration
- [Circuit Breakers](circuit-breakers.md) -- how automation pauses on repeated failures
