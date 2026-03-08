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
- Automation with guardrails (autopilot, autotest, autosubmit, dependency checks).

## Runtime Architecture

### CLI entrypoints

- `wallfacer run [flags] [workspace ...]`
- `wallfacer env`
- `wallfacer exec <task-id-prefix> [-- command...]`

`run` initializes config/env, scopes data by workspace set, ensures workspace instructions, recovers orphaned tasks, starts automation watchers, and serves HTTP.

### Startup flow (`runServer`)

1. Parse flags and resolve workspace absolute paths.
2. Scope data dir by workspace key: `DATA_DIR/<instructions.Key(workspaces)>`.
3. Load store from disk (`task.json`, `traces/*.json`).
4. Ensure `~/.wallfacer/worktrees` exists.
5. Ensure workspace instructions file in `~/.wallfacer/instructions/<key>.md`.
6. Resolve/pull sandbox image (`ensureImage` with local fallback).
7. Build runner + handler.
8. Prune stale worktrees and recover orphaned tasks.
9. Start background watchers:
   - auto-promoter
   - ideation watcher
   - waiting-sync watcher
   - auto-tester
   - auto-submitter
10. Serve HTTP routes and embedded UI.
11. On shutdown: graceful HTTP drain, wait for tracked runner background jobs.

## Main Components

### `internal/store` (state + persistence)

- In-memory maps guarded by `sync.RWMutex`.
- Per-task persistence:
  - `task.json` (task state)
  - `outputs/turn-*.json` + optional stderr sidecar
  - `traces/0001.json...` (append-only event timeline)
  - `oversight.json`, `oversight-test.json`
- Atomic writes via temp-file + rename.
- Task state machine enforced via transition table (`TaskStatus`).
- Pub/sub channel for live task deltas (used by SSE and automation triggers).

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
- Commit pipeline:
  - host-side `git add/commit`
  - rebase/merge with conflict handling
  - optional auto-push (`WALLFACER_AUTO_PUSH*`)
  - cleanup worktrees
- Background generation:
  - titles
  - oversight summaries
  - periodic oversight while running
- Tracks background goroutines for safe shutdown.

### `internal/handler` (HTTP + automation control plane)

- REST API for tasks, execution, git, env, config, files, instructions.
- SSE endpoints for task deltas, logs, git status.
- In-process automation toggles:
  - `autopilot`
  - `autotest`
  - `autosubmit`
  - `ideation` (+ interval scheduling)
- Diff cache and API-oriented data shaping.
- SSRF-hardening for user-provided gateway base URLs.

### `internal/envconfig`

- Parses and updates `.env` while preserving unknown keys/comments.
- Supports sandbox routing config and operational settings.
- Used by runner and handlers at runtime (no restart required for most settings).

### `ui/` frontend

- Vanilla JS modules, no framework runtime.
- Drag-and-drop task board, modal detail views, timeline/flamegraph, oversight, diff, usage, sandbox monitor.
- SSE-driven updates with snapshot + typed delta events.
- Frontend tests under `ui/js/tests` with Vitest.

## Task Execution Lifecycle (Implementation Task)

1. Task created in `backlog`.
2. Transition to `in_progress` (manual drag or autopilot).
3. Runner ensures worktrees and board context (`/workspace/.tasks/board.json`).
4. Runner launches container for each turn.
5. Per-turn artifacts saved:
   - output files
   - timeline events
   - usage aggregation
6. Stop reason handling:
   - `max_tokens` / `pause_turn`: auto-continue turn loop.
   - `end_turn`: move to `committing`, run commit pipeline, then `done` or `failed`.
   - other/empty stop reason: move to `waiting` for human feedback.
7. Oversight generation:
   - sync for waiting path
   - background for done path
   - optional periodic while task is running.

### Test run lifecycle

- Triggered from waiting via `/api/tasks/{id}/test`.
- Same task moves back to `in_progress` with `IsTestRun=true`.
- Returns to `waiting` with `LastTestResult` = `pass` / `fail` / `unknown`.
- Generates dedicated test oversight (`oversight-test.json`).

## Background Automation Loops

- Auto-promoter:
  - respects `WALLFACER_MAX_PARALLEL` (default 5)
  - skips idea-agent tasks
  - enforces dependency readiness (`DependsOn` all done)
- Auto-tester:
  - runs tests for eligible waiting tasks when enabled
- Auto-submitter:
  - promotes verified waiting tasks to done when conflict-free and up-to-date
- Waiting-sync watcher:
  - auto-syncs waiting tasks behind default branch
- Ideation watcher:
  - schedules/enqueues idea-agent tasks based on enable flag + interval

## API Surface (High-Level)

Core groups in `buildMux`:

- UI/static:
  - `GET /`
- Debug:
  - `GET /api/debug/health`
  - `GET /api/debug/spans`
- Config/env/instructions:
  - `GET/PUT /api/config`
  - `GET/PUT /api/env`
  - `POST /api/env/test`
  - `GET/PUT /api/instructions`
  - `POST /api/instructions/reinit`
- Tasks:
  - CRUD: `GET/POST/PATCH/DELETE /api/tasks...`
  - execution actions: feedback, done, cancel, resume, sync, test
  - refinement: start/cancel/logs/apply/dismiss
  - oversight/spans/diff/events/logs/outputs
  - archive/unarchive + archive-all
  - generation helpers: missing titles/oversight
- Streams:
  - `GET /api/tasks/stream`
  - `GET /api/git/stream`
  - `GET /api/tasks/{id}/logs`
  - `GET /api/tasks/{id}/refine/logs`
- Git workspace ops:
  - status/push/sync/rebase/branches/checkout/create-branch
- Ops/observability:
  - `GET /api/containers`
  - `GET /api/files`
  - `GET /api/usage`
  - ideation status/trigger/cancel

See `docs/internals/orchestration.md` for endpoint-level behavior.

## Data Model Highlights

- `TaskStatus`: `backlog`, `in_progress`, `waiting`, `committing`, `done`, `failed`, `cancelled`
- `TaskKind`: regular task or `idea-agent`
- `archived` is a flag (not a status)
- Optional per-activity sandbox overrides on each task
- `DependsOn` DAG support for gated autopromotion

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
  - if commit landed after last task update -> recover to `done`
  - else -> mark `failed`
- `in_progress` tasks:
  - container still running -> keep running and monitor
  - container gone -> move to `waiting` for operator decision

## Security and Hardening

- Base URL validation rejects unsafe hosts/schemes for gateway endpoints.
- DNS + dial-time SSRF checks block private/loopback/link-local targets.
- Output file serving guards against path traversal.
- Task/container correlation uses labels (`wallfacer.task.id`) for safer lookup.

## Configuration Model

### Runtime flags

- `-addr`
- `-data`
- `-container`
- `-image`
- `-env-file`
- `-no-browser`
- `-log-format`

### Important env keys

- Auth/models:
  - `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`
  - `OPENAI_API_KEY`, `OPENAI_BASE_URL`
  - `CLAUDE_DEFAULT_MODEL`, `CLAUDE_TITLE_MODEL`
  - `CODEX_DEFAULT_MODEL`, `CODEX_TITLE_MODEL`
- Ops:
  - `WALLFACER_MAX_PARALLEL`
  - `WALLFACER_OVERSIGHT_INTERVAL`
  - `WALLFACER_AUTO_PUSH`
  - `WALLFACER_AUTO_PUSH_THRESHOLD`
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
