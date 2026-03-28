This file provides guidance when working with code in this repository.

## Project Overview

Wallfacer is a task-board runner for AI agents. It provides a web UI where tasks are created as cards, dragged to "In Progress" to trigger AI agent execution in an isolated sandbox container, and results are inspected when done.

**Architecture:** Browser → Go server (:8080) → per-task directory storage (`data/<uuid>/`). The server runs natively on the host and launches ephemeral sandbox containers via `os/exec` (podman/docker). Each task gets its own git worktree for isolation.

For detailed documentation see `docs/`. The user manual is at `docs/guide/usage.md` and the technical internals index is at `docs/internals/internals.md`.

## Specs & Roadmap

Design specs live in `specs/`, numbered by milestone order. The roadmap and dependency graph are in [`specs/README.md`](specs/README.md). When creating or modifying a spec:

- **Read `specs/README.md` first** to understand the milestone ordering and dependency graph.
- **Number new specs** to reflect their position: main milestones use `NN-name.md` (e.g., `01-`, `02-`), branch-outs use `NNx-name.md` (e.g., `01a-`, `02a-`), and independent enhancements use `9N-name.md`.
- **Assess cross-impacts** with existing milestones. If a new spec modifies interfaces defined in M1 (`SandboxBackend`) or M2 (`StorageBackend`), it must declare the dependency and be placed after those milestones.
- **Update `specs/README.md`** when adding a spec — add it to the appropriate milestone table, branch-out section, or independent enhancements list.

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
make test-frontend  # Run frontend JS unit tests (cd ui && npx vitest@2 run)
make ui-css         # Regenerate Tailwind CSS from UI sources
make api-contract   # Regenerate API route artifacts from apicontract/routes.go
```

CLI usage (after `go build -o wallfacer .`):

```bash
wallfacer                                    # Print help
wallfacer run                                # Start server, restore last workspace group
wallfacer run -addr :9090 -no-browser        # Custom port, no browser
wallfacer doctor                             # Check prerequisites and config
wallfacer status                             # Print board state to terminal
wallfacer status -watch                      # Live-updating board state
wallfacer exec <task-id-prefix>              # Attach to running task container
wallfacer exec --sandbox claude              # Open shell in a new sandbox
```

The Makefile uses Podman (`/opt/podman/bin/podman`) by default. Adjust `PODMAN` variable if using Docker.

## Server Development

The Go source lives at the top level. Module path: `changkun.de/x/wallfacer`. Go version: 1.25.7.

```bash
go build -o wallfacer .   # Build server binary
go vet ./...              # Lint
go test ./...             # Run backend tests
cd ui && npx --yes vitest@2 run    # Run frontend tests
```

The server uses `net/http` stdlib routing (Go 1.22+ pattern syntax) with no framework.

Key server files:
- `main.go` — Tiny entry point: embed FS declarations and subcommand dispatch
- `internal/cli/` — CLI subcommand implementations (server, exec, status, env) and shared helpers
- `internal/apicontract/` — Single source of truth for all HTTP API routes; generates `ui/js/generated/routes.js`
- `internal/handler/` — HTTP API handlers (one file per concern: tasks, env, config, git, instructions, containers, stream, execute, files, oversight, refine, ideate, templates, stats, admin, workspace, runtime)
- `internal/runner/` — Container orchestration via `os/exec`; task execution loop; commit pipeline; usage tracking; worktree sync; title generation; oversight; refinement; ideation; auto-retry; circuit breaker
- `internal/store/` — Per-task directory persistence, data models (Task, TaskUsage, TurnUsageRecord, TaskEvent, TaskOversight, TaskSummary, Tombstone, RetryRecord, FailureCategory), event sourcing, soft delete, search index; see `docs/internals/data-and-storage.md` for full data model documentation
- `internal/envconfig/` — `.env` file parsing and atomic update; exposes `Parse` and `Update` for the handler and runner
- `internal/gitutil/` — Git utility operations (ops, repo, status, stash, worktree)
- `internal/workspace/` — Workspace manager; scopes data by workspace key; supports runtime workspace switching with concurrent multi-group execution (stores stay alive while tasks are running)
- `internal/logger/` — Structured logging utilities
- `internal/metrics/` — Prometheus-compatible metrics
- `internal/constants/` — Consolidated system parameters: timeouts, intervals, retry counts, size limits, concurrency caps, pagination defaults
- `internal/sandbox/` — Sandbox type enumeration (Claude, Codex); Windows host path translation for container mounts
- `prompts/` — System prompt templates (title, commit, refinement, oversight, test, ideation, conflict, instructions) and workspace-level AGENTS.md management (`~/.wallfacer/instructions/`)
- `ui/index.html` + `ui/js/` — Task board UI (vanilla JS + Tailwind CSS + Sortable.js)

## API Routes

All routes are defined in `internal/apicontract/routes.go`. See `docs/internals/api-and-transport.md` for full details.

### Debug & Monitoring
- `GET /api/debug/health` — Operational health check
- `GET /api/debug/spans` — Aggregate span timing statistics
- `GET /api/debug/runtime` — Live server internals (goroutines, memory, task states, containers)
- `GET /api/debug/board` — Board manifest as seen by a hypothetical new task

### Configuration
- `GET /api/config` — Server config (workspaces, autopilot flags, sandbox list)
- `PUT /api/config` — Update config (autopilot, autotest, autosubmit, sandbox assignments)
- `GET /api/env` — Get env config (tokens masked)
- `PUT /api/env` — Update env config; omitted/empty token fields are preserved
- `POST /api/env/test` — Validate sandbox credentials via test container

### Workspace Management
- `GET /api/workspaces/browse` — List child directories for an absolute host path
- `PUT /api/workspaces` — Replace the active workspace set and switch task board

### Instructions
- `GET /api/instructions` — Get workspace AGENTS.md content
- `PUT /api/instructions` — Save workspace AGENTS.md (JSON: `{content}`)
- `POST /api/instructions/reinit` — Rebuild workspace AGENTS.md from default + repo files

### System Prompt Templates
- `GET /api/system-prompts` — List all built-in system prompt templates with override status
- `GET /api/system-prompts/{name}` — Get a single system prompt template
- `PUT /api/system-prompts/{name}` — Write a user override for a built-in template
- `DELETE /api/system-prompts/{name}` — Remove override, restoring the embedded default

### Prompt Templates
- `GET /api/templates` — List all prompt templates
- `POST /api/templates` — Create a new named prompt template
- `DELETE /api/templates/{id}` — Delete a prompt template

### Tasks
- `GET /api/tasks` — List all tasks (optionally including archived)
- `POST /api/tasks` — Create task (JSON: `{prompt, goal, timeout}`)
- `POST /api/tasks/batch` — Create multiple tasks atomically with symbolic dependency wiring
- `PATCH /api/tasks/{id}` — Update status/position/prompt/goal/timeout/sandbox/dependencies/fresh_start
- `DELETE /api/tasks/{id}` — Soft-delete task (tombstone); data retained within retention window
- `POST /api/tasks/{id}/feedback` — Submit feedback for waiting tasks
- `POST /api/tasks/{id}/done` — Mark waiting task as done (triggers commit-and-push)
- `POST /api/tasks/{id}/cancel` — Cancel task; discard worktrees; move to Cancelled
- `POST /api/tasks/{id}/resume` — Resume failed task with existing session
- `POST /api/tasks/{id}/restore` — Restore a soft-deleted task by removing its tombstone
- `POST /api/tasks/{id}/sync` — Rebase task worktrees onto latest default branch
- `POST /api/tasks/{id}/test` — Run test verification on task worktrees

- `POST /api/tasks/{id}/archive` — Move done/cancelled task to archived
- `POST /api/tasks/{id}/unarchive` — Restore archived task
- `POST /api/tasks/archive-done` — Archive all done tasks
- `POST /api/tasks/generate-titles` — Auto-generate missing task titles
- `POST /api/tasks/generate-oversight` — Generate missing oversight summaries
- `GET /api/tasks/search` — Search tasks by keyword
- `GET /api/tasks/summaries` — Immutable task summaries for completed tasks (cost dashboard)
- `GET /api/tasks/deleted` — List soft-deleted (tombstoned) tasks within retention window
- `GET /api/tasks/stream` — SSE: push task list on state change
- `GET /api/tasks/{id}/events` — Task event timeline (supports cursor pagination)
- `GET /api/tasks/{id}/diff` — Git diff for task worktrees vs default branch
- `GET /api/tasks/{id}/outputs/{filename}` — Raw Claude Code output per turn
- `GET /api/tasks/{id}/logs` — SSE: stream live container logs
- `GET /api/tasks/{id}/turn-usage` — Per-turn token usage breakdown
- `GET /api/tasks/{id}/spans` — Span timing statistics for a task
- `GET /api/tasks/{id}/oversight` — Get task oversight summary
- `GET /api/tasks/{id}/oversight/test` — Get test oversight summary
- `GET /api/tasks/{id}/board` — Board manifest as it appeared to a specific task

### Git
- `GET /api/git/status` — Git status for all workspaces
- `GET /api/git/stream` — SSE: git status updates
- `POST /api/git/push` — Push a workspace
- `POST /api/git/sync` — Sync workspace
- `POST /api/git/rebase-on-main` — Rebase workspace onto main
- `GET /api/git/branches` — List git branches
- `POST /api/git/checkout` — Checkout a branch
- `POST /api/git/create-branch` — Create a new branch
- `POST /api/git/open-folder` — Open workspace directory in the OS file manager

### Refinement
- `POST /api/tasks/{id}/refine` — Start prompt refinement via sandbox agent
- `DELETE /api/tasks/{id}/refine` — Cancel active refinement
- `GET /api/tasks/{id}/refine/logs` — Stream refinement container logs
- `POST /api/tasks/{id}/refine/apply` — Apply refined prompt to task
- `POST /api/tasks/{id}/refine/dismiss` — Dismiss refinement result without applying

### Ideation
- `GET /api/ideate` — Get current ideation session state
- `POST /api/ideate` — Launch brainstorm/ideation agent
- `DELETE /api/ideate` — Cancel running ideation agent

### Usage & Statistics
- `GET /api/usage` — Aggregated token and cost usage statistics
- `GET /api/stats` — Task status and workspace cost statistics
- `GET /api/containers` — List running containers
- `GET /api/files` — File listing for @ mention autocomplete

### Sandbox Images
- `GET /api/images` — Check which sandbox images are cached locally
- `POST /api/images/pull` — Start async pull for a sandbox image
- `DELETE /api/images` — Remove a cached sandbox image
- `GET /api/images/pull/stream` — SSE stream of pull progress

### File Explorer
- `GET /api/explorer/tree` — List one level of a workspace directory

### Admin
- `POST /api/admin/rebuild-index` — Rebuild the in-memory search index from disk

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
- "Cancel" on Backlog/In Progress/Waiting/Failed/Done → Cancelled; kills container, discards worktrees
- "Resume" on Failed → continues in existing session
- "Retry" on Failed/Done/Waiting/Cancelled → resets to Backlog (via PATCH with status change)
- "Sync" on Waiting/Failed → rebases worktrees onto latest default branch without merging
- "Test" on Waiting/Done/Failed → runs test verification agent on task worktrees

- Auto-promoter watches for capacity and promotes backlog tasks to in_progress
- Auto-retry automatically retries failed tasks based on failure category and budget

## Key Conventions

- **UUIDs** for all task IDs (auto-generated via `github.com/google/uuid`)
- **Event sourcing** via per-task trace files; types: `state_change`, `output`, `feedback`, `error`, `system`, `span_start`, `span_end`
- **Per-task directory storage** with atomic writes (temp file + rename); `sync.RWMutex` for concurrency
- **Soft delete** via tombstone files; `DELETE /api/tasks/{id}` writes a tombstone, data pruned after retention window (`WALLFACER_TOMBSTONE_RETENTION_DAYS`, default 7)
- **Git worktrees** per task for isolation; see `docs/internals/git-worktrees.md`
- **Usage tracking** accumulates input/output tokens, cache tokens, and cost across turns; per-sub-agent breakdown (implementation, test, refinement, title, oversight, oversight-test, commit_message, idea_agent); per-turn records available via `/api/tasks/{id}/turn-usage`
- **Container execution** creates ephemeral containers via `os/exec`; mounts worktrees under `/workspace/<basename>`
- **Container resource limits** configurable via `WALLFACER_CONTAINER_CPUS` and `WALLFACER_CONTAINER_MEMORY`
- **Workspace AGENTS.md** mounted read-only at `/workspace/AGENTS.md` so Claude Code picks it up automatically
- **Oversight summaries** generated asynchronously when tasks reach waiting/done/failed
- **Task refinement** via sandbox agent: refines prompts before execution
- **System prompt templates** are overridable built-in prompts (`prompts/*.tmpl`); users can customize via the UI or API; includes the workspace instructions template
- **Prompt templates** for reusable task creation patterns

- **Auto-retry** with per-failure-category budget; failed tasks can be automatically retried
- **Cost/token budgets** via `MaxCostUSD` and `MaxInputTokens` per task
- **Failure categorization** classifies failures (timeout, budget_exceeded, worktree_setup, container_crash, agent_error, sync_error, unknown)
- **Execution environment recording** captures container image, model, API base URL, and instructions hash for reproducibility
- **Frontend** uses SSE for live updates; escapes HTML to prevent XSS
- **No framework** on backend (stdlib `net/http`) or frontend (vanilla JS)
- **Server API key** authentication via `WALLFACER_SERVER_API_KEY`
- **Circuit breaker** for container launches (`WALLFACER_CONTAINER_CB_THRESHOLD`)

## Workspace AGENTS.md (Instructions)

Each unique combination of workspace directories gets its own `AGENTS.md` in `~/.wallfacer/instructions/`.
The file is identified by a SHA-256 fingerprint of the sorted workspace paths, so switching to workspaces `~/a` and `~/b` (in any order) shares the same file.

On first run the file is created from:
1. The `instructions.tmpl` template in `prompts/`, rendered via the prompt Manager (overridable like other system prompts).
2. A reference list of per-repo `AGENTS.md` (or `CLAUDE.md`) paths so agents can read them on demand.

Users can manually edit the file from **Settings → AGENTS.md → Edit** in the UI, or regenerate it from the repo files at any time with **Re-init**. The file is mounted read-only into every task container at `/workspace/AGENTS.md`.

## Configuration

See `docs/getting-started.md` for the full configuration reference.

`~/.wallfacer/.env` must contain at least one of:
- `CLAUDE_CODE_OAUTH_TOKEN` — OAuth token from `claude setup-token`
- `ANTHROPIC_API_KEY` — direct API key from console.anthropic.com

Optional variables (also in `.env`):
- `ANTHROPIC_AUTH_TOKEN` — bearer token for LLM gateway proxy authentication
- `ANTHROPIC_BASE_URL` — custom API endpoint; when set, the server queries `{base_url}/v1/models` to populate the model dropdown
- `CLAUDE_DEFAULT_MODEL` — default model passed as `--model` to task containers
- `CLAUDE_TITLE_MODEL` — model for background title generation; falls back to `CLAUDE_DEFAULT_MODEL`
- `WALLFACER_SERVER_API_KEY` — bearer token for server API authentication
- `WALLFACER_MAX_PARALLEL` — maximum concurrent tasks for auto-promotion (default: 5)
- `WALLFACER_MAX_TEST_PARALLEL` — maximum concurrent test runs (default: inherits from MAX_PARALLEL)
- `WALLFACER_OVERSIGHT_INTERVAL` — minutes between periodic oversight generation while a task runs (0 = only at task completion, default: 0)
- `WALLFACER_AUTO_PUSH` — enable auto-push after task completion (`true`/`false`)
- `WALLFACER_AUTO_PUSH_THRESHOLD` — minimum completed tasks before auto-push triggers
- `WALLFACER_SANDBOX_FAST` — enable fast-mode sandbox hints (default: `true`)
- `WALLFACER_SANDBOX_BACKEND` — sandbox backend selection (values: `local`; default: `local`)
- `WALLFACER_CONTAINER_NETWORK` — container network name
- `WALLFACER_CONTAINER_CPUS` — container CPU limit (e.g. `"2.0"`)
- `WALLFACER_CONTAINER_MEMORY` — container memory limit (e.g. `"4g"`)
- `WALLFACER_TASK_WORKERS` — enable per-task worker containers for container reuse (`true`/`false`, default: `true`)
- `WALLFACER_DEPENDENCY_CACHES` — mount named volumes for dependency caches (npm, pip, cargo, go-build) that persist across container restarts (`true`/`false`, default: `false`)
- `WALLFACER_WORKSPACES` — workspace paths (OS path-list separated)
- `WALLFACER_ARCHIVED_TASKS_PER_PAGE` — pagination size for archived tasks
- `WALLFACER_TOMBSTONE_RETENTION_DAYS` — days to retain soft-deleted task data (default: 7)
- `OPENAI_API_KEY` — API key for OpenAI Codex sandbox
- `OPENAI_BASE_URL` — custom OpenAI API endpoint
- `CODEX_DEFAULT_MODEL` — default model for Codex sandbox containers
- `CODEX_TITLE_MODEL` — model for Codex title generation
- Sandbox routing: `WALLFACER_DEFAULT_SANDBOX`, `WALLFACER_SANDBOX_IMPLEMENTATION`, `WALLFACER_SANDBOX_TESTING`, `WALLFACER_SANDBOX_REFINEMENT`, `WALLFACER_SANDBOX_TITLE`, `WALLFACER_SANDBOX_OVERSIGHT`, `WALLFACER_SANDBOX_COMMIT_MESSAGE`, `WALLFACER_SANDBOX_IDEA_AGENT`

All can be edited from **Settings → API Configuration** in the UI (calls `PUT /api/env`).

## Implementation checklist

Every implementation task MUST complete all three steps before finishing:

1. **Add tests** — Write unit tests for all new or changed functionality. Tests must cover the happy path and at least one error/edge case. **Bug fixes must always include a regression test** that fails without the fix and passes with it. Run `go test ./...` (backend) or `cd ui && npx vitest@2 run` (frontend) to confirm they pass before committing.

2. **Update docs** — If your change adds, removes, or modifies any API route, CLI flag, env variable, data model field, or user-visible behavior, update the corresponding documentation. Do not skip this step. The user manual lives in `docs/guide/` with these focused guides:
   - `docs/guide/usage.md` — Index page with reading order (update if adding a new guide)
   - `docs/guide/getting-started.md` — Installation and first run
   - `docs/guide/board-and-tasks.md` — Task board, lifecycle, creation, dependencies, search
   - `docs/guide/workspaces.md` — Workspaces, workspace groups, git integration
   - `docs/guide/automation.md` — Automation pipeline toggles, auto-retry, circuit breakers
   - `docs/guide/refinement-and-ideation.md` — Prompt refinement and brainstorm agents
   - `docs/guide/oversight-and-analytics.md` — Oversight, usage tracking, costs, timeline
   - `docs/guide/configuration.md` — Settings, env vars, sandboxes, CLI, security
   - `docs/guide/circuit-breakers.md` — Fault isolation reference

   Each guide has an **Essentials** section (core usage) and an **Advanced Topics** section (power-user features). Place new content in the appropriate section. If a new feature doesn't fit any existing guide, create a new guide file and add it to the `## Reading Order` section in `usage.md` — the server parses the first `[Title](file.md)` link under each `###` heading to build the numbered docs sidebar. Also update `AGENTS.md`, `CLAUDE.md`, and `docs/internals/*.md` as needed.

3. **Reflect on codebase health** — After implementing, review the files you touched and their immediate surroundings. If you spot a small, safe refactoring opportunity (dead code, unclear naming, duplicated logic, missing error handling) that is directly related to your change, include it. Keep refactoring changes minimal and scoped — do not redesign unrelated subsystems.

## Commit and push strategy

- Before committing, always run `make fmt` and `make lint` and fix any issues they report.
- Keep commits small and focused on one logical change.
- Do not include unrelated changes in the same commit.
- Use scoped, imperative commit messages matching existing style, e.g. `internal/runner: ...`, `ui: ...`, `docs: ...`.
- Stage only the files required for that change, then commit once.
- Push only once after creating the commit.
- If follow-up work is needed, create a new small commit and push again.
