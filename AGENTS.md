This file provides guidance when working with code in this repository.

## Project Overview

Wallfacer is a task-board runner for AI agents. It provides a web UI where tasks are created as cards, dragged to "In Progress" to trigger AI agent execution in an isolated sandbox container, and results are inspected when done.

**Architecture:** Browser → Go server (:8080) → per-task directory storage (`data/<uuid>/`). The server runs natively on the host and launches ephemeral sandbox containers via `os/exec` (podman/docker). Each task gets its own git worktree for isolation.

For detailed documentation see `docs/`. The user manual is at `docs/guide/usage.md` and the technical internals index is at `docs/internals/internals.md`.

## Specs & Roadmap

Design specs live in `specs/`, organized by track:
- `specs/foundations/` — completed abstraction interfaces (sandbox, storage, container reuse, file explorer, terminal)
- `specs/local/` — local product and UX (spec coordination, desktop app, attachments, terminal extensions, enhancements)
- `specs/cloud/` — cloud platform (tenant filesystem, K8s sandbox, multi-tenant, tenant API)
- `specs/shared/` — cross-track specs (authentication, agent abstraction, native sandboxes, overlay snapshots)

The roadmap and dependency graph are in [`specs/README.md`](specs/README.md). When creating or modifying a spec:

- **Read `specs/README.md` first** to understand the track organization and dependency graph.
- **Place new specs** in the appropriate track directory. Use descriptive filenames without numeric prefixes (e.g., `specs/local/live-serve.md`).
- **Every spec MUST have valid YAML frontmatter** matching the spec document model (see `specs/local/spec-coordination/spec-document-model.md`). Required fields:
  ```yaml
  ---
  title: Human-readable title
  status: drafted          # vague | drafted | validated | complete | stale | archived
  depends_on:              # list of spec paths this one requires (DAG edges)
    - specs/shared/agent-abstraction.md
  affects:                 # packages and files this spec describes
    - internal/runner/
  effort: large            # small | medium | large | xlarge
  created: 2026-04-01      # ISO date
  updated: 2026-04-01      # ISO date, must be >= created
  author: changkun
  dispatched_task_id: null  # null or UUID (leaf specs only)
  ---
  ```
- **Assess cross-impacts** with existing specs. If a new spec modifies interfaces defined in foundations (`SandboxBackend`, `StorageBackend`), it must declare the dependency.
- **Update `specs/README.md`** when adding a spec — add it to the appropriate track table, the status overview, and the dependency graph.

## Build & Run Commands

```bash
make build          # Full gate: fmt + lint (Go + JS) + binary + pull sandbox images
make build-host     # Host-mode build: full gate, no image pull (for `wallfacer run --backend host`)
make build-binary   # Build just the Go binary (no fmt/lint/pull)
make pull-images    # Pull Claude and Codex sandbox images
make install-wails  # Install the Wails CLI (tracked as tool in go.mod)
make build-desktop  # Build native desktop app for current platform (uses go tool wails)
make build-desktop-darwin   # Build macOS .app bundle
make build-desktop-windows  # Build Windows .exe
make build-desktop-linux    # Build Linux desktop binary
make server         # Build and run the Go server natively
make shell          # Open bash shell in sandbox container for debugging
make clean          # Remove all sandbox images
make run PROMPT="…" # Headless one-shot Claude execution with a prompt
make test           # Run all tests (backend + frontend)
make test-backend   # Run Go unit tests (go test ./...)
make test-frontend  # Run frontend JS unit tests (cd ui && bunx vitest@2 run)
make ui-css         # Regenerate Tailwind CSS from UI sources
make api-contract   # Regenerate API route artifacts from apicontract/routes.go
make e2e-lifecycle              # E2E: task lifecycle for both sandboxes (requires running server)
make e2e-lifecycle SANDBOX=claude  # E2E: task lifecycle for Claude only
make e2e-lifecycle BACKEND=host    # E2E against `wallfacer run --backend host`
make e2e-dependency-dag WORKSPACE=/path/to/repo  # E2E: dependency DAG with conflict resolution
```

CLI usage (after `make build-binary`, or `make build` for the full fmt/lint-gated build):

