# Usage Guide

## Board Overview

Wallfacer presents a four-column task board. Every task card moves through these columns as it progresses:

| Column | Meaning |
|---|---|
| **Backlog** | Queued, not yet started |
| **In Progress** | Container running; agent executing |
| **Waiting** | Agent paused, awaiting your feedback |
| **Done** | Completed; changes committed to your repo |

Archived tasks appear in the Done column when the "Show archived tasks" toggle is enabled in Settings.

## Creating Tasks

Click **+ New Task** in the toolbar, enter a description of what you want the agent to do, and click Add. The card appears in Backlog with an auto-generated short title. Each task card has a model/sandbox selector so you can override the default container image for that task.

### Batch Creation

Use `POST /api/tasks/batch` to create multiple tasks atomically. This endpoint supports symbolic dependency wiring — tasks in the batch can reference each other by position so that dependencies are wired up as part of the same atomic operation.

### Prompt Templates

Save frequently used prompt patterns as templates via **Settings → Prompt Templates** or the API (`GET/POST/DELETE /api/templates`). When creating a task, select a template to pre-fill the prompt.

### Refining Prompts

For complex tasks, sharpen the prompt before running it. Click the refine icon on a Backlog card to launch a sandbox agent that analyses your codebase and produces a detailed implementation spec. Stream the agent's output in real time. When it finishes, click **Apply** to replace the task prompt with the refined version, or **Dismiss** to discard it.

Prompt refinement is only available for Backlog tasks.

### Task Budgets

Set per-task cost and token limits to prevent runaway execution:

- **Max Cost (USD)** — the task is stopped when accumulated cost exceeds this threshold
- **Max Input Tokens** — the task is stopped when cumulative input+cache tokens exceed this limit

Set to 0 (default) for unlimited.

## Ideation

Ideation is disabled by default. Enable it from the **Automation** menu (lightning bolt icon) in the header. Once enabled, clicking **Ideate** launches the brainstorm agent. The agent analyses your workspace, identifies opportunities, and automatically creates backlog cards for each idea. Each generated card is tagged so you can identify and filter it. Cards created by ideation have a short display title (`Prompt`) and a more detailed `ExecutionPrompt` passed to the container at runtime. Cancel a running ideation session by clicking the button again.

## Running Tasks

### Manual Execution

Drag a card from **Backlog** to **In Progress**. Wallfacer:

1. Creates an isolated git branch (`task/<id>`) and a git worktree for each workspace
2. Launches a sandbox container with the agent
3. Streams live output to the task detail panel

Click a card to open the detail panel, which shows:

- Live log output as the agent works
- Token usage and estimated cost (broken down by sub-agent activity)
- Per-turn usage breakdown
- The git diff of the agent's changes so far
- **Oversight tab** — a high-level summary of what the agent did, organised into phases (e.g. "Reading codebase", "Implementing feature", "Running tests"). Each phase lists tools used, commands run, and key actions. The **Timeline** tab renders the same data as an interactive flamegraph.

### Autopilot

Enable **Autopilot** from the **Automation** menu (lightning bolt icon) in the header to automatically promote Backlog tasks to In Progress as capacity becomes available. The concurrency limit defaults to 5 and is controlled by `WALLFACER_MAX_PARALLEL` in your env file. Autopilot is off by default and resets to off on server restart.

### Auto-Test and Auto-Submit

- **Auto-Test** — automatically runs the test verification agent on tasks that reach Waiting
- **Auto-Submit** — automatically promotes verified waiting tasks to Done when conflict-free and up-to-date

Both are toggled via the **Automation** menu or `PUT /api/config`. Additional automation toggles include **Tip-sync** (auto-rebase waiting tasks) and **Auto-Refine** (auto-refine unrefined backlog tasks).

### Task Dependencies

Tasks can declare other tasks as prerequisites (`DependsOn`). Autopilot will not promote a task to In Progress until all of its dependencies have reached Done. The dependency graph panel visualises these relationships.

