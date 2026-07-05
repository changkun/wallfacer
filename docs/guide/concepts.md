# Concepts

Most AI coding tools pin work to one interaction mode: a chatbot that needs constant steering, or a fire-and-forget agent that surfaces hours later with unpredictable results. Wallfacer offers a spectrum between those extremes. Work happens at four levels, and moving between them is free in both directions.

## The four working levels

### Chat: conversational exploration

The [Chat](chat.md) page runs a persistent agent session against the active workspace. Describe a problem, explore trade-offs, and let the agent read code and answer questions before anything is committed to a plan. Sessions stream live, survive reloads, and can be reattached or retried. This is the entry point when the shape of the work is still unknown.

### Plan: structured design

On the [Plan](plan.md) page, ideas crystallize into specs: structured markdown documents with lifecycle states, effort estimates, and dependencies. Agents iterate on design rather than code, driven by slash commands (`/create`, `/refine`, `/validate`, `/break-down`, `/dispatch`, and others). The output is a blueprint, not a diff. A validated spec dispatches to the board as a task with one command.

### Board: managed execution

The [Board](board.md) is where most day-to-day work happens. Each card is a concrete, trackable task. Starting a task runs the built-in Implement pipeline in an isolated worktree; results wait for review before landing. Diff comments, verification runs, and pull requests all attach to the card.

### Autopilot: automation

With the [automation toggles](automation.md) enabled, the board runs itself: backlog tasks promote as capacity opens, finished work is tested automatically, passing work is submitted, and commits are pushed. [Routines](routines.md) add a schedule, firing recurring tasks without anyone at the keyboard. Attention shifts from managing tasks to monitoring outcomes through [oversight and analytics](oversight.md) and [Mission Control](mission-control.md).

The levels are access points, not a pipeline. Start a task directly when the fix is obvious; start in chat when it is not. A failing automated task can be pulled down to manual review, debugged in the built-in terminal, and resumed with feedback.

## The primitives

### Workspace

A workspace is a named set of project folders with a stable identity. The identity survives renames and folder changes, so task history stays attached when the folder set evolves. Each workspace can carry its own parallelism cap. Workspaces are created, switched, and edited from the sidebar switcher; see [Workspaces](workspaces.md).

### Task

A task is a card on the board: a prompt, an optional title, a harness selection, and dependencies. Tasks move through seven lifecycle states:

| State | Meaning |
|---|---|
| `backlog` | Created, not yet started |
| `in progress` | An agent is working in the task's worktree |
| `waiting` | Work finished, awaiting review or verification |
| `committing` | Accepted, the commit message agent is writing the commit |
| `done` | Committed and complete |
| `failed` | Stopped with a failure category (timeout, agent error, worktree setup, and others) |
| `cancelled` | Abandoned; can be revived to backlog |

Transitions are enforced by a state machine: backlog starts into in progress; in progress can pause back to backlog, finish into waiting, fail, or be cancelled; waiting resumes, commits, or cancels; committing lands in done or failed; failed retries to backlog. Failed tasks carry a category that automation uses to decide retry eligibility.

### Spec

A spec is a design document with a lifecycle of its own: `vague`, `drafted`, `validated`, `testing`, `complete`, plus the off-axis states `stale` (reality has drifted from the design) and `archived`. The main axis runs vague to drafted to validated; a validated spec cannot jump straight to complete, it must pass through testing. By default, completion of the dispatched task marks the spec complete directly; with the experimental drift tester enabled (`WALLFACER_DRIFT_TESTER`), an assessment agent compares the implementation against the spec first and classifies the result as complete or stale. Specs carry an effort estimate (small, medium, large, xlarge) and form a dependency graph. See [Plan](plan.md).

### Agent roles

Five built-in agent roles cover the task pipeline:

- **Implementation**: the multi-turn coding agent with write access to the worktree.
- **Testing**: verifies finished work and produces a pass/fail verdict.
- **Title**: names the task from its prompt.
- **Oversight**: writes a review summary of what the agent did.
- **Commit message**: writes the final commit.

Custom agents can be defined, cloned, given custom system prompts, and pinned to a specific harness on the [Agent Graph](agent-graph.md) page.

### Fleet and agent graph

The Agent Graph page is the composition surface: it defines agents and wires them into flows. One built-in flow exists, **Implement**: implementation, then testing, then a parallel finishing step (commit message, title, oversight). Every task runs a flow; unknown or legacy flow names resolve to Implement. Custom flows built on the canvas can be selected per task or per routine.

An experimental in-process execution path (Topos) can run agent graphs natively without spawning a subprocess harness. It is opt-in, currently API-key only, and does not support session resume; treat it as a preview. See [Agent Graph](agent-graph.md).

### Routine

A routine is a scheduled card: a prompt, an interval, and an agent graph to spawn. The routine itself stays in the backlog and is excluded from automation and archiving; each time it fires, it spawns a fresh instance task that runs the chosen flow. See [Routines](routines.md).

### Harness

A harness is the coding CLI that executes an agent turn. Five subprocess harnesses are supported: **Claude** (default), **Codex**, **Cursor**, **OpenCode**, and **Pi**, plus the experimental in-process **Topos**. Harness selection is layered: a task-level or per-activity setting wins over the per-activity environment override, which wins over the global default, which falls back to Claude. Agent definitions can also pin a harness. Credentials and routing are covered in [Configuration](configuration.md#harness-tab).

### Worktree isolation

Every task runs in its own git worktree under `~/.wallfacer/worktrees`, on a branch named `task/<id>` (the first eight characters of the task id). The worktree is what contains the work: an agent can edit freely without touching the main checkout, and the diff stays reviewable until accepted. Worktrees are garbage collected after tasks finish, and a health watcher prunes orphans. Non-git folders fall back to snapshot-based diffs without branch isolation.

## The autonomy dial

Automation is not a single switch but a set of composable toggles, reachable from the board header and **Settings > Execution**:

- **Autoimplement** promotes backlog tasks as capacity opens, respecting dependencies and schedules.
- **Autotest** runs the testing agent on finished work.
- **Autosubmit** accepts work whose verification passed.
- **Autosync** rebases waiting tasks onto the default branch as it moves.
- **Autopush** pushes accepted commits to the remote.

A sixth, separate toggle enables **Agon**, an adversarial verification mode where critic forks debate the change before it is accepted. Enable any subset: autotest without autosubmit means automatic verification with manual acceptance; everything on means the board runs itself. Details and guard rails (retry budgets, circuit breakers, parallelism caps) are in [Automation](automation.md).

## Moving between levels

Work flows up and down the spectrum:

- **Up (more autonomy):** a chat conversation produces a spec; the spec is broken into tasks; tasks execute and verify automatically.
- **Down (more control):** a failing task surfaces a problem; inspect the diff, open the terminal in the worktree, fix the issue by hand, and let the agent continue from the corrected state.

The value of the spectrum is spending attention where it matters and delegating the rest.

## See also

- [Getting Started](getting-started.md): installation and the first task
- [Board](board.md), [Chat](chat.md), [Plan](plan.md): the three main working surfaces
- [Agent Graph](agent-graph.md): custom agents and flows
- [Automation](automation.md) and [Routines](routines.md): the autopilot level
- [Configuration](configuration.md): settings, environment variables, shortcuts
- [Architecture](../internals/architecture.md): how the pieces fit together internally