```bash
wallfacer                                    # Print help
wallfacer run                                # Start server, restore last workspace group
wallfacer run -addr :9090 -no-browser        # Custom port, no browser
wallfacer run --backend host                 # Host mode: exec claude/codex directly (no container)
wallfacer doctor                             # Check prerequisites and config
wallfacer doctor --backend host              # Readiness check for host-mode deployments
wallfacer status                             # Print board state to terminal
wallfacer status -watch                      # Live-updating board state
wallfacer exec <task-id-prefix>              # Attach to running task container
wallfacer exec --sandbox claude              # Open shell in a new sandbox
wallfacer spec validate                      # Validate the entire specs/ tree
wallfacer spec validate specs/local/foo.md   # Validate specific files only
wallfacer spec validate -json                # Emit JSON for scripting / CI
wallfacer spec new specs/local/foo.md        # Scaffold a spec with valid frontmatter defaults
wallfacer spec new -title "My Feature" -status drafted specs/local/my-feature.md
```

The Makefile uses Podman (`/opt/podman/bin/podman`) by default. Adjust `PODMAN` variable if using Docker.

## Server Development

The Go source lives at the top level. Module path: `changkun.de/x/wallfacer`. Go version: 1.25.7.

**Preferred loop — use `make` targets, not raw `go` invocations.** The `make` targets run the project's full validation (gofmt, golangci-lint, Biome for JS), not just compilation. Raw `go build`/`go vet` skip lint and can commit code that fails CI.

```bash
make build          # fmt + lint (Go + JS) + build + pull images (full gate)
make build-binary   # just build the Go binary (fast; skips lint/pull)
make lint           # lint only (fastest way to catch style regressions)
make fmt            # format Go and JS in place
make test           # lint + backend tests + frontend tests
make test-backend   # go test ./...
make test-frontend  # vitest in ui/

# Raw Go equivalents (useful for debugging a single package, but run
# `make lint` before committing — they do not run golangci-lint or Biome):
go build -o wallfacer .
go vet ./...
go test ./...
cd ui && bunx --bun vitest@2 run
```

The server uses `net/http` stdlib routing (Go 1.22+ pattern syntax) with no framework.

Key server files:
- `main.go` — Tiny entry point: embed FS declarations and subcommand dispatch
- `internal/cli/` — CLI subcommand implementations (server, exec, status, env) and shared helpers
- `internal/apicontract/` — Single source of truth for all HTTP API routes; generates `ui/js/generated/routes.js`
- `internal/handler/` — HTTP API handlers (one file per concern: tasks, env, config, git, instructions, containers, stream, execute, files, oversight, ideate, templates, stats, admin, workspace, runtime, auth)
- `internal/oauth/` — OAuth 2.0 PKCE flow engine, ephemeral callback server, provider configs (Claude, Codex), token exchange
- `internal/runner/` — Container orchestration via `os/exec`; task execution loop; commit pipeline; usage tracking; worktree sync; title generation; oversight; refinement; ideation; auto-retry; circuit breaker
- `internal/store/` — Per-task directory persistence, data models (Task, TaskUsage, TurnUsageRecord, TaskEvent, TaskOversight, TaskSummary, Tombstone, RetryRecord, FailureCategory), event sourcing, soft delete, search index; see `docs/internals/data-and-storage.md` for full data model documentation
- `internal/envconfig/` — `.env` file parsing and atomic update; exposes `Parse` and `Update` for the handler and runner
- `internal/gitutil/` — Git utility operations (ops, repo, status, stash, worktree)
- `internal/workspace/` — Workspace manager; scopes data by workspace key; supports runtime workspace switching with concurrent multi-group execution (stores stay alive while tasks are running)
- `internal/logger/` — Structured logging utilities
- `internal/metrics/` — Prometheus-compatible metrics
- `internal/constants/` — Consolidated system parameters: timeouts, intervals, retry counts, size limits, concurrency caps, pagination defaults
- `internal/sandbox/` — Sandbox type enumeration (Claude, Codex); Windows host path translation for container mounts
- `internal/prompts/` — System prompt templates (title, commit, refinement, oversight, test, ideation, conflict, instructions) and workspace-level AGENTS.md management (`~/.wallfacer/instructions/`)
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
- `POST /api/tasks` — Create task (JSON: `{prompt, flow?, timeout, tags?, ...}`). `flow` is a flow slug from `/api/flows`; defaults to `implement` when omitted. **Does not accept `sandbox` or `sandbox_by_activity`** — harness lives on the agent definition, and per-task sandbox overrides are applied via `PATCH /api/tasks/{id}` after creation.
- `POST /api/tasks/batch` — Create multiple tasks atomically with symbolic dependency wiring; same `flow` support and same sandbox rejection rule as the singular endpoint.
- `PATCH /api/tasks/{id}` — Update status/position/prompt/timeout/sandbox/dependencies/fresh_start
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

### Agents
- `GET /api/agents` — List merged built-in + user-authored agent catalog
- `GET /api/agents/{slug}` — Get one agent's full descriptor including its prompt template body
- `POST /api/agents` — Create a user-authored agent (rejects slugs that shadow a built-in)
- `PUT /api/agents/{slug}` — Update a user-authored agent (409 on built-in slugs)
- `DELETE /api/agents/{slug}` — Delete a user-authored agent (409 on built-in slugs)

User agents live as YAML under `~/.wallfacer/agents/` (overridable via `WALLFACER_AGENTS_DIR`). The runner watches the directory via fsnotify and reloads the merged registry within a ~150ms debounce on any change.

### Flows
- `GET /api/flows` — List merged built-in + user-authored flow catalog
- `GET /api/flows/{slug}` — Get one flow's full descriptor including its step chain and resolved agent names
- `POST /api/flows` — Create a user-authored flow (rejects slugs that shadow a built-in)
- `PUT /api/flows/{slug}` — Update a user-authored flow (409 on built-in slugs)
- `DELETE /api/flows/{slug}` — Delete a user-authored flow (409 on built-in slugs)

User flows live as YAML under `~/.wallfacer/flows/` (overridable via `WALLFACER_FLOWS_DIR`). Same fsnotify watching as agents.

Built-in flows: `implement` (impl → test → commit-msg ‖ title ‖ oversight), `brainstorm` (ideate), `test-only` (test).

### Ideation
- `GET /api/ideate` — Get current ideation session state (reads from the system:ideation routine card)
- `POST /api/ideate` — Fire the ideation routine immediately; auto-bootstraps the routine if missing
- `DELETE /api/ideate` — Cancel the currently-running idea-agent instance task (routine card unaffected)

Ideation is implemented as a routine task (`Kind=routine`, `Tags=["system:ideation"]`, `RoutineSpawnFlow=brainstorm`) that the scheduler engine in `internal/routine/` fires on its configured cadence. See the Routines section below for the generic primitive.

### Routines
- `GET /api/routines` — List routine cards with schedules and next-run times
- `POST /api/routines` — Create a routine (JSON: `{prompt, interval_minutes, spawn_flow?, spawn_kind?, enabled?, tags?, timeout?}`). `spawn_flow` (a flow slug) wins over `spawn_kind` (legacy) when both are present.
- `PATCH /api/routines/{id}/schedule` — Update interval and/or enabled flag (JSON: `{interval_minutes?, enabled?}`); omitted fields left unchanged
- `POST /api/routines/{id}/trigger` — Fire immediately; scheduled cycle continues unchanged
- Deletion uses `DELETE /api/tasks/{id}` (routine cards are tasks)

### Spec Tree
- `GET /api/specs/tree` — Full spec tree with metadata, progress, and dependency edges
- `GET /api/specs/stream` — SSE: spec tree change notifications (sends snapshot on change)
- `POST /api/specs/dispatch` — Dispatch validated specs to create board tasks atomically (JSON: `{paths, run}`)
- `POST /api/specs/undispatch` — Cancel dispatched tasks and clear spec dispatch linkage (JSON: `{paths}`)
- `POST /api/specs/archive` — Archive a spec: transition status to `archived` (JSON: `{path}`). 409 if a dispatched task is still linked; 422 on invalid transition.
- `POST /api/specs/unarchive` — Unarchive a spec: transition status from `archived` back to `drafted` (JSON: `{path}`).

### Planning Sandbox
- `GET /api/planning` — Get planning sandbox status (running or not)
- `POST /api/planning` — Start the planning sandbox container (idempotent)
- `DELETE /api/planning` — Stop the planning sandbox container
- `GET /api/planning/messages` — Retrieve conversation history for a thread (supports `?before=<RFC3339>` pagination and `?thread=<id>`; defaults to the active thread)
- `POST /api/planning/messages` — Send a user message; body fields: `message` (required), `focused_spec` (spec-mode), `focused_task` (task-mode UUID), `thread`. `focused_spec` and `focused_task` are mutually exclusive (422). Unknown `focused_task` → 404. Mode mismatch on an already-pinned thread → 409. Returns 202; 409 if busy.
- `DELETE /api/planning/messages` — Clear a thread's conversation history and session (`?thread=<id>`; defaults to active)
- `GET /api/planning/messages/stream` — SSE: stream agent response tokens. Returns 204 if no exec in flight or `?thread=<id>` does not match the in-flight thread
- `POST /api/planning/messages/interrupt` — Interrupt current agent turn. `?thread=<id>` must match the in-flight thread (409 otherwise)
- `POST /api/planning/undo` — Undo the caller thread's most recent planning round via `git revert` (history keeps the original commit and the revert; the revert carries `Plan-Thread: <id>` and an incremented `Plan-Round`). `?thread=<id>` selects the caller's thread; defaults to the active. Dispatched board tasks whose `dispatched_task_id` was added in the reverted commit are cancelled. 409 on revert conflict (aborts the revert, leaves the tree clean) or stash-pop conflict.
- `GET /api/planning/commands` — List available slash commands for UI autocomplete

### Planning Chat Threads
- `GET /api/planning/threads` — List planning chat threads for the current workspace group; returns `{threads, active_id}`. Each thread includes `mode: "spec"|"task"` and `task_id` (non-empty when mode is "task"). Pass `?includeArchived=true` to include archived threads.
- `POST /api/planning/threads` — Create a new thread. Body `{name?, focused_task?}`; when `name` is omitted a default `Chat N` name is used. When `focused_task` (a valid task UUID) is supplied, the thread is pinned to task-mode immediately.
- `PATCH /api/planning/threads/{id}` — Rename a thread (body `{name}`).
- `POST /api/planning/threads/{id}/archive` — Archive a thread (hides it from the tab bar; files are retained). 409 if the thread is in-flight.
- `POST /api/planning/threads/{id}/unarchive` — Restore an archived thread.
- `POST /api/planning/threads/{id}/activate` — Record the UI's active thread (rejects archived/unknown IDs).

### Usage & Statistics
- `GET /api/usage` — Aggregated token and cost usage statistics
- `GET /api/stats` — Task status and workspace cost statistics; includes a `planning` section keyed by workspace group; `?days=N` restricts planning aggregation only
- `GET /api/containers` — List running containers
- `GET /api/files` — File listing for @ mention autocomplete

### Sandbox Images
- `GET /api/images` — Check which sandbox images are cached locally
- `POST /api/images/pull` — Start async pull for a sandbox image
- `DELETE /api/images` — Remove a cached sandbox image
- `GET /api/images/pull/stream` — SSE stream of pull progress

### File Explorer
- `GET /api/explorer/tree` — List one level of a workspace directory
- `GET /api/explorer/stream` — SSE: file tree change notifications for workspace directories
- `GET /api/explorer/file` — Read file contents from a workspace
- `PUT /api/explorer/file` — Write file contents to a workspace

### Terminal
- `GET /api/terminal/ws` — WebSocket: interactive host shell and container exec with multi-session tab support (not in apicontract; requires `?token=` auth). `create_session` accepts optional `container` field to exec into a running task container. See `docs/internals/api-and-transport.md` for full protocol.

### OAuth Authentication
- `POST /api/auth/{provider}/start` — Start OAuth flow for `claude` or `codex`; returns `{authorize_url}`
- `GET /api/auth/{provider}/status` — Poll flow status; returns `{state: "pending"|"success"|"error"}`
- `POST /api/auth/{provider}/cancel` — Cancel an in-progress OAuth flow

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
- **System prompt templates** are overridable built-in prompts (`internal/prompts/*.tmpl`); users can customize via the UI or API; includes the workspace instructions template
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
- `WALLFACER_PLANNING_WINDOW_DAYS` — default window (in days) for the planning-cost analytics display (default: `30`; `0` = all time)
- `WALLFACER_SANDBOX_FAST` — enable fast-mode sandbox hints (default: `true`)
- `WALLFACER_HOST_CLAUDE_BINARY` — explicit path to the host `claude` CLI (host-mode only; default: `$PATH` lookup)
- `WALLFACER_HOST_CODEX_BINARY` — explicit path to the host `codex` CLI (host-mode only; default: `$PATH` lookup)
- *(backend selection)* Not an env var — pass `--backend container` (default) or `--backend host` to `wallfacer run`.
- `WALLFACER_CONTAINER_NETWORK` — container network name
- `WALLFACER_CONTAINER_CPUS` — container CPU limit (e.g. `"2.0"`)
- `WALLFACER_CONTAINER_MEMORY` — container memory limit (e.g. `"4g"`)
- `WALLFACER_TASK_WORKERS` — enable per-task worker containers for container reuse (`true`/`false`, default: `true`)
- `WALLFACER_DEPENDENCY_CACHES` — mount named volumes for dependency caches (npm, pip, cargo, go-build) that persist across container restarts (`true`/`false`, default: `false`)
- `WALLFACER_TERMINAL_ENABLED` — enable integrated host terminal (`true`/`false`, default `true`)
- `WALLFACER_WORKSPACES` — workspace paths (OS path-list separated)
- `WALLFACER_ARCHIVED_TASKS_PER_PAGE` — pagination size for archived tasks
- `WALLFACER_TOMBSTONE_RETENTION_DAYS` — days to retain soft-deleted task data (default: 7)
- `OPENAI_API_KEY` — API key for OpenAI Codex sandbox
- `OPENAI_BASE_URL` — custom OpenAI API endpoint
- `CODEX_DEFAULT_MODEL` — default model for Codex sandbox containers
- `CODEX_TITLE_MODEL` — model for Codex title generation
- Sandbox routing: `WALLFACER_DEFAULT_SANDBOX`, `WALLFACER_SANDBOX_IMPLEMENTATION`, `WALLFACER_SANDBOX_TESTING`, `WALLFACER_SANDBOX_TITLE`, `WALLFACER_SANDBOX_OVERSIGHT`, `WALLFACER_SANDBOX_COMMIT_MESSAGE`, `WALLFACER_SANDBOX_IDEA_AGENT`
- `WALLFACER_CLOUD` — enable cloud-gated UI surfaces (latere.ai sign-in badge, future cloud toggles); shell env only, not edited from the UI. Defaults to `false` (pure local mode). See [`docs/cloud/README.md`](docs/cloud/README.md) for the full env-var reference, deployment constraints, and Phase 1 scope; [`specs/shared/authentication.md`](specs/shared/authentication.md) covers the long-range design.
- `AUTH_JWKS_URL` — JWKS endpoint used to validate `Authorization: Bearer <jwt>` headers on `/api/*` in cloud mode. Auto-derived from `AUTH_URL + /.well-known/jwks.json` when unset.
- `AUTH_ISSUER` — expected `iss` claim on incoming JWTs. Auto-derived from `AUTH_URL` when unset.

All can be edited from **Settings → API Configuration** in the UI (calls `PUT /api/env`).

**Cloud vs local partition.** `WALLFACER_CLOUD` is the single gate between local-only functionality and cloud surfaces. Cloud adds identity, not feature gates — task execution is identical in both modes. Full details in [`docs/cloud/README.md`](docs/cloud/README.md).

## End-to-end integration tests

Two E2E test scripts in `scripts/` exercise the full task lifecycle against a running wallfacer server with real sandbox containers. Use these to verify that task execution, commit pipelines, conflict resolution, and automation work correctly after making changes.

### e2e-lifecycle (task lifecycle)

Tests the basic create-run-archive lifecycle for Claude and Codex sandboxes. Each sandbox gets a "who are you?" task that must complete without errors.

```bash
# Requires a running wallfacer server with valid credentials.
wallfacer run &

# Test both sandboxes:
make e2e-lifecycle

# Test one sandbox:
make e2e-lifecycle SANDBOX=claude
make e2e-lifecycle SANDBOX=codex

# Custom server URL:
WALLFACER_URL=http://localhost:9090 sh scripts/e2e-lifecycle.sh
```

