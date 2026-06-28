# Prompt Refinement

Wallfacer lets a task's prompt be shaped before it executes. **Prompt Refinement** iterates on a task's prompt inside the Plan task-mode chat: a chat thread whose agent writes back to the task prompt through a bounded tool endpoint.

Recurring idea generation is no longer a built-in agent. To explore a repository for improvements on a schedule, create an ordinary routine on the [Routines](board-and-tasks.md) page with a prompt such as "Review the repository for the three highest-impact improvements and create a task for each." The routine creator offers this as a one-click example.

---

## Essentials

### Prompt Refinement (Plan task-mode)

Prompt iteration happens inside the Plan panel, pinned to a specific task. The agent reads the workspaces, discusses the prompt in chat, and writes rewrites straight to the task's prompt field through the `update_task_prompt` tool. There is no separate "Refine" modal and no before/apply preview step: every round is a commit that can be undone.

Task-mode chat is available for tasks in the **Backlog** or **Waiting** columns. Once a task moves to In Progress, locking kicks in (see [Automation, Task lock and cascade](automation.md)) and the thread shows the live state without offering further writes.

#### Opening Plan for a Task

Two entry points open the same task-mode thread:

1. **From the card**: click the **Plan** action on a Backlog or Waiting card. The Plan panel opens with the task pinned, its current prompt rendered, and a fresh chat thread ready for input.
2. **From the explorer**: open the Plan panel (press **P**), switch the left pane to the **Task Prompts** section, and pick the task. The section lists every Backlog task in the active workspace group (and, optionally, Waiting tasks) as virtual entries; selecting one opens or reuses its task-mode thread.

If a thread for that task already exists, it is re-opened; otherwise a new one is created. Thread tabs are persistent across sessions.

#### Iterating in Chat

Type normally to talk to the agent. When the request is a rewrite ("Make this more specific about error handling", "Merge the last two goals", and so on), the agent responds with the new prompt and calls `update_task_prompt`. The edit lands atomically on the task and the Spec panel updates immediately.

Each round is numbered (round 1, round 2, and so on). The tool call records the round number and thread ID so the undo pipeline can rewind by exactly one commit. The agent never invokes `update_task_prompt` unprompted; the conversation can stay open-ended (exploring the codebase, asking questions) without producing a rewrite.

#### Undoing a Round

Click **Undo** above the chat input, or use the task-mode undo shortcut. The server runs `git revert` against the commit produced by the latest `update_task_prompt` call for that thread:

- The task prompt rolls back to its previous value.
- The git history keeps both the original commit and the revert: there is no force-push.
- If the reverted commit carried a `dispatched_task_id` that referred to a task added in that round, those tasks are cancelled.
- A revert conflict aborts cleanly and surfaces as a 409; the working tree stays clean.

Undo is scoped per thread: undoing in thread A leaves thread B's rounds intact.

---

## Advanced Topics

### Plan task-mode thread lifecycle

Task-mode threads are distinct from the general threads. They are pinned to a specific task ID, rendered with a dedicated system prompt (`task_prompt_refine.tmpl`), and surface the task's current metadata (prompt, status, age) to the agent on every turn.

Thread tabs persist across sessions; archive a thread to hide it while keeping its files on disk. Creating a thread from a card that already has an active task-mode thread reuses the existing thread rather than spawning a duplicate.

When the pinned task transitions out of Backlog / Waiting (for example, dragging it to In Progress), the thread keeps rendering but the `update_task_prompt` tool refuses with a 422: task movement locks the prompt until the task returns to an editable state.

### System Prompt Customization

Task-mode chat uses a built-in system prompt template:

| Template | Purpose |
|---|---|
| `task_prompt_refine.tmpl` | Instructs the task-mode agent how to iterate on a pinned task's prompt, including when to call `update_task_prompt`. |

To customize the template:

1. Open **Settings > System Prompts**.
2. Select the template to modify.
3. Edit the template text and save.

An override replaces the built-in default for all future runs. To restore the original, delete the override from the same settings page. Overrides are validated for correct template syntax before saving.

Overrides can also be managed via the API:

- `GET /api/system-prompts` lists all templates with override status
- `PUT /api/system-prompts/{name}` writes an override
- `DELETE /api/system-prompts/{name}` restores the embedded default

For the full HTTP API reference, see [API and Transport](../internals/api-and-transport.md).

### Configuration Notes

Task-mode chat inherits the harness pinned on the surrounding thread. There is no `autorefine` config field: task-mode chat is user-driven and does not have an auto-promoted refine step.

### Agent Chat for Spec Iteration

For spec-level iteration (as opposed to task-level), see the Agent Chat in Plan Mode (press **P** to switch, then **C** for chat). The agent supports slash commands like `/summarize`, `/break-down`, `/create`, `/status`, `/validate`, `/impact`, and `/dispatch`.

---

## See Also

- [Usage Guide](usage.md) for full board operations, task lifecycle, and automation overview
- [Getting Started](getting-started.md) for initial setup and configuration
- [Circuit Breakers](circuit-breakers.md) for how automation pauses on repeated failures
</content>
</invoke>
