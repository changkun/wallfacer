# Wallfacer

> Build software with a self-operating engineering team.

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/changkun/wallfacer?display_name=tag&logo=github)](https://github.com/changkun/wallfacer/releases)
[![License](https://img.shields.io/github/license/changkun/wallfacer)](./LICENSE)
[![Coverage](https://codecov.io/gh/changkun/wallfacer/branch/main/graph/badge.svg)](https://app.codecov.io/gh/changkun/wallfacer)
[![Stars](https://img.shields.io/github/stars/changkun/wallfacer?style=social)](https://github.com/changkun/wallfacer/stargazers)
[![Last Commit](https://img.shields.io/github/last-commit/changkun/wallfacer)](https://github.com/changkun/wallfacer/commits/main)

Wallfacer is a self-operating engineering platform. It orchestrates autonomous coding agents across a task board, executes them in isolated sandboxes, and gives you full oversight over every decision they make — live logs, diffs, costs, and timelines — so you stay in control while shipping at machine speed.

![Wallfacer teaser](./images/overview.png)

## Quick Start

Install:

```bash
curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | sh
```

Check prerequisites:

```bash
wallfacer doctor
```

Start the server:

```bash
wallfacer run
```

A browser window opens automatically. Add your Claude credential (OAuth token via `claude setup-token`, or API key from [console.anthropic.com](https://console.anthropic.com/)) in **Settings → API Configuration**. See [Getting Started](docs/guide/getting-started.md) for the full setup walkthrough.

## Why Wallfacer

- **Autonomous delivery loop**: backlog, refinement, implementation, testing, review, merge-ready output — end to end
- **Self-development capability**: wallfacer runs tasks that improve wallfacer itself, creating a compounding engineering loop
- **Isolation by default**: per-task containers and per-task git worktrees for safe parallelism across many concurrent tasks
- **Full operator visibility**: live logs, traces, timelines, diffs, usage/cost tracking, per-turn token breakdown
- **Integrated development environment**: built-in file explorer, host terminal, workspace management — all in the browser
- **Model/runtime flexibility**: Claude Code, Codex, and custom sandbox setups with per-role routing

## Capability Stack

- **Execution engine**: isolated containers, per-task git worktrees, worker container reuse, safe parallel runs, circuit breaker, resource limits, dependency caching
- **Autonomous loop**: prompt refinement, implementation, testing, auto-submit, autopilot promotion, auto-retry, cost/token budgets, failure categorization
- **Oversight layer**: live logs, timelines, traces, diff review, usage/cost visibility, per-turn breakdown, task search, oversight summaries
- **Repo operations**: multi-workspace groups, branch switching, sync/rebase helpers, auto commit and push, task forking
- **Development tools**: file explorer with editor, interactive host terminal, prompt templates, system prompt customization
- **Flexible runtime**: Podman/Docker support, workspace-level AGENTS.md instructions, Claude + Codex backends, per-role sandbox routing

For a complete walkthrough of workflows and controls, see [Usage Guide](docs/guide/usage.md).
For implementation details and architecture, see [Technical Internals](docs/internals/internals.md).

## Product Tour

### Mission Control Board

![Wallfacer board overview](./images/overview.png)

Coordinate many agent tasks in one place, move cards across the lifecycle, and keep execution throughput high without losing control. Batch-create tasks with dependency wiring, refine prompts before execution, and let autopilot promote backlog items as capacity opens.

### Oversight That Is Actually Actionable

**Execution oversight**

![Oversight view 1](./images/oversight1.png)

**Timeline and phase detail**

![Oversight view 2](./images/oversight2.png)

Inspect what happened, when it happened, and why it happened before you accept any automated output. Every task produces a structured event timeline, diff against the default branch, and AI-generated oversight summary.

### Cost and Usage Visibility

![Usage and cost breakdown](./images/usage.png)

Track token usage and cost by task, activity, and turn so operations stay measurable as automation scales. Per-role breakdown (implementation, testing, refinement, oversight) shows exactly where budget goes.

## Roadmap

Development is organized into three parallel tracks with shared foundations. See [`specs/README.md`](specs/README.md) for the full dependency graph and spec index.

**Foundations** (complete) — Sandbox backend interface, storage backend interface, container reuse, file explorer, host terminal, multi-workspace groups, Windows support.

**Local Product** — Desktop experience and developer workflow: epic coordination, native desktop app, file/image attachments, host mounts, file panel viewer, terminal sessions, container exec, oversight risk scoring, visual verification, live serve.

**Cloud Platform** — Multi-tenant hosted service: tenant filesystem, K8s sandbox backend, cloud infrastructure, multi-tenant control plane, tenant API.

**Shared Design** — Cross-track specs: authentication, agent abstraction, native sandboxes (Linux/macOS/Windows), overlay snapshots.

## Documentation

**[User Manual](docs/guide/usage.md)** — start here for the full reading order.

| # | Guide | Topics |
|---|-------|--------|
| 1 | [Getting Started](docs/guide/getting-started.md) | Installation, credentials, first run |
| 2 | [Board & Tasks](docs/guide/board-and-tasks.md) | Kanban board, task lifecycle, dependencies, search |
| 3 | [Workspaces & Git](docs/guide/workspaces.md) | Workspace management, git integration, branches |
| 4 | [Automation](docs/guide/automation.md) | Autopilot, auto-test, auto-submit, auto-retry |
| 5 | [Refinement & Ideation](docs/guide/refinement-and-ideation.md) | Prompt refinement, brainstorm agent |
| 6 | [Oversight & Analytics](docs/guide/oversight-and-analytics.md) | Oversight summaries, costs, timeline |
| 7 | [Configuration](docs/guide/configuration.md) | Settings, env vars, sandboxes, CLI |
| 8 | [Circuit Breakers](docs/guide/circuit-breakers.md) | Fault isolation, self-healing automation |

**[Technical Internals](docs/internals/internals.md)** — start here for implementation details and architecture.

| # | Reference | Topics |
|---|-----------|--------|
| 1 | [Architecture](docs/internals/architecture.md) | System design, end-to-end walkthrough, concurrency model, where to look |
| 2 | [Data & Storage](docs/internals/data-and-storage.md) | Data models, persistence, event sourcing, search index |
| 3 | [Task Lifecycle](docs/internals/task-lifecycle.md) | State machine, turn loop, dependencies, board context |
| 4 | [Git Worktrees](docs/internals/git-worktrees.md) | Worktree lifecycle, commit pipeline, branch management |
| 5 | [API & Transport](docs/internals/api-and-transport.md) | HTTP routes, SSE, metrics, middleware |
| 6 | [Automation](docs/internals/automation.md) | Background watchers, autopilot, circuit breakers |
| 7 | [Workspaces & Config](docs/internals/workspaces-and-config.md) | Workspace manager, sandboxes, templates, env config |

## Origin Story

Wallfacer started as a practical response to a repeated workflow: write a task prompt, run an agent, inspect output, and do it again. The bottleneck was not coding speed — it was coordination and visibility across many concurrent agent tasks. A task board became the control surface.

The first version was a Go server with a minimal web UI. Tasks moved from backlog to in progress, executed in isolated containers, and landed in done when complete. Git worktrees provided branch-level isolation so many tasks could run in parallel without collisions.

Since then, Wallfacer has evolved into a self-operating engineering platform. The execution engine gained container reuse, circuit breakers, dependency caching, and multi-workspace groups. An autonomous loop handles prompt refinement, implementation, testing, auto-retry, and autopilot promotion. A full oversight layer — live logs, timelines, traces, diffs, and per-turn cost breakdown — ensures every agent decision is auditable before results are accepted.

The integrated development environment now includes a file explorer with editor, an interactive host terminal, system prompt customization, and prompt templates — all accessible from the browser. The goal is not blind autonomy; it is high-throughput engineering with clear, auditable control.

Most of Wallfacer's recent capabilities were developed by Wallfacer itself, creating a compounding loop where the system continuously improves its own engineering process.

## License

See [LICENSE](LICENSE).