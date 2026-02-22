# Architecture

Wallfacer is a Kanban task runner that executes Claude Code in isolated sandbox containers. Users create tasks on a web board; dragging a card from Backlog to In Progress triggers autonomous AI execution in an isolated git worktree, with auto-merge back to the main branch on completion.

## System Overview

```
┌─────────────────────────────────────────────────────────────┐
│  Browser (Vanilla JS + Tailwind + Sortable.js)              │
│  5-column Kanban board — SSE for live updates               │
└────────────────────────┬────────────────────────────────────┘
                         │ HTTP / SSE
┌────────────────────────▼────────────────────────────────────┐
│  Go Server (native on host)                                 │
│  main.go · handler.go · runner.go · store.go · git.go      │
└──────┬──────────────────────────────────────┬───────────────┘
       │ os/exec (podman/docker)              │ git commands
┌──────▼──────────────┐              ┌────────▼──────────────┐
│  Sandbox Container  │              │  Git Worktrees        │
│  Ubuntu 24.04       │              │  ~/.wallfacer/        │
│  Claude Code CLI    │◄────mount────│  worktrees/<uuid>/    │
└─────────────────────┘              └───────────────────────┘
```

The Go server runs natively on the host and persists tasks to per-task directories. It launches ephemeral sandbox containers via `podman run` (or `docker run`). Each task gets its own git worktree so multiple tasks can run concurrently without interfering.

## Technology Stack

**Backend** — Go 1.25, stdlib `net/http` (no framework), `os/exec` for containers, `sync.RWMutex` for concurrency, `github.com/google/uuid` for task IDs.

**Frontend** — Vanilla JavaScript, Tailwind CSS, Sortable.js, Marked.js. `EventSource` (SSE) for live updates, `localStorage` for theme preferences.

**Infrastructure** — Podman or Docker as container runtime. Ubuntu 24.04 sandbox image with Claude Code CLI installed. Git worktrees for per-task isolation.

**Persistence** — Filesystem only, no database. `~/.wallfacer/data/<uuid>/` per task. Atomic writes via temp file + `os.Rename`.

## Project Structure

```
wallfacer/
├── main.go              # CLI dispatch, HTTP routing, server init, browser launch
├── handler.go           # HTTP API handlers (CRUD, feedback, git, SSE)
├── runner.go            # Container orchestration, task execution loop, commit pipeline
├── store.go             # In-memory task store, event sourcing, atomic file I/O
├── git.go               # Git worktree operations, branch detection, rebase/merge
├── logger.go            # Structured logging (pretty-print + JSON)
│
├── internal/
│   ├── envconfig/       # .env file parsing and atomic update helpers
│   ├── handler/         # HTTP API handlers (one file per concern)
│   ├── instructions/    # Workspace CLAUDE.md management
│   ├── runner/          # Container orchestration, task execution, commit pipeline
│   └── store/           # In-memory task store, event sourcing, atomic file I/O
│
├── ui/
│   ├── index.html       # 5-column Kanban board layout
│   └── js/
│       ├── state.js     # Global state management
│       ├── api.js       # HTTP client & SSE stream setup
│       ├── tasks.js     # Task CRUD operations
│       ├── render.js    # Board rendering & DOM updates
│       ├── modal.js     # Task detail modal
│       ├── git.js       # Git status display
│       ├── dnd.js       # Drag-and-drop (Sortable.js)
│       ├── events.js    # Event timeline rendering
│       └── envconfig.js # API configuration editor (token, base URL, model)
│
├── sandbox/
│   ├── Dockerfile       # Ubuntu 24.04 + Go + Node + Python + Claude Code
│   └── entrypoint.sh    # Git config setup, Claude Code launcher
│
├── Makefile             # build, server, run, shell, clean targets
├── go.mod, go.sum
└── docs/                # Documentation
```

## Design Choices

| Choice | Rationale |
|---|---|
| Git worktrees per task | Full isolation; concurrent tasks don't interfere; Claude sees a clean branch |
| Goroutines, no queue | Simplicity; Go's scheduler handles parallelism; tasks are long-running and IO-bound |
| Filesystem persistence, no DB | Zero dependencies; atomic rename is crash-safe; human-readable for debugging |
| SSE, not WebSocket | Simpler server-side; one-directional push is all the UI needs |
| Ephemeral containers | No state leaks between tasks; each run starts clean |
| Event sourcing (traces/) | Full audit trail; enables crash recovery and replay |

## Configuration

### CLI Subcommands

- `wallfacer run [flags] [workspace ...]` — Start the Kanban server
- `wallfacer env` — Show configuration and env file status

Running `wallfacer` with no arguments prints help.

### Flags for `wallfacer run`

All flags have env var fallbacks:

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `-addr` | `ADDR` | `:8080` | Listen address |
| `-data` | `DATA_DIR` | `~/.wallfacer/data` | Data directory |
| `-container` | `CONTAINER_CMD` | `/opt/podman/bin/podman` | Container runtime command |
| `-image` | `SANDBOX_IMAGE` | `wallfacer:latest` | Sandbox container image |
| `-env-file` | `ENV_FILE` | `~/.wallfacer/.env` | Env file passed to containers |
| `-no-browser` | — | `false` | Do not open browser on start |

Positional arguments after flags are workspace directories to mount (defaults to current directory).

### Environment File

`~/.wallfacer/.env` is passed into every sandbox container via `--env-file`. The server also parses it to extract the model override.

At least one authentication variable must be set:

| Variable | Required | Description |
|---|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | one of these two | OAuth token from `claude setup-token` (Claude Pro/Max) |
| `ANTHROPIC_API_KEY` | one of these two | Direct API key from console.anthropic.com |
| `ANTHROPIC_BASE_URL` | no | Custom API endpoint; defaults to `https://api.anthropic.com` |
| `CLAUDE_CODE_MODEL` | no | Model passed as `--model` to every `claude` invocation; omit to use the Claude Code default |

When both `CLAUDE_CODE_OAUTH_TOKEN` and `ANTHROPIC_API_KEY` are set, the OAuth token takes precedence. This is Claude Code CLI behavior — wallfacer simply passes both variables through to the container via `--env-file`.

All four variables can be edited at runtime from **Settings → API Configuration** in the web UI. Changes take effect on the next task run without restarting the server.

`wallfacer env` reports the status of all four variables.

## Server Initialization

`main.go` → `runServer`:

```
parse CLI flags / env vars
→ load tasks from data/<uuid>/task.json into memory
→ create worktreesDir (~/.wallfacer/worktrees/)
→ pruneOrphanedWorktrees()   (removes stale worktree dirs + runs `git worktree prune`)
→ recover crashed tasks      (in_progress / committing → failed)
→ register HTTP routes
→ start listener on :8080
→ open browser (unless -no-browser)
```
