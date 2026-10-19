# Concepts

Most AI coding tools pin work to one interaction mode: a chatbot that needs constant steering, or a fire-and-forget agent that surfaces hours later with unpredictable results. Wallfacer offers a spectrum between those extremes. Work happens at three levels, and moving between them is free in both directions.

## The three working levels

### Chat: conversational exploration

The chat runs a persistent agent session against the active workspace, from the chat popup on any page or from [Plan](plan.md), which opens as a full chat when the workspace has no specs yet. Describe a problem, explore trade-offs, and let the agent read code and answer questions before anything is committed to a plan. Sessions stream live, survive reloads, and can be reattached or retried. This is the entry point when the shape of the work is still unknown.

### Plan: structured design

On the [Plan](plan.md) page, ideas crystallize into specs: structured markdown documents with lifecycle states, effort estimates, and dependencies. Agents iterate on design rather than code, driven by slash commands (`/create`, `/refine`, `/validate`, `/break-down`, `/dispatch`, and others). The output is a blueprint, not a diff. A validated spec dispatches to the board as a task with one command.

### Board: managed execution

The [Board](board.md) is where most day-to-day work happens. Each card is a concrete, trackable task. Starting a task runs the built-in Implement pipeline in an isolated worktree; results wait for review before landing. Diff comments and reviews attach to the card.

The levels are access points, not a pipeline. Start a task directly when the fix is obvious; start in chat when it is not. A failing task can be inspected and resumed with feedback.

## The primitives

### Workspace

A workspace is a named project folder with a stable identity. The identity survives renames and re-pointing to another folder, so task history stays attached when the folder changes. Work across several repositories uses one workspace per repository. Each workspace can carry its own parallelism cap. Workspaces are created, switched, and edited from the sidebar switcher; see [Workspaces](workspaces.md).

### Task

A task is a card on the board: a prompt, an optional title, and a harness selection. Tasks move through seven lifecycle states:

| State | Meaning |
|---|---|
| `backlog` | Created, not yet started |
| `in progress` | An agent is working in the task's worktree |
| `waiting` | Work finished, awaiting review |
| `committing` | Accepted, the commit message agent is writing the commit |
| `done` | Committed and complete |
| `failed` | Stopped with a failure category (timeout, agent error, worktree setup, and others) |
| `cancelled` | Abandoned; can be revived to backlog |

Transitions are enforced by a state machine: backlog starts into in progress; in progress can pause back to backlog, finish into waiting, fail, or be cancelled; waiting resumes, commits, or cancels; committing lands in done or failed; failed retries to backlog. Failed tasks carry a category that names why they stopped.

### Spec

A spec is a design document with a lifecycle of its own: `vague`, `drafted`, `validated`, `testing`, `complete`, plus the off-axis states `stale` (reality has drifted from the design) and `archived`. The main axis runs vague to drafted to validated; a validated spec cannot jump straight to complete, it must pass through testing. By default, completion of the dispatched task marks the spec complete directly; with the experimental drift tester enabled (`WALLFACER_DRIFT_TESTER`), an assessment agent compares the implementation against the spec first and classifies the result as complete or stale. Specs carry an effort estimate (small, medium, large, xlarge) and form a dependency graph. See [Plan](plan.md).

### Agent roles

Four built-in agent roles cover the task pipeline:

- **Implementation**: the multi-turn coding agent with write access to the worktree.
- **Oversight**: writes a review summary of what the agent did.
- **Commit message**: writes the final commit.
- **Review**: checks the change on a second model when Review is on.

Every task runs the same built-in pipeline: implementation, then a parallel finishing step (commit message, oversight). A task's title is the first line of its prompt. The roles are fixed; a task's instructions come from the repository's instruction files and the task prompt. Each role's harness can be chosen through the per-activity settings described under [Harness](#harness).

### Harness

A harness is the coding CLI that executes an agent turn. Five harnesses are supported: **Claude** (default), **Codex**, **Cursor**, **OpenCode**, and **Pi**. Harness selection is layered: a task-level or per-activity setting wins over the per-activity environment override, which wins over the global default, which falls back to Claude. Credentials and routing are covered in [Configuration](configuration.md#harness-tab).

### Worktree isolation

Every task runs in its own git worktree under `~/.wallfacer/worktrees`, on a branch named `task/<id>` (the first eight characters of the task id). The worktree is what contains the work: an agent can edit freely without touching the main checkout, and the diff stays reviewable until accepted. Worktrees are garbage collected after tasks finish, and a health watcher prunes orphans. Non-git folders fall back to snapshot-based diffs without branch isolation.

## Moving between levels

Work flows up and down the spectrum:

- **Up (more autonomy):** a chat conversation produces a spec; the spec is broken into tasks; tasks execute in their own worktrees.
- **Down (more control):** a failing task surfaces a problem; inspect the diff, fix the issue by hand in the task's worktree, and let the agent continue from the corrected state.

The value of the spectrum is spending attention where it matters and delegating the rest.

## See also

- [Getting Started](getting-started.md): installation and the first task
- [Board](board.md) and [Plan](plan.md): the two main working surfaces
- [Configuration](configuration.md): settings and environment variables
- [Architecture](../internals/architecture.md): how the pieces fit together internally
