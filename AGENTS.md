# AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Wallfacer is a task-board runner for Claude Code. It provides a web UI where tasks are created as cards, dragged to "In Progress" to trigger Claude Code execution in an isolated sandbox container, and results are inspected when done.

**Architecture:** Browser → Go server (:8080) → per-task directory storage (`data/<uuid>/`). The server runs natively on the host and launches ephemeral sandbox containers via `os/exec` (podman/docker). Each task gets its own git worktree for isolation.

For detailed documentation see `docs/`.

## Build & Run Commands

```bash
make build          # Build Go binary + Claude & Codex sandbox images
make build-binary   # Build just the Go binary
make build-claude   # Build the Claude Code sandbox image
make build-codex    # Build the OpenAI Codex sandbox image
make server         # Build and run the Go server natively
make shell          # Open bash shell in sandbox container for debugging
make clean          # Remove all sandbox images
make run PROMPT="…" # Headless one-shot Claude execution with a prompt
make test           # Run all tests (backend + frontend)
make test-backend   # Run Go unit tests (go test ./...)
make test-frontend  # Run frontend JS unit tests (npx vitest@2 run)
make ui-css         # Regenerate Tailwind CSS from UI sources
```

CLI usage (after `go build -o wallfacer .`):

```bash
wallfacer                                    # Print help
wallfacer run ~/project1 ~/project2          # Mount workspaces, open browser
wallfacer run                                # Defaults to current directory
wallfacer run -addr :9090 -no-browser        # Custom port, no browser
wallfacer env                                # Show config and env status
```

The Makefile uses Podman (`/opt/podman/bin/podman`) by default. Adjust `PODMAN` variable if using Docker.

## Server Development

The Go source lives at the top level. Module path: `changkun.de/wallfacer`. Go version: 1.25.7.

```bash
go build -o wallfacer .   # Build server binary
go vet ./...              # Lint
go test ./...             # Run backend tests
npx --yes vitest@2 run    # Run frontend tests
```

The server uses `net/http` stdlib routing (Go 1.22+ pattern syntax) with no framework.

Key server files:
- `main.go` — Subcommand dispatch, CLI flags, workspace resolution, HTTP routing, browser launch
- `server.go` — HTTP server setup, mux construction, route registration, container recovery
- `internal/handler/` — HTTP API handlers (one file per concern: tasks, env, config, git, instructions, containers, stream, execute, files, oversight, refine)
- `internal/runner/` — Container orchestration via `os/exec`; task execution loop; commit pipeline; usage tracking; worktree sync; title generation; oversight; refinement
- `internal/store/` — Per-task directory persistence, data models (Task, TaskUsage, TaskEvent, TaskOversight, RefinementJob), event sourcing
- `internal/envconfig/` — `.env` file parsing and atomic update; exposes `Parse` and `Update` for the handler and runner
- `internal/instructions/` — Workspace-level AGENTS.md management (`~/.wallfacer/instructions/`)
- `internal/gitutil/` — Git utility operations (ops, repo, status, stash, worktree)
- `internal/logger/` — Structured logging utilities
- `ui/index.html` + `ui/js/` — Task board UI (vanilla JS + Tailwind CSS + Sortable.js)

## API Routes

See `docs/internals/orchestration.md` for full details.

- `GET /` — Task board UI (embedded static files)
- `GET /api/config` — Server config (workspaces, instructions path)
- `PUT /api/config` — Update config
- `GET /api/containers` — List running containers
- `GET /api/files` — File listing for @ mention autocomplete
- `GET /api/tasks` — List all tasks
- `POST /api/tasks` — Create task (JSON: `{prompt, timeout}`)
- `PATCH /api/tasks/{id}` — Update status/position/prompt/timeout/fresh_start/model
- `DELETE /api/tasks/{id}` — Delete task
- `POST /api/tasks/{id}/feedback` — Submit feedback for waiting tasks
- `POST /api/tasks/{id}/done` — Mark waiting task as done (triggers commit-and-push)
- `POST /api/tasks/{id}/cancel` — Cancel task; discard worktrees; move to Cancelled
- `POST /api/tasks/{id}/resume` — Resume failed task with existing session
- `POST /api/tasks/{id}/sync` — Rebase task worktrees onto latest default branch
- `POST /api/tasks/{id}/test` — Run test verification on task worktrees
- `POST /api/tasks/{id}/refine` — Start prompt refinement via sandbox agent
- `DELETE /api/tasks/{id}/refine` — Cancel active refinement
- `GET /api/tasks/{id}/refine/logs` — Stream refinement container logs
- `POST /api/tasks/{id}/refine/apply` — Apply refined prompt to task
- `GET /api/tasks/{id}/oversight` — Get task oversight summary
- `GET /api/tasks/{id}/oversight/test` — Get test oversight summary
- `POST /api/tasks/{id}/archive` — Move done/cancelled task to archived
- `POST /api/tasks/{id}/unarchive` — Restore archived task
- `POST /api/tasks/archive-done` — Archive all done tasks
- `POST /api/tasks/generate-titles` — Auto-generate missing task titles
- `POST /api/tasks/generate-oversight` — Generate missing oversight summaries
- `GET /api/tasks/stream` — SSE: push task list on state change
- `GET /api/tasks/{id}/events` — Task event timeline
- `GET /api/tasks/{id}/diff` — Git diff for task worktrees vs default branch
- `GET /api/tasks/{id}/outputs/{filename}` — Raw Claude Code output per turn
- `GET /api/tasks/{id}/logs` — SSE: stream live container logs
- `GET /api/git/status` — Git status for all workspaces
- `GET /api/git/stream` — SSE: git status updates
- `POST /api/git/push` — Push a workspace
- `POST /api/git/sync` — Sync workspace
- `POST /api/git/rebase-on-main` — Rebase workspace onto main
- `GET /api/git/branches` — List git branches
- `POST /api/git/checkout` — Checkout a branch
- `POST /api/git/create-branch` — Create a new branch
- `GET /api/env` — Get env config (tokens masked)
- `PUT /api/env` — Update env config; omitted/empty token fields are preserved
- `GET /api/instructions` — Get workspace AGENTS.md content
- `PUT /api/instructions` — Save workspace AGENTS.md (JSON: `{content}`)
- `POST /api/instructions/reinit` — Rebuild workspace AGENTS.md from default + repo files

