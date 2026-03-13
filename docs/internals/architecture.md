# Architecture

Wallfacer is a host-native Go service that coordinates autonomous coding agents running in ephemeral containers, with per-task git worktree isolation and a web task board for human oversight.

## System Overview

```text
┌─────────────────────────────────────────────────────────────────────┐
│ Browser UI (Vanilla JS + Tailwind + Sortable.js)                  │
│ - Task board and modals                                            │
│ - SSE streams: task deltas, logs, git status                      │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ HTTP / SSE
┌──────────────────────────────▼──────────────────────────────────────┐
│ Go Server (main.go + server.go + internal/*)                       │
│ - REST handlers + background automation loops                       │
│ - Store-backed task state machine + event timeline                  │
│ - Runner orchestration (containers, worktrees, commit pipeline)     │
└───────────────┬───────────────────────────────┬─────────────────────┘
                │ os/exec container runtime     │ git CLI on host
┌───────────────▼───────────────┐   ┌───────────▼────────────────────┐
│ Sandbox Containers             │   │ Per-task Git Worktrees         │
│ - Claude / Codex images        │   │ ~/.wallfacer/worktrees/...     │
│ - Activity-routed execution    │   │ task/<id> branches             │
└───────────────────────────────┘   └────────────────────────────────┘
```

## Core Design Principles

- Filesystem-first persistence (no database dependency).
- Strong task isolation (container + branch/worktree per task).
- Explicit, validated task state transitions.
- High operator visibility (SSE, traces, spans, oversight, usage stats).
- Automation with guardrails (autopilot, autotest, autosubmit, auto-retry, dependency checks, cost/token budgets).

## Runtime Architecture

### CLI entrypoints

- `wallfacer run [flags] [workspace ...]`
- `wallfacer env`
- `wallfacer exec <task-id-prefix> [-- command...]`
- `wallfacer status [flags]`

`run` initializes config/env, scopes data by workspace set, ensures workspace instructions, recovers orphaned tasks, starts automation watchers, and serves HTTP.

### Startup flow (`runServer`)

1. Parse flags and resolve workspace absolute paths.
2. Create workspace manager; scope data dir by workspace key.
3. Load store from disk (`task.json`, `traces/*.json`, `oversights/*.json`).
4. Purge tombstoned tasks older than retention period.
5. Ensure `~/.wallfacer/worktrees` exists.
6. Ensure workspace instructions file in `~/.wallfacer/instructions/<key>.md`.
7. Resolve/pull sandbox image (`ensureImage` with local fallback).
8. Build runner + handler.
9. Prune stale worktrees and recover orphaned tasks.
10. Start background watchers:
    - auto-promoter
    - ideation watcher
    - waiting-sync watcher
    - auto-tester
    - auto-submitter
11. Serve HTTP routes and embedded UI.
12. On shutdown: graceful HTTP drain, wait for tracked runner background jobs.

## Main Components

### `internal/store` (state + persistence)

- In-memory maps guarded by `sync.RWMutex`.
- Per-task persistence:
  - `task.json` (task state, with `SchemaVersion` for migrations)
  - `outputs/turn-*.json` + optional stderr sidecar
  - `traces/0001.json...` (append-only event timeline)
  - `oversights/<id>.json` (generated oversight summaries)
  - `summary.json` (immutable completion snapshot for analytics)
  - `tombstone.json` (soft-delete marker)
- Atomic writes via temp-file + rename.
- Task state machine enforced via transition table (`TaskStatus`).
- Pub/sub channel for live task deltas (used by SSE and automation triggers).
- Full-text search index over titles, prompts, tags, and oversight text.
- Soft delete with tombstone retention and automatic pruning.

### `internal/runner` (orchestration engine)

- Creates and cleans task worktrees (`task/<uuid8>` branches).
- Builds container invocations via `ContainerSpec`.
- Routes sandbox/model by activity:
  - implementation
  - testing
  - refinement
  - title
  - oversight
  - commit_message
  - idea_agent
- Executes turn loop, handles stop reasons, accumulates usage/cost.
- Per-turn usage records (`TurnUsageRecord`) for detailed cost analysis.
- Cost/token budget enforcement (`MaxCostUSD`, `MaxInputTokens`).
- Auto-retry with per-failure-category budget.
- Failure categorization (timeout, budget_exceeded, worktree_setup, container_crash, agent_error, sync_error, unknown).
- Execution environment recording for reproducibility auditing.
- Commit pipeline:
  - host-side `git add/commit`
  - rebase/merge with conflict handling
  - optional auto-push (`WALLFACER_AUTO_PUSH*`)
  - cleanup worktrees
- Task forking (branch from existing worktree state).
- Background generation:
  - titles
  - oversight summaries
  - periodic oversight while running
- Circuit breaker for container launch failures.
- Webhook notifications on task state changes.
- Tracks background goroutines for safe shutdown.

### `internal/handler` (HTTP + automation control plane)

- REST API for tasks, execution, git, env, config, files, instructions, templates, stats, admin.
- SSE endpoints for task deltas, logs, git status.
- In-process automation toggles:
  - `autopilot`
  - `autotest`
  - `autosubmit`
  - `ideation` (+ interval scheduling)