**What it checks:**
- Task creation with sandbox selection (Claude/Codex)
- Task execution (backlog -> in_progress -> done)
- Task result contains a response
- Commit pipeline (waiting -> committing -> done)
- Archive and container cleanup

**Expected output:** 30 checks pass (10 per sandbox + preflight + smoke tests if ENV_FILE is set).

### e2e-dependency-dag (parallel tasks with conflicts)

Tests a fan-out/fan-in dependency DAG with 8 tasks that modify the same file in parallel, exercising autopilot promotion, conflict resolution, and autosync.

Task graph:
```
a: create test.md
b,c,d,e,f,g: update test.md (all depend on a, run in parallel)
h: delete test.md (depends on b,c,d,e,f,g)
```

```bash
# Requires a running wallfacer server.
wallfacer run &

# Create a fresh git repo as workspace:
WORKSPACE=$(mktemp -d)
git -C "$WORKSPACE" init -b main
git -C "$WORKSPACE" commit --allow-empty -m "init"

# Run the test:
make e2e-dependency-dag WORKSPACE="$WORKSPACE"
```

**What it checks:**
- Batch task creation with dependency wiring (8 tasks, DAG edges)
- Autopilot promotion respects dependencies (waits for AutoPromoteInterval, 60s)
- Max parallel tasks honored (configured to 3)
- Conflict resolution during rebase (tasks b-g all modify test.md)
- Autosync rebases worktrees onto latest default branch
- Autosubmit commits and merges without manual intervention
- Each task produces at least one commit
- test.md is deleted at the end (task h ran last)
- Archive all tasks, verify containers are cleaned up
- Reports commit order for inspection

**Expected output:** all checks pass, 9+ commits (1 init + 8 tasks), commit history printed oldest-first.

**Environment variables:**
| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_URL` | `http://localhost:8080` | Server URL |
| `WALLFACER_SERVER_API_KEY` | (empty) | Bearer token if server has auth enabled |
| `WALLFACER_TEST_TIMEOUT` | `300` | Seconds to wait for all tasks to complete |

### When to run E2E tests

- After changes to `internal/runner/` (task execution, commit pipeline, conflict resolution)
- After changes to `internal/handler/tasks*.go` or `internal/handler/execute.go` (task API, automation)
- After changes to `internal/gitutil/` (worktree, rebase, merge operations)
- After sandbox image updates (new versions of sandbox-agents)
- Before releases to verify end-to-end correctness

## Bug fixes require a reproducible test

Every bug fix MUST ship with a regression test that **fails without the fix and passes with it**. No exceptions — this applies to backend, frontend, and CLI bugs alike. Before applying the fix:

1. Write a test that reproduces the bug and confirm it fails on the unpatched code.
2. Apply the minimal fix.
3. Confirm the same test now passes.
4. Commit the test and the fix together (single logical change).

If a bug genuinely cannot be covered by an automated test (e.g. a rare race reproducible only under manual load), document in the commit message *why* and what manual reproduction steps were used.

## Implementation checklist

Every implementation task MUST complete all three steps before finishing:

1. **Add tests** — Write unit tests for all new or changed functionality. Tests must cover the happy path and at least one error/edge case. **Bug fixes must always include a reproducible regression test** (see the *Bug fixes require a reproducible test* section above) — this is mandatory, not a best effort. Run `make test` before committing — it wraps `make lint` (golangci-lint + Biome) plus backend + frontend test runs, so a clean `make test` is the single gate that matches CI. Use `make test-backend` / `make test-frontend` for faster targeted runs during iteration.

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

- Before committing, always run `make build` (or at minimum `make fmt && make lint`) and fix any issues they report. `make build` is the full gate — it runs fmt, lint (Go + JS), and the binary compile, catching everything CI checks at build time.
- Keep commits small and focused on one logical change.
- Do not include unrelated changes in the same commit.
- Use scoped, imperative commit messages matching existing style, e.g. `internal/runner: ...`, `ui: ...`, `docs: ...`.
- Stage only the files required for that change, then commit once.
- Push only once after creating the commit.
- If follow-up work is needed, create a new small commit and push again.