## Task Lifecycle

States: `backlog` → `in_progress` → `committing` → `done` | `waiting` | `failed` | `cancelled`

Tasks can also be marked `archived` (boolean flag on done/cancelled tasks, not a separate state).

See `docs/internals/task-lifecycle.md` for the full state machine, turn loop, and data models.

- Drag Backlog → In Progress triggers `runner.Run()` in a background goroutine
- Claude `end_turn` → commit pipeline (`committing` state) → Done
- Empty stop_reason → Waiting (needs user feedback)
- `max_tokens`/`pause_turn` → auto-continue in same session
- Feedback on Waiting → resumes execution
- "Mark as Done" on Waiting → Done + auto commit-and-push
- "Cancel" on Backlog/In Progress/Waiting/Failed → Cancelled; kills container, discards worktrees
- "Resume" on Failed → continues in existing session
- "Retry" on Failed/Done/Waiting/Cancelled → resets to Backlog (via PATCH with status change)
- "Sync" on Waiting/Failed → rebases worktrees onto latest default branch without merging
- "Test" on Waiting/Done/Failed → runs test verification agent on task worktrees
- Auto-promoter watches for capacity and promotes backlog tasks to in_progress

## Key Conventions

- **UUIDs** for all task IDs (auto-generated via `github.com/google/uuid`)
- **Event sourcing** via per-task trace files; types: `state_change`, `output`, `feedback`, `error`, `system`
- **Per-task directory storage** with atomic writes (temp file + rename); `sync.RWMutex` for concurrency
- **Git worktrees** per task for isolation; see `docs/internals/git-worktrees.md`
- **Usage tracking** accumulates input/output tokens, cache tokens, and cost across turns; per-sub-agent breakdown (implementation, test, refinement, title, oversight, oversight-test)
- **Container execution** creates ephemeral containers via `os/exec`; mounts worktrees under `/workspace/<basename>`
- **Workspace AGENTS.md** mounted read-only at `/workspace/AGENTS.md` so Claude Code picks it up automatically
- **Oversight summaries** generated asynchronously when tasks reach waiting/done/failed
- **Task refinement** via sandbox agent: refines prompts before execution
- **Frontend** uses SSE for live updates; escapes HTML to prevent XSS
- **No framework** on backend (stdlib `net/http`) or frontend (vanilla JS)

## Workspace AGENTS.md (Instructions)

Each unique combination of workspace directories gets its own `AGENTS.md` in `~/.wallfacer/instructions/`.
The file is identified by a SHA-256 fingerprint of the sorted workspace paths, so `wallfacer run ~/a ~/b` and `wallfacer run ~/b ~/a` share the same file.

On first run the file is created from:
1. A default wallfacer template (defined in `instructions.go`).
2. Any `AGENTS.md` found at the root of each workspace directory (appended in order).

Users can manually edit the file from **Settings → AGENTS.md → Edit** in the UI, or regenerate it from the repo files at any time with **Re-init**. The file is mounted read-only into every task container at `/workspace/AGENTS.md`.

## Configuration

See `docs/internals/architecture.md#configuration` for the full reference.

`~/.wallfacer/.env` must contain at least one of:
- `CLAUDE_CODE_OAUTH_TOKEN` — OAuth token from `claude setup-token`
- `ANTHROPIC_API_KEY` — direct API key from console.anthropic.com

Optional variables (also in `.env`):
- `ANTHROPIC_AUTH_TOKEN` — bearer token for LLM gateway proxy authentication
- `ANTHROPIC_BASE_URL` — custom API endpoint; when set, the server queries `{base_url}/v1/models` to populate the model dropdown
- `CLAUDE_DEFAULT_MODEL` — default model passed as `--model` to task containers
- `CLAUDE_TITLE_MODEL` — model for background title generation; falls back to `CLAUDE_DEFAULT_MODEL`
- `WALLFACER_MAX_PARALLEL` — maximum concurrent tasks for auto-promotion (default: 5)
- `WALLFACER_OVERSIGHT_INTERVAL` — minutes between periodic oversight generation while a task runs (0 = only at task completion, default: 0)
- `OPENAI_API_KEY` — API key for OpenAI Codex sandbox
- `OPENAI_BASE_URL` — custom OpenAI API endpoint
- `CODEX_DEFAULT_MODEL` — default model for Codex sandbox containers
- `CODEX_TITLE_MODEL` — model for Codex title generation

All can be edited from **Settings → API Configuration** in the UI (calls `PUT /api/env`).

## Commit and push strategy

- Keep commits small and focused on one logical change.
- Do not include unrelated changes in the same commit.
- Use scoped, imperative commit messages matching existing style, e.g. `internal/runner: ...`, `ui: ...`, `docs: ...`.
- Stage only the files required for that change, then commit once.
- Push only once after creating the commit.
- If follow-up work is needed, create a new small commit and push again.