- System prompt template management (view/override/restore built-in prompts).
- Prompt template CRUD.
- Task search.
- Workspace switching at runtime.
- Diff cache and API-oriented data shaping.
- SSRF-hardening for user-provided gateway base URLs.
- Server API key authentication via `WALLFACER_SERVER_API_KEY`.

### `internal/envconfig`

- Parses and updates `.env` while preserving unknown keys/comments.
- Supports sandbox routing config, container resource limits, webhook settings, and operational settings.
- Used by runner and handlers at runtime (no restart required for most settings).

### `internal/workspace`

- Workspace manager scopes data by workspace key (SHA-256 of sorted paths).
- Supports runtime workspace switching (`PUT /api/workspaces`).
- Persists workspace list via `WALLFACER_WORKSPACES` in the env file.

### `internal/metrics`

- Prometheus-compatible metrics registry.
- Counters, histograms, and gauges for task execution, container lifecycle, and API latency.

### `prompts/`

- System prompt templates (`*.tmpl`) for background agents.
- Template manager with runtime rendering and user override support.
- Templates: title, commit, refinement, oversight, test, ideation, conflict.

### `ui/` frontend

- Vanilla JS modules, no framework runtime.
- Drag-and-drop task board, modal detail views, timeline/flamegraph, oversight, diff, usage, sandbox monitor, command palette.
- SSE-driven updates with snapshot + typed delta events.
- Frontend tests under `ui/js/tests` with Vitest.

## Task Execution Lifecycle (Implementation Task)

1. Task created in `backlog`.
2. Transition to `in_progress` (manual drag or autopilot).
3. Runner records execution environment and ensures worktrees and board context (`/workspace/.tasks/board.json`).
4. Runner launches container for each turn.
5. Per-turn artifacts saved:
   - output files
   - timeline events
   - usage aggregation (aggregate + per-turn records)
6. Budget enforcement: check `MaxCostUSD` and `MaxInputTokens` after each turn.
7. Stop reason handling:
   - `max_tokens` / `pause_turn`: auto-continue turn loop.
   - `end_turn`: move to `committing`, run commit pipeline, then `done` or `failed`.
   - other/empty stop reason: move to `waiting` for human feedback.
8. Oversight generation:
   - sync for waiting path
   - background for done path
   - optional periodic while task is running.
9. On failure: classify failure category; check auto-retry budget.
10. Task summary written on completion (immutable snapshot for analytics).

### Test run lifecycle

- Triggered from waiting via `/api/tasks/{id}/test`.
- Same task moves back to `in_progress` with `IsTestRun=true`.
- Returns to `waiting` with `LastTestResult` = `pass` / `fail` / `unknown`.
- Generates dedicated test oversight (`oversights/<id>.json`).

## Background Automation Loops

- Auto-promoter:
  - respects `WALLFACER_MAX_PARALLEL` (default 5)
  - skips idea-agent tasks
  - enforces dependency readiness (`DependsOn` all done)
  - enforces scheduled time (`ScheduledAt`)
- Auto-tester:
  - runs tests for eligible waiting tasks when enabled
  - respects `WALLFACER_MAX_TEST_PARALLEL`
- Auto-submitter:
  - promotes verified waiting tasks to done when conflict-free and up-to-date
- Waiting-sync watcher:
  - auto-syncs waiting tasks behind default branch
- Ideation watcher:
  - schedules/enqueues idea-agent tasks based on enable flag + interval
- Auto-retry:
  - failed tasks with remaining budget are automatically retried after classification

## API Surface (High-Level)

Core groups in `buildMux` (see `internal/apicontract/routes.go` for the canonical list):

- UI/static:
  - `GET /`
- Debug:
  - `GET /api/debug/health`
  - `GET /api/debug/spans`
  - `GET /api/debug/runtime`
  - `GET /api/debug/board`
  - `GET /api/tasks/{id}/board`
- Config/env/instructions:
  - `GET/PUT /api/config`
  - `GET/PUT /api/env`
  - `POST /api/env/test`
  - `POST /api/env/test-webhook`
  - `GET/PUT /api/instructions`
  - `POST /api/instructions/reinit`
- Workspaces:
  - `GET /api/workspaces/browse`
  - `PUT /api/workspaces`
- System prompt templates:
  - `GET /api/system-prompts`
  - `GET/PUT/DELETE /api/system-prompts/{name}`
- Prompt templates:
  - `GET/POST /api/templates`
  - `DELETE /api/templates/{id}`
- Tasks:
  - CRUD: `GET/POST/PATCH/DELETE /api/tasks...`
  - batch create: `POST /api/tasks/batch`
  - execution actions: feedback, done, cancel, resume, sync, test, fork, restore
  - refinement: start/cancel/logs/apply/dismiss
  - oversight/spans/diff/events/logs/outputs/turn-usage/board
  - archive/unarchive + archive-all
  - generation helpers: missing titles/oversight
  - search, summaries, deleted tasks
