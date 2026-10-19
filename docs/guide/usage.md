# Usage Guide

Start here for the full reading order.

## Reading Order

### Get Started

- [Getting Started](getting-started.md). Installation, credentials, first task end to end.
- [Concepts](concepts.md). The mental model: workspaces, tasks, specs, agents, and the working levels.

### Use Wallfacer

- [Board](board.md). The task board: lifecycle, search, the task detail view.
- [Plan](plan.md). Spec mode: the spec tree, lifecycle states, dispatch, and planning conversations.

### Operate

- [Oversight](oversight.md). Oversight summaries, timelines, logs, diff review, per-task cost and usage.
- [Workspaces](workspaces.md). Workspace management, git integration, branches.
- [Configuration](configuration.md). Settings, environment variables, harness selection, CLI reference.

## Common Workflows

### Plan mode workflow

1. Sketch the idea in the agent chat (Plan mode).
2. Issue `/create` to scaffold a first draft.
3. Refine with `/refine`, `/impact`, then `/validate`.
4. If the spec is too large, `/break-down` into sub-specs or leaf tasks.
5. When leaves are ready, `/dispatch` to push them to the board.
6. After tasks finish, `/review-impl` and `/wrapup` to close out the parent.

### Parallel feature development

Create multiple Backlog tasks and start each one; Wallfacer runs them concurrently up to the parallel limit (see [Workspaces](workspaces.md)). Each task works in its own git worktree, so there are no conflicts during execution. Conflicts (if any) are resolved at merge time.

### Iterative refinement

1. Create a task and run it.
2. Review the diff and mark it Done if it looks right, or send feedback if it needs adjustment.
3. Continue the feedback loop until the result is satisfactory, then mark Done.

## For Developers

Architecture details, the HTTP API reference, the task state machine, the execution model, and data models are documented in the [internals documentation](../internals/internals.md).
