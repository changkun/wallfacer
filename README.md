# Wallfacer

A work management board for autonomous AI agents. Create tasks as cards, drag them to execute in isolated sandbox containers, and collect results when the agents are done.

![](./images/overview-plain.png)

## Features

- **Kanban board** — visual task management with Backlog, In Progress, Waiting, Done, and Cancelled columns
- **Isolated sandbox execution** — each task runs in an ephemeral Podman/Docker container; tasks can run in parallel without interfering
- **Git worktree isolation** — every task gets its own git branch and worktree so concurrent changes never conflict
- **Branch switching** — switch or create branches from the UI; all future tasks branch from the new HEAD
- **Human-in-the-loop feedback** — when the agent needs clarification, the card moves to Waiting; submit feedback to resume execution
- **Prompt refinement** — chat with an AI assistant to iteratively improve a task description before running it
- **Test verification** — trigger a separate test agent on a waiting task to verify it meets acceptance criteria; records a pass/fail verdict
- **Autopilot mode** — automatically promotes backlog tasks to In Progress as capacity becomes available (configurable concurrency limit)
- **Auto commit and push** — completed task changes are automatically committed and pushed to the remote
- **Worktree sync** — rebase waiting/failed task worktrees onto the latest default branch without losing progress
- **Cross-task awareness** — each container receives a board manifest (`board.json`) so agents can see sibling tasks and avoid conflicts
- **Live log streaming** — real-time container output via Server-Sent Events
- **Task event timeline** — full audit trail of state changes, outputs, and feedback per task
- **Usage tracking** — input/output tokens, cache hits, and cost accumulated across all turns per task
- **Auto-generated titles** — task cards get short titles generated from the prompt
- **Workspace instructions** — per-workspace `CLAUDE.md` managed from the UI; shared across all tasks for that workspace
- **Multiple workspaces** — mount several project directories at once; agents can read and write across all of them
- **Diff viewer** — inspect exactly what changed in each task before accepting it
- **Container runtime auto-detection** — automatically finds Podman or Docker; both are fully supported
- **Configurable API** — set token, base URL, and model from the UI; supports OAuth tokens, direct API keys, and any Anthropic-compatible endpoint
- **Multiple agent runtimes** — bring your own sandbox image; built-in support for Claude Code and OpenAI Codex out of the box

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

Wallfacer was built in about a week of spare time. The idea came from running AI agents on everyday tasks. After a while, the workflow settled into writing task descriptions, running the agent, reviewing the output, and repeating. The main bottleneck was watching execution and managing all these tasks, so a Kanban board felt like a natural fit.

The first version was a Go server with a simple web UI. Tasks go into a backlog, get dragged to "in progress" to run an agent in a container, and move to "done" when finished. Git worktrees keep each task isolated so multiple can run at the same time without stepping on each other.

At some point Wallfacer was stable enough to develop itself — you can create a task card like "add retry logic," drag it to in progress, and let the agent implement the feature inside a Wallfacer sandbox. Most of the later features were built this way.

## License

See [LICENSE](LICENSE).