- Streams:
  - `GET /api/tasks/stream`
  - `GET /api/git/stream`
  - `GET /api/tasks/{id}/logs`
  - `GET /api/tasks/{id}/refine/logs`
- Git workspace ops:
  - status/push/sync/rebase/branches/checkout/create-branch/open-folder
- Ops/observability:
  - `GET /api/containers`
  - `GET /api/files`
  - `GET /api/usage`
  - `GET /api/stats`
  - ideation status/trigger/cancel
- Admin:
  - `POST /api/admin/rebuild-index`

See `docs/internals/orchestration.md` for endpoint-level behavior.

## Data Model Highlights

- `TaskStatus`: `backlog`, `in_progress`, `waiting`, `committing`, `done`, `failed`, `cancelled`
- `TaskKind`: regular task or `idea-agent`
- `archived` is a flag (not a status)
- `FailureCategory`: `timeout`, `budget_exceeded`, `worktree_setup`, `container_crash`, `agent_error`, `sync_error`, `unknown`
- Optional per-activity sandbox overrides on each task
- `DependsOn` DAG support for gated autopromotion
- `ScheduledAt` for time-delayed promotion
- `ForkedFrom` lineage tracking
- `MaxCostUSD` and `MaxInputTokens` budget limits
- `AutoRetryBudget` per failure category
- `ExecutionEnvironment` for reproducibility auditing
- `ModelOverride` for per-task model selection
- `TaskSummary` immutable completion snapshot
- `Tombstone` soft-delete marker with retention
- `TurnUsageRecord` per-turn token consumption
- `RetryRecord` for retry history tracking

## Concurrency and Synchronization

- `Store.mu`: task/event map integrity.
- `promoteMu`: serialize autopromote decisions.
- `Runner.worktreeMu`: serialize filesystem worktree operations.
- Per-repo mutex map for rebase/merge serialization.
- Per-task oversight mutex map to avoid concurrent summary races.
- `trackedWg` for lifecycle-safe background jobs.

## Recovery and Fault Tolerance

On startup, `RecoverOrphanedTasks` reconciles interrupted work:

- `committing` tasks:
  - inspects each worktree's git branch; if a commit landed after the task's `UpdatedAt` timestamp → recover to `done`
  - else → mark `failed`
- `in_progress` tasks:
  - container still running → keep running and monitor
  - container gone → move to `waiting` for operator decision

If worktrees are missing for a task during recovery, the task is marked `failed` with `FailureCategory = worktree_setup`.

## Security and Hardening

- Server API key authentication (`WALLFACER_SERVER_API_KEY`).
- Base URL validation rejects unsafe hosts/schemes for gateway endpoints.
- DNS + dial-time SSRF checks block private/loopback/link-local targets.
- Output file serving guards against path traversal.
- Task/container correlation uses labels (`wallfacer.task.id`) for safer lookup.
- CSRF protection on state-changing endpoints.
- Request body size limits.

## Configuration Model

### Runtime flags

- `-addr`
- `-data`
- `-container`
- `-image`
- `-env-file`
- `-no-browser`
- `-no-workspaces`
- `-log-format`

### Important env keys

- Auth/models:
  - `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`
  - `OPENAI_API_KEY`, `OPENAI_BASE_URL`
  - `CLAUDE_DEFAULT_MODEL`, `CLAUDE_TITLE_MODEL`
  - `CODEX_DEFAULT_MODEL`, `CODEX_TITLE_MODEL`
  - `WALLFACER_SERVER_API_KEY`
- Ops:
  - `WALLFACER_MAX_PARALLEL`
  - `WALLFACER_MAX_TEST_PARALLEL`
  - `WALLFACER_OVERSIGHT_INTERVAL`
  - `WALLFACER_AUTO_PUSH`
  - `WALLFACER_AUTO_PUSH_THRESHOLD`
  - `WALLFACER_SANDBOX_FAST`
  - `WALLFACER_ARCHIVED_TASKS_PER_PAGE`
  - `WALLFACER_TOMBSTONE_RETENTION_DAYS`
- Container resources:
  - `WALLFACER_CONTAINER_NETWORK`
  - `WALLFACER_CONTAINER_CPUS`
  - `WALLFACER_CONTAINER_MEMORY`
- Webhooks:
  - `WALLFACER_WEBHOOK_URL`
  - `WALLFACER_WEBHOOK_SECRET`
- Workspaces:
  - `WALLFACER_WORKSPACES`
- Sandbox routing:
  - `WALLFACER_DEFAULT_SANDBOX`
  - `WALLFACER_SANDBOX_IMPLEMENTATION`
  - `WALLFACER_SANDBOX_TESTING`
  - `WALLFACER_SANDBOX_REFINEMENT`
  - `WALLFACER_SANDBOX_TITLE`
  - `WALLFACER_SANDBOX_OVERSIGHT`
  - `WALLFACER_SANDBOX_COMMIT_MESSAGE`
  - `WALLFACER_SANDBOX_IDEA_AGENT`

For setup and operator-facing behavior, see:

- `docs/getting-started.md`
- `docs/usage.md`
- `docs/internals/task-lifecycle.md`
- `docs/internals/orchestration.md`
