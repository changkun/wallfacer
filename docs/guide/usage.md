# Usage Guide

Start here for the full reading order.

## Reading Order

<!-- NOTE: The server parses this section to build the docs sidebar.
     Each markdown link under a heading becomes a sidebar entry.
     To add a new guide: add a new ### heading with a link below. -->

### Getting Started
[Getting Started](getting-started.md) — Installation, credentials, first run.

### The Autonomy Spectrum
[The Autonomy Spectrum](autonomy-spectrum.md) — The mental model: chat, spec, task, code. How to move between levels and dial autonomy up or down.

### Exploring Ideas
[Exploring Ideas](exploring-ideas.md) — Planning chat, slash commands, @mentions, conversation persistence.

### Designing Specs
[Designing Specs](designing-specs.md) — Plan mode, focused view, dependency minimap, spec lifecycle.

### Executing Tasks
[Board & Tasks](board-and-tasks.md) — Task board, lifecycle, dependencies, batch creation, search.

### Automation & Control
[Automation](automation.md) — Autopilot, auto-test, auto-submit, auto-retry, circuit breakers.

### Oversight & Analytics
[Oversight & Analytics](oversight-and-analytics.md) — Oversight summaries, costs, timeline, logs.

### Workspaces & Git
[Workspaces](workspaces.md) — Workspace management, git integration, branches, auto-push.

### Refinement & Ideation
[Refinement & Ideation](refinement-and-ideation.md) — Prompt refinement agent, brainstorm agent, auto-refine.

### Configuration
[Configuration](configuration.md) — Settings, env vars, sandboxes, CLI, keyboard shortcuts.

### Circuit Breakers
[Circuit Breakers](circuit-breakers.md) — Fault isolation, self-healing automation, container launch protection.

## Common Workflows

### Parallel feature development

Create multiple Backlog tasks, enable [Autopilot](automation.md), and let Wallfacer run them concurrently. Each task works on a separate branch, so there are no conflicts during execution. Conflicts (if any) are resolved at merge time.

### Iterative refinement

1. Create a task and run it
2. Review the diff and mark it as Done if it looks right, or provide feedback if it needs adjustment
3. Continue the feedback loop until the result is satisfactory, then mark Done

### Test-driven acceptance

1. Write a task prompt that includes clear acceptance criteria
2. Run the task; when it reaches Waiting, click Test (see [Oversight & Analytics](oversight-and-analytics.md))
3. If it fails, send feedback with the test output; re-run until passing
4. Mark Done to commit

### Fully automated pipeline

1. Enable Autopilot + Auto-Test + Auto-Submit (see [Automation](automation.md))
2. Create backlog tasks with dependencies (see [Board & Tasks](board-and-tasks.md))
3. Tasks are automatically promoted, tested, and submitted as they complete

## For Developers

Architecture details, the HTTP API reference, task state machine, container orchestration, and data models are documented in the [internals documentation](../internals/).
