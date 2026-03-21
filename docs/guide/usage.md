# User Manual

Wallfacer is a task-board runner for Claude Code. It provides a web-based kanban board where tasks are created as cards, executed by AI agents in isolated sandbox containers, and reviewed when complete.

## How to Read This Manual

The guides below are numbered in recommended reading order. Each guide has an **Essentials** section covering the basics you need to get productive, and an **Advanced Topics** section for power users. Start from Guide 1 and work forward. You can skip Advanced sections on first read and return to them when you need deeper control.

## Reading Order

### 1. Getting Started

[Getting Started](getting-started.md)

Install Wallfacer, configure your API credentials, and launch the task board for the first time. **Essentials** — install, configure, first run.

### 2. Board & Tasks

[Board & Tasks](board-and-tasks.md)

The core experience: create tasks, run them, and review results on the kanban board.

- **Essentials:** Creating tasks, running them, reviewing diffs, handling waiting tasks
- **Advanced:** Batch creation, dependencies, scheduled execution, budgets, soft delete

### 3. Workspaces & Git

[Workspaces & Git](workspaces.md)

Where your code lives and how changes flow back to your repositories.

- **Essentials:** Setting up workspaces, basic git operations (push, sync, branches)
- **Advanced:** Workspace groups, worktree internals, commit pipeline, conflict resolution, AGENTS.md instructions

### 4. Automation

[Automation](automation.md)

Hands-off operation: let Wallfacer promote, test, and submit tasks without manual intervention.

- **Essentials:** Enabling autopilot, auto-test, auto-submit
- **Advanced:** Pipeline architecture, auto-retry, circuit breakers, dependency ordering, scheduled execution

### 5. Refinement & Ideation

[Refinement & Ideation](refinement-and-ideation.md)

AI-assisted prompt improvement and idea generation before tasks are executed.

- **Essentials:** Refining a prompt, running the ideation agent
- **Advanced:** Auto-refine, ideation intervals, system prompt customization

### 6. Oversight & Analytics

[Oversight & Analytics](oversight-and-analytics.md)

Understanding what agents did and what it cost.

- **Essentials:** Reading oversight summaries, checking costs, live log monitoring
- **Advanced:** Flamegraph/timeline, span statistics, budget enforcement, stats dashboard

### 7. Configuration

[Configuration](configuration.md)

Deep reference for all settings and customization options.

- **Essentials:** Settings UI walkthrough, key environment variables
- **Advanced:** System prompt templates, sandbox routing, webhooks, security, CLI reference

### 8. Circuit Breakers

[Circuit Breakers](circuit-breakers.md)

Fault isolation and self-healing automation. **Advanced only** — circuit breakers automatically pause promotion, testing, and submission when repeated failures are detected, then self-heal via exponential backoff.

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
