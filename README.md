# Wallfacer

> Build software with a self-operating engineering team.

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/changkun/wallfacer?display_name=tag&logo=github)](https://github.com/changkun/wallfacer/releases)
[![License](https://img.shields.io/github/license/changkun/wallfacer)](./LICENSE)
[![Go Coverage](https://img.shields.io/codecov/c/github/changkun/wallfacer?label=go%20coverage&flag=go)](https://app.codecov.io/gh/changkun/wallfacer?flags[]=go)
[![JS Coverage](https://img.shields.io/codecov/c/github/changkun/wallfacer?label=js%20coverage&flag=javascript)](https://app.codecov.io/gh/changkun/wallfacer?flags[]=javascript)
[![Stars](https://img.shields.io/github/stars/changkun/wallfacer?style=social)](https://github.com/changkun/wallfacer/stargazers)
[![Last Commit](https://img.shields.io/github/last-commit/changkun/wallfacer)](https://github.com/changkun/wallfacer/commits/main)

Wallfacer is a task-board orchestration system for autonomous coding agents.  
Create tasks, run them in isolated sandboxes, review diffs, and keep shipping with minimal manual overhead.

![Wallfacer teaser](./images/overview.png)

## Why Wallfacer

- **Autonomous delivery loop**: backlog -> implementation -> testing -> review -> merge-ready output
- **Self-development capability**: wallfacer can run tasks that improve wallfacer itself
- **Isolation by default**: per-task containers and per-task git worktrees for safe parallelism
- **Operator visibility**: live logs, traces, timelines, and usage/cost tracking
- **Model/runtime flexibility**: support for Claude Code, Codex, and custom sandbox setups

## Capability Stack

- **Execution engine**: isolated containers, per-task git worktrees, safe parallel runs
- **Autonomous loop**: refinement, implementation, testing, auto-submit, autopilot promotion
- **Oversight layer**: live logs, timelines, traces, diff review, usage/cost visibility
- **Repo operations**: branch switching, sync/rebase helpers, auto commit and push
- **Flexible runtime**: Podman/Docker support, workspace-level instructions, Claude + Codex backends

For a complete walkthrough of workflows and controls, see [Usage Guide](docs/usage.md).

## Product Tour

### Mission Control Board

![Wallfacer board overview](./images/overview.png)

Coordinate many agent tasks in one place, move cards across the lifecycle, and keep execution throughput high without losing control.

### Oversight That Is Actually Actionable

**Execution oversight**

![Oversight view 1](./images/oversight1.png)

**Timeline and phase detail**

![Oversight view 2](./images/oversight2.png)

Inspect what happened, when it happened, and why it happened before you accept any automated output.

### Cost and Usage Visibility

![Usage and cost breakdown](./images/usage.png)

Track token usage and cost by task/activity so operations stay measurable as automation scales.

## Quick Start

```bash
# 1. Build the sandbox image (once)
make build

# 2. Build the binary
go build -o wallfacer .

# 3. Start with the project directories you want to work on
./wallfacer run ~/projects/myapp
```

On first launch, `~/.wallfacer/.env` is created. Edit it to add your Claude credential (OAuth token or API key), then restart. The browser opens to `http://localhost:8080`.

Codex can be enabled either by:
- host auth cache at `~/.codex/auth.json` (auto-detected at bootstrap), or
- `OPENAI_API_KEY` in `~/.wallfacer/.env` / **Settings → API Configuration** (plus one successful Codex test).

**See [Getting Started](docs/getting-started.md) for the full setup walkthrough**, including credential setup, configuration options, and troubleshooting.

### Common Commands

```bash
# Mount multiple workspaces
./wallfacer run ~/project1 ~/project2

# Custom port, skip auto-opening the browser
./wallfacer run -addr :9090 -no-browser ~/myapp

# Show configuration and env file status
./wallfacer env
```

### Make Targets

| Target | Description |
|---|---|
| `make build` | Build both sandbox images (Claude + Codex) |
| `make build-claude` | Build the Claude sandbox image only |
| `make build-codex` | Build the OpenAI Codex sandbox image only |
| `make server` | Build and run the Go server |
| `make run PROMPT="..."` | Headless one-shot agent execution |
| `make shell` | Debug shell inside a sandbox container |
| `make ui-css` | Regenerate Tailwind CSS from UI sources |
| `make clean` | Remove both sandbox images |

## Documentation

**User guides**

- [Getting Started](docs/getting-started.md) — installation, credentials, configuration reference, first run
- [Usage Guide](docs/usage.md) — creating tasks, handling feedback, autopilot, test verification, git branch management

**Internals**

- [Architecture](docs/internals/architecture.md) — system overview, tech stack, project structure
- [Task Lifecycle](docs/internals/task-lifecycle.md) — states, turn loop, feedback, data models, persistence
- [Git Worktrees](docs/internals/git-worktrees.md) — per-task isolation, commit pipeline, conflict resolution
- [Orchestration](docs/internals/orchestration.md) — API routes, container execution, SSE, concurrency

## Origin Story

Wallfacer started as a practical response to a repeated workflow: write a task prompt, run an agent, inspect output, and do it again. The bottleneck was not coding speed, it was coordination and visibility across many concurrent agent tasks. A task board became the control surface.

The first version was a Go server with a minimal web UI. Tasks moved from backlog to in progress, executed in isolated containers, and landed in done when complete. Git worktrees provided branch-level isolation so many tasks could run in parallel without collisions.

Wallfacer is now beyond simple task execution. It has become a fully automated engineering team: planning and refinement, implementation, test verification, commit and sync workflows, and continuous operation controls such as autopilot.

Just as importantly, automation is paired with oversight. Operators can inspect live logs, timelines, traces, diffs, and usage/cost signals before accepting results. The goal is not blind autonomy; it is high-throughput engineering with clear, auditable control.

Most recent capabilities were developed by wallfacer itself, creating a compounding loop where the system continuously improves its own engineering process.

## License

See [LICENSE](LICENSE).

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=changkun/wallfacer&type=Date)](https://star-history.com/#changkun/wallfacer&Date)
