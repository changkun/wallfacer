# Usage Guide

## Board Overview

Wallfacer presents a five-column Kanban board. Every task card moves through these columns as it progresses:

| Column | Meaning |
|---|---|
| **Backlog** | Queued, not yet started |
| **In Progress** | Container running; agent executing |
| **Waiting** | Agent paused, awaiting your feedback |
| **Done** | Completed; changes committed to your repo |
| **Archived** | Done or cancelled tasks moved off the active board (tasks with `archived=true`) |

## Creating Tasks

Click **+ New Task** in the toolbar, enter a description of what you want the agent to do, and click **Add**. The card appears in Backlog with an auto-generated short title. Each task card has a model/sandbox selector so you can override the default container image for that task.

### Refining Prompts

For complex tasks, sharpen the prompt before running it. Click the refine icon on a Backlog card to launch a sandbox agent that analyses your codebase and produces a detailed implementation spec. Stream the agent's output in real time. When it finishes, click **Apply** to replace the task prompt with the refined version, or **Dismiss** to discard it.

Prompt refinement is only available for Backlog tasks.

## Ideation

Click the **Ideate** button (lightbulb icon) in the toolbar to launch the brainstorm agent. The agent analyses your workspace, identifies opportunities, and automatically creates backlog cards for each idea. Each generated card is tagged so you can identify and filter it. Cards created by ideation have a short display title (`Prompt`) and a more detailed `ExecutionPrompt` passed to the container at runtime. Cancel a running ideation session by clicking the button again.

## Running Tasks

### Manual Execution

Drag a card from **Backlog** to **In Progress**. Wallfacer:

1. Creates an isolated git branch (`task/<id>`) and a git worktree for each workspace
2. Launches a sandbox container with the agent
3. Streams live output to the task detail panel

Click a card to open the detail panel, which shows:

- Live log output as the agent works
- Token usage and estimated cost (broken down by sub-agent activity)
- The git diff of the agent's changes so far
- **Oversight tab** — a high-level summary of what the agent did, organised into phases (e.g. "Reading codebase", "Implementing feature", "Running tests"). Each phase lists tools used, commands run, and key actions. The **Timeline** tab renders the same data as an interactive flamegraph.

### Autopilot

Enable **Autopilot** from the toolbar to automatically promote Backlog tasks to In Progress as capacity becomes available. The concurrency limit defaults to 5 and is controlled by `WALLFACER_MAX_PARALLEL` in your env file. Autopilot is off by default and resets to off on server restart.

### Task Dependencies

Tasks can declare other tasks as prerequisites (`DependsOn`). Autopilot will not promote a task to In Progress until all of its dependencies have reached Done. The dependency graph panel visualises these relationships.

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
- **Usage** — input/output tokens, cache hits, and total cost accumulated across all turns

After review, drag the card to **Archived** (or use **Archive All Done** from the toolbar) to move it off the active board. Archived tasks retain their full history.

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

## Workspace Instructions

Each workspace can have a `CLAUDE.md` file that provides instructions to every agent running in that workspace. Open **Settings → Workspace Instructions** to edit this file directly from the UI. All tasks in the workspace share these instructions.

Use workspace instructions to set coding standards, preferred patterns, project context, or any constraints the agent should follow.

## Settings

Open **Settings** (gear icon) to access:

- **API Configuration** — credential, base URL, model selection, concurrency limit; changes take effect on the next task run without restarting
- **Workspace Instructions** — the `CLAUDE.md` content for each workspace

**WALLFACER_OVERSIGHT_INTERVAL** controls how often (in minutes) the server generates intermediate oversight summaries while a task is running. Set to `0` (default) to generate only when the task completes.

## Keyboard Shortcuts and Tips

- Click any card to open its detail panel (diff, events, logs)
- The log stream in the detail panel updates in real time via Server-Sent Events
- Multiple tasks can run simultaneously; each operates on its own isolated branch and container
- Completed containers are automatically removed (`--rm`); no cleanup needed
- Use the search bar to filter visible cards by title, prompt text, or tag

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

---

For setup instructions, see [Getting Started](getting-started.md).
For system internals, see the [internals documentation](internals/).
