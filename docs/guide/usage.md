# Usage Guide

Start here for the full reading order.

## Reading Order

<!-- NOTE: The server parses this section to build the in-app docs sidebar.
     Each "### " heading is a sidebar SECTION; every same-dir markdown link
     beneath it becomes an entry in that section, in order. To add a guide,
     drop a link under the right section heading. Keep example link syntax
     out of this comment: the parser scrapes any link it sees here.
     frontend/scripts/gen-docs-index.mjs derives the cloud docs index from
     this same section; run it after editing (see the docs-rewrite spec). -->

### Get Started

- [Getting Started](getting-started.md). Installation, credentials, sign-in, first task end to end.
- [Concepts](concepts.md). The mental model: workspaces, tasks, specs, agents, and the autonomy spectrum.

### Use Wallfacer

- [Board](board.md). The task board: lifecycle, dependencies, batch creation, search, the task detail view.
- [Chat](chat.md). The dedicated chat surface: sessions, slash commands, @mentions.
- [Plan](plan.md). Spec mode: the spec tree, lifecycle states, dispatch, and planning conversations.
- [Agent Graph](agent-graph.md). Defining agents, composing fleets, harness pinning, live traces.
- [Routines](routines.md). Scheduled cards that spawn fresh tasks on an interval.
- [Whiteboard](whiteboard.md). The free-form drawing canvas.

### Operate

- [Automation](automation.md). Autoimplement, auto-test, auto-submit, auto-retry, circuit breakers.
- [Oversight](oversight.md). Oversight summaries, timelines, logs, diff review, cost and usage analytics.
- [Mission Control](mission-control.md). The unified spec and task graph, acting on the pipeline.
- [Workspaces](workspaces.md). Workspace management, git integration, branches, GitHub.
- [Configuration](configuration.md). Settings, environment variables, harness selection, CLI reference, keyboard shortcuts.

## Common Workflows

### Plan mode workflow

1. Sketch the idea in the agent chat (Plan mode, press **P**).
2. Issue `/create` to scaffold a first draft.
3. Refine with `/refine`, `/impact`, then `/validate`.
4. If the spec is too large, `/break-down` into sub-specs or leaf tasks.
5. When leaves are ready, `/dispatch` to push them to the board with dependency wiring.
6. After tasks finish, `/review-impl` and `/wrapup` to close out the parent.

### Parallel feature development

Create multiple Backlog tasks, enable [Autoimplement](automation.md), and let Wallfacer run them concurrently. Each task works in its own git worktree, so there are no conflicts during execution. Conflicts (if any) are resolved at merge time.

### Iterative refinement

1. Create a task and run it.
2. Review the diff and mark it Done if it looks right, or send feedback if it needs adjustment.
3. Continue the feedback loop until the result is satisfactory, then mark Done.

### Test-driven acceptance

1. Write a task prompt that includes clear acceptance criteria.
2. Run the task; when it reaches Waiting, click Test (see [Oversight](oversight.md)).
3. If it fails, send feedback with the test output; re-run until passing.
4. Mark Done to commit.

### Fully automated pipeline

1. Enable Autoimplement, Auto-Test, and Auto-Submit (see [Automation](automation.md)).
2. Create backlog tasks with dependencies (see [Board](board.md)).
3. Tasks are automatically promoted, tested, and submitted as they complete.

## For Developers

Architecture details, the HTTP API reference, the task state machine, the execution model, and data models are documented in the [internals documentation](../internals/internals.md).