Backlog cards show a dependency badge when prerequisites are still unmet, and switch to a ready badge once all dependencies are satisfied. The task detail modal also includes a **Blocked by** section with live dependency status badges and links to each prerequisite task.

### Scheduled Execution

Set `ScheduledAt` on a task to delay auto-promotion until a specific time. The auto-promoter skips tasks whose scheduled time has not yet arrived.

### Auto-Retry

Tasks can have an `AutoRetryBudget` that maps failure categories (timeout, budget_exceeded, worktree_setup, container_crash, agent_error, sync_error) to retry counts. When a task fails, the runner checks the budget for that failure category and automatically retries if budget remains.

## Handling Waiting Tasks

When the agent needs clarification or is blocked, the card moves to **Waiting**. Open the task detail panel to see what it asked, then choose an action:

| Action | What it does |
|---|---|
| **Send feedback** | Type a reply and click Send. The agent resumes from where it paused with your message as the next input |
| **Mark done** | Skip any remaining agent turns and commit the current changes as-is |
| **Run test** | Launch a separate verification agent to check whether the work meets requirements (see below) |
| **Sync** | Rebase the task branch onto the latest default branch — useful when other tasks have merged since this one started |

| **Cancel** | Discard all changes and delete the task branch; execution history is preserved |


## Test Verification

From a **Waiting** task, click **Test** to launch a verification agent on the current state of the code. The agent:

1. Inspects the changes
2. Runs any relevant tests
3. Reports **PASS** or **FAIL**

You can optionally enter additional acceptance criteria before starting the run. The verdict appears as a badge on the card. Run tests multiple times — each run overwrites the previous verdict.

After reviewing the verdict:

- **PASS** — click **Mark Done** to commit the changes
- **FAIL** — provide feedback to guide the agent, then re-test

## Reviewing and Accepting Results

When a task reaches **Done**, open it to review what happened:

- **Diff view** — the exact file changes the agent made across all workspaces
- **Event timeline** — the full history of state changes, outputs, and feedback rounds
- **Usage** — input/output tokens, cache hits, and total cost accumulated across all turns, broken down by sub-agent activity
- **Per-turn usage** — detailed token consumption for each individual turn

After review, drag the card to **Archived** (or use **Archive All Done** from the toolbar) to move it off the active board. Archived tasks retain their full history.

## Soft Delete and Restore

Deleting a task creates a tombstone rather than immediately removing data. Soft-deleted tasks can be viewed via `GET /api/tasks/deleted` and restored via `POST /api/tasks/{id}/restore` within the retention window (`WALLFACER_TOMBSTONE_RETENTION_DAYS`, default 7 days). After the retention period, data is permanently pruned on the next server startup.

## Managing the Git Branch

Each task operates on an isolated branch (`task/<id>`). When a task reaches Done, Wallfacer:

1. Has the agent commit its changes
2. Rebases the task branch onto the current default branch
3. Fast-forward merges into the default branch
4. Deletes the task branch and worktree

If a rebase conflict occurs, Wallfacer invokes the agent again (same session, full context) to resolve it, then retries. Up to three attempts are made before the task is marked Failed.

### Branch Switching

The header bar shows the current branch for each workspace. Use the branch switcher dropdown to:

- **Switch branches** — select an existing branch; all future task worktrees will branch from the new HEAD
- **Create a branch** — type a new name in the search field and select **Create branch**

Both operations are blocked while tasks are in progress.

### Syncing Workspace

To rebase your current workspace branch onto the latest upstream, use the sync button in the header bar. This runs `git fetch` and `git rebase` on the workspace itself (not a task branch).

### Auto-Push

Auto-push is disabled by default. Enable it from Settings > Execution or via `WALLFACER_AUTO_PUSH=true` in the env file. `WALLFACER_AUTO_PUSH_THRESHOLD` controls the minimum number of completed tasks before a push is triggered.

## Workspace Management

Workspaces are managed via VS Code-style tabs in the header bar. Each tab represents a saved workspace group. Click a tab to switch to that group, use the + button to add a new group or restore a hidden one, and click the x on a tab to hide it from the bar. Groups are saved automatically when they become active.

