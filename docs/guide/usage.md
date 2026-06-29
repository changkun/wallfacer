# Usage Guide

Start here for the full reading order.

## Reading Order

<!-- NOTE: The server parses this section to build the in-app docs sidebar.
     Each "### " heading is a sidebar SECTION; every same-dir markdown link
     beneath it becomes an entry in that section, in order. To add a guide,
     drop a link under the right section heading. Keep example link syntax
     out of this comment: the parser scrapes any link it sees here. -->

### Get Started

- [Getting Started](getting-started.md). Installation, credentials, first run.
- [The Autonomy Spectrum](autonomy-spectrum.md). The mental model (chat, spec, task, code), how to move between levels and dial autonomy up or down.

### Use Wallfacer

- [Exploring Ideas](exploring-ideas.md). Agent chat, slash commands, @mentions, threads, the agent.
- [Designing Specs](designing-specs.md). Plan mode layout, spec lifecycle (six states), dispatch workflow, archive/unarchive, dependency minimap.
- [Agents & Flows](agents-and-flows.md). The four primitives (agent, flow, task, routine), how they compose, how to clone and customize built-ins, harness pinning, recipes.
- [Board & Tasks](board-and-tasks.md). Task board, lifecycle, dependencies, batch creation, search.
- [Prompt Refinement](refinement-and-ideation.md). Prompt refinement in the Plan task-mode chat.

### Operate

- [Automation](automation.md). Autoimplement, auto-test, auto-submit, auto-retry, circuit breakers.
- [Oversight & Analytics](oversight-and-analytics.md). Oversight summaries, costs, timeline, logs.
- [Workspaces](workspaces.md). Workspace management, git integration, branches, auto-push.
- [Configuration](configuration.md). Settings, env vars, harness selection, CLI, keyboard shortcuts.
- [Circuit Breakers](circuit-breakers.md). Fault isolation, self-healing automation, launch protection.

## Common Workflows

### Plan mode workflow

1. Sketch the idea in the agent chat (Plan mode, press **P**).
2. Issue `/create` (or let the agent emit `/spec-new`) to scaffold a first draft.
3. Refine with `/refine`, `/impact`, then `/validate`.
4. If the spec is too large, `/break-down` into sub-specs or leaf tasks.
5. When leaves are ready, `/dispatch` to push them to the board with dependency wiring.
6. After tasks finish, `/review-impl` and `/wrapup` to close out the parent.

### Agents & Flows

Customise what Wallfacer runs on your behalf. Built-ins are read-only, so clone first:

1. Clone a built-in agent (impl, test, commit-msg, title, oversight) and pin a harness (claude or codex) or tighten its system prompt.
2. Clone the built-in flow (implement) and reorder or swap its steps, for example add a security-review pass.
3. Author custom YAML under `~/.wallfacer/agents/` and `~/.wallfacer/flows/`. The clone is available immediately.

See [Agents & Flows](agents-and-flows.md) for the full recipes and runtime-scope caveats.

### Parallel feature development

Create multiple Backlog tasks, enable [Autoimplement](automation.md), and let Wallfacer run them concurrently. Each task works on a separate branch, so there are no conflicts during execution. Conflicts (if any) are resolved at merge time.

### Iterative refinement

1. Create a task and run it.
2. Review the diff and mark it Done if it looks right, or send feedback if it needs adjustment.
3. Continue the feedback loop until the result is satisfactory, then mark Done.

### Test-driven acceptance

1. Write a task prompt that includes clear acceptance criteria.
2. Run the task; when it reaches Waiting, click Test (see [Oversight & Analytics](oversight-and-analytics.md)).
3. If it fails, send feedback with the test output; re-run until passing.
4. Mark Done to commit.

### Fully automated pipeline

1. Enable Autoimplement + Auto-Test + Auto-Submit (see [Automation](automation.md)).
2. Create backlog tasks with dependencies (see [Board & Tasks](board-and-tasks.md)).
3. Tasks are automatically promoted, tested, and submitted as they complete.

## For Developers

Architecture details, the HTTP API reference, the task state machine, the execution model, and data models are documented in the [internals documentation](../internals/internals.md).