### Workspace Browser

The `GET /api/workspaces/browse` endpoint lists child directories for a given path, making it easy to select workspaces from the UI.

## Workspace Instructions

Each workspace can have a `AGENTS.md` file that provides instructions to every agent running in that workspace. Open **Settings → Workspace Instructions** to edit this file directly from the UI. All tasks in the workspace share these instructions.

Use workspace instructions to set coding standards, preferred patterns, project context, or any constraints the agent should follow.

## System Prompt Templates

Wallfacer uses built-in prompt templates for background agents (title generation, commit messages, refinement, oversight, test verification, ideation, conflict resolution). These templates can be customized per-installation via **Settings → System Prompts** or the API:

- `GET /api/system-prompts` — list all templates with override status
- `PUT /api/system-prompts/{name}` — write a user override
- `DELETE /api/system-prompts/{name}` — restore the embedded default

Overrides are validated before saving to ensure template syntax is correct.

## Settings

Open **Settings** (gear icon) to access:

- **Appearance** — theme, archived task visibility
- **Execution** — parallel task limits, oversight interval, auto-push, brainstorm/ideation settings
- **Sandbox** — credentials, base URLs, model selection, sandbox routing, container resource limits, webhook settings
- **Workspace** — active workspaces, saved workspace groups, workspace instructions (AGENTS.md), system prompts, prompt templates
- **Trash** — view and restore soft-deleted tasks
- **About** — version and server information

Codex availability rules:
- If host Codex auth cache exists and is valid at `~/.codex/auth.json`, Codex is available automatically.
- Otherwise configure `OPENAI_API_KEY` and run **Test (Codex)** once in API Configuration.

**WALLFACER_OVERSIGHT_INTERVAL** controls how often (in minutes) the server generates intermediate oversight summaries while a task is running. Set to `0` (default) to generate only when the task completes.

## Search

Use the search bar (or `GET /api/tasks/search`) to find tasks by keyword across titles, prompts, tags, and oversight summaries. Results include the matched field and a context snippet.

## Webhooks

Configure `WALLFACER_WEBHOOK_URL` and optionally `WALLFACER_WEBHOOK_SECRET` to receive HTTP notifications on task state changes. Use `POST /api/env/test-webhook` to send a synthetic test event to verify your webhook endpoint.

## Keyboard Shortcuts and Tips

- Click any card to open its detail panel (diff, events, logs)
- The log stream in the detail panel updates in real time via Server-Sent Events
- Multiple tasks can run simultaneously; each operates on its own isolated branch and container
- Completed containers are automatically removed (`--rm`); no cleanup needed
- Use the search bar to filter visible cards by title, prompt text, or tag
- Use the command palette for quick access to actions

## Common Workflows

### Parallel feature development

Create multiple Backlog tasks, enable Autopilot, and let Wallfacer run them concurrently. Each task works on a separate branch, so there are no conflicts during execution. Conflicts (if any) are resolved at merge time.

### Iterative refinement

1. Create a task and run it
2. Review the diff and mark it as Done if it looks right, or provide feedback if it needs adjustment
3. Continue the feedback loop until the result is satisfactory, then mark Done

### Test-driven acceptance

1. Write a task prompt that includes clear acceptance criteria
2. Run the task; when it reaches Waiting, click Test
3. If it fails, send feedback with the test output; re-run until passing
4. Mark Done to commit


### Fully automated pipeline

1. Enable Autopilot + Auto-Test + Auto-Submit
2. Create backlog tasks with dependencies
3. Tasks are automatically promoted, tested, and submitted as they complete

## Circuit Breakers

Wallfacer includes circuit breakers that automatically pause automation when repeated failures are detected. Each automation watcher has its own breaker that self-heals via exponential backoff. See [Circuit Breakers](circuit-breakers.md) for details.

---

For setup instructions, see [Getting Started](getting-started.md).
For system internals, see the [internals documentation](internals/).
