This file provides guidance when working with code in this repository.

## Project Overview

Wallfacer is a task-board runner for AI agents. It provides a web UI where tasks are created as cards, dragged to "In Progress" to trigger AI agent execution as a host process, and results are inspected when done.

**Architecture:** Browser → Go server (:8080) → per-task directory storage (`data/<uuid>/`). The server runs natively on the host and execs the `claude`/`codex` CLI directly via `os/exec`. Each task gets its own git worktree for isolation.

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
- **Every spec MUST have valid YAML frontmatter** matching the spec document model (see `specs/local/spec-coordination/spec-document-model.md`). Required fields: `title`, `status`, `depends_on`, `affects`, `effort`, `created`, `updated`, `author`, `dispatched_task_id`.
- **Assess cross-impacts** with existing specs. If a new spec modifies interfaces defined in foundations (`SandboxBackend`, `StorageBackend`), it must declare the dependency.
- **Update `specs/README.md`** when adding a spec — add it to the appropriate track table, the status overview, and the dependency graph.

## Build & Run Commands

```bash
make build          # Full gate: fmt + lint (Go + JS) + ts build + binary
make build-binary   # Build just the Go binary (no fmt/lint)
make lint           # Lint only (fastest way to catch style regressions)
make fmt            # Format Go and JS in place
make test           # lint + backend tests + frontend tests (matches CI)
make test-backend   # go test ./...
make test-frontend  # vitest in ui/
make server         # Build and run the Go server natively
make ui-css         # Regenerate Tailwind CSS from UI sources
make api-contract   # Regenerate API route artifacts from apicontract/routes.go
make e2e-lifecycle              # E2E: task lifecycle (requires running server)
make e2e-dependency-dag WORKSPACE=/path/to/repo  # E2E: dependency DAG with conflicts
```

**Preferred loop — use `make` targets, not raw `go` invocations.** The `make` targets run gofmt, golangci-lint, and Biome; raw `go build`/`go vet` skip lint and can commit code that fails CI.

CLI usage (after `make build-binary`):

```bash
wallfacer run                                # Start server, restore last workspace group
wallfacer run -addr :9090 -no-browser        # Custom port, no browser
wallfacer doctor                             # Check prerequisites and config
wallfacer status [-watch]                    # Print board state
wallfacer spec validate [-json] [path...]    # Validate spec frontmatter
wallfacer spec new specs/local/foo.md        # Scaffold a spec with valid defaults
wallfacer auth login [--org=<uuid>]          # Local-mode sign-in (RFC 8628 device code)
wallfacer auth whoami                        # Print the saved principal_id / org_id
wallfacer auth logout                        # Remove the locally stored token
```

Local-mode token storage is `<UserConfigDir>/latere/token.json`, shared with the `latere` CLI: signing in via either tool carries over to the other. The local-mode SPA can also drive the same device-code flow without leaving the desktop window via the `POST /api/auth/device/start`, `GET /api/auth/device/poll`, and `POST /api/auth/device/cancel` endpoints (see `internal/handler/device_auth.go`).

## Server Development

The Go source lives at the top level. Module path: `changkun.de/x/wallfacer`. Go version: 1.25.7. The server uses `net/http` stdlib routing (Go 1.22+ pattern syntax) with no framework.

Key server files:
- `main.go` — Tiny entry point: embed FS declarations and subcommand dispatch
- `internal/cli/` — CLI subcommand implementations (run, doctor, status, spec, auth, web, desktop) and shared helpers
- `internal/apicontract/` — Single source of truth for all HTTP API routes; generates `ui/js/generated/routes.js`
- `internal/handler/` — HTTP API handlers (one file per concern)
- `internal/oauth/` — OAuth 2.0 PKCE flow engine, ephemeral callback server, provider configs (Claude, Codex)
- `internal/runner/` — Host-process orchestration via `os/exec`; task execution loop; commit pipeline; usage tracking; worktree sync; title generation; oversight; refinement; ideation; auto-retry; circuit breaker
- `internal/store/` — Per-task directory persistence, data models, event sourcing, soft delete, search index; see `docs/internals/data-and-storage.md`
- `internal/envconfig/` — `.env` file parsing and atomic update
- `internal/gitutil/` — Git utility operations (ops, repo, status, stash, worktree)
- `internal/workspace/` — Workspace manager; scopes data by workspace key; supports runtime workspace switching with concurrent multi-group execution
- `internal/constants/` — Consolidated system parameters: timeouts, intervals, retry counts, size limits, concurrency caps, pagination defaults
- `internal/executor/` — Host process backend: spawn/stream/wait/kill the agent CLI; launch spec and event-stream parsing
- `internal/harness/` — Harness abstraction: per-CLI argv building, NDJSON event parsing, and auth env (Claude, Codex)
- `internal/prompts/` — System prompt templates (title, commit, refinement, oversight, test, ideation, conflict, instructions) and workspace-level AGENTS.md management
- `ui/index.html` + `ui/js/` — Task board UI (vanilla JS + Tailwind CSS + Sortable.js)

## API Routes

All routes are defined in `internal/apicontract/routes.go` — that file is the single source of truth. See `docs/internals/api-and-transport.md` for full details on each endpoint.

Categories:
- **Debug & Monitoring** — `/api/debug/*` (health, spans, runtime, board)
- **Configuration** — `/api/config`, `/api/env`, `/api/env/test`
- **Workspace Management** — `/api/workspaces*`
- **Instructions** — `/api/instructions*`
- **System Prompt Templates** — `/api/system-prompts*`
- **Prompt Templates** — `/api/templates*`
- **Tasks** — `/api/tasks*` (CRUD, lifecycle actions, feedback, archive, search, summaries, deleted, events, diff, outputs, logs SSE, turn-usage, spans, oversight, board manifest)
- **Git** — `/api/git/*` (status, push, sync, rebase, branches, checkout, open-folder)
- **Agents** — `/api/agents*` (built-in + user YAML under `~/.wallfacer/agents/`; fsnotify-watched)
- **Flows** — `/api/flows*` (built-in + user YAML under `~/.wallfacer/flows/`; built-ins: `implement`, `brainstorm`, `test-only`)
- **Ideation** — `/api/ideate` (routine-backed; `Tags=["system:ideation"]`, `RoutineSpawnFlow=brainstorm`)
- **Routines** — `/api/routines*` (cron-like scheduler in `internal/routine/`; routine cards are tasks, deleted via `/api/tasks/{id}`)
- **Spec Tree** — `/api/specs/*` (tree, stream, dispatch, undispatch, archive, unarchive)
- **Planning Sandbox** — `/api/planning*` (sandbox lifecycle, threads, messages, SSE stream, interrupt, undo, slash commands)
- **Usage & Statistics** — `/api/usage`, `/api/stats`, `/api/files`
- **Sandbox Images** — `/api/images*` (cache check, pull, delete, SSE pull progress)
- **File Explorer** — `/api/explorer/*` (tree, stream, file read/write)
- **Terminal** — `/api/terminal/ws` WebSocket (multi-session host shell tabs; not in apicontract; `?token=` auth)
- **OAuth Authentication** — `/api/auth/{provider}/*` for `claude` and `codex`
- **Admin** — `/api/admin/rebuild-index`

Key constraints to remember when touching task creation:
- `POST /api/tasks` and `POST /api/tasks/batch` **do not accept `sandbox` or `sandbox_by_activity`** — harness lives on the agent definition; per-task sandbox overrides go via `PATCH /api/tasks/{id}` after creation.
- `flow` defaults to `implement` when omitted.

## Task Lifecycle

States: `backlog` → `in_progress` → `committing` → `done` | `waiting` | `failed` | `cancelled`. Tasks can also be marked `archived` (boolean flag on done/cancelled tasks, not a separate state).

See `docs/internals/task-lifecycle.md` for the full state machine, turn loop, and data models.

- Drag Backlog → In Progress triggers `runner.Run()` in a background goroutine
- Claude `end_turn` → commit pipeline (`committing` state) → Done
- Empty stop_reason → Waiting (needs user feedback)
- `max_tokens`/`pause_turn` → auto-continue in same session
- Feedback on Waiting → resumes execution
- "Mark as Done" on Waiting → Done + auto commit-and-push
- "Cancel" on Backlog/In Progress/Waiting/Failed/Done → Cancelled; kills the task process, discards worktrees
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
- **Usage tracking** accumulates input/output tokens, cache tokens, and cost across turns; per-sub-agent breakdown; per-turn records available via `/api/tasks/{id}/turn-usage`
- **Host execution** execs the agent CLI via `os/exec` with the task's git worktree as the working directory
- **Workspace AGENTS.md** delivered to the agent via `--append-system-prompt` (path passed as `WALLFACER_INSTRUCTIONS_PATH`)
- **Oversight summaries** generated asynchronously when tasks reach waiting/done/failed
- **System prompt templates** are overridable built-in prompts (`internal/prompts/*.tmpl`); users can customize via the UI or API
- **Auto-retry** with per-failure-category budget
- **Cost/token budgets** via `MaxCostUSD` and `MaxInputTokens` per task
- **Failure categorization** classifies failures (timeout, budget_exceeded, worktree_setup, container_crash, agent_error, sync_error, unknown)
- **Frontend** uses SSE for live updates; escapes HTML to prevent XSS
- **No framework** on backend (stdlib `net/http`) or frontend (vanilla JS)
- **Server API key** authentication via `WALLFACER_SERVER_API_KEY`
- **Circuit breaker** for agent process launches (`WALLFACER_CONTAINER_CB_THRESHOLD`)

## Workspace AGENTS.md (Instructions)

Each unique combination of workspace directories gets its own `AGENTS.md` in `~/.wallfacer/instructions/`, identified by a SHA-256 fingerprint of the sorted workspace paths (order-independent).

On first run the file is created from the `instructions.tmpl` template (overridable) plus a reference list of per-repo `AGENTS.md`/`CLAUDE.md` paths. Users can edit it from **Settings → AGENTS.md → Edit** or rebuild from repo files via **Re-init**. Delivered to every task agent via `--append-system-prompt` (path passed as `WALLFACER_INSTRUCTIONS_PATH`).

## Configuration

Full reference: `docs/guide/configuration.md` and `docs/cloud/README.md`. All settings can be edited from **Settings → API Configuration** in the UI (calls `PUT /api/env`).

`~/.wallfacer/.env` must contain at least one of:
- `CLAUDE_CODE_OAUTH_TOKEN` — OAuth token from `claude setup-token`
- `ANTHROPIC_API_KEY` — direct API key from console.anthropic.com

Commonly-tuned variables (full list in `docs/guide/configuration.md`):
- `WALLFACER_MAX_PARALLEL` — concurrent task cap (default: 5)
- `WALLFACER_AUTO_PUSH` / `WALLFACER_AUTO_PUSH_THRESHOLD` — auto-push controls
- `WALLFACER_WORKSPACES` — workspace paths (OS path-list separated)
- `WALLFACER_SERVER_API_KEY` — bearer token for server API authentication
- `WALLFACER_CLOUD` — gate for cloud-only UI surfaces; shell env only (defaults `false`)
- `WALLFACER_LEGACY_UI` — when truthy, fall back to the vanilla-JS UI in `ui/` instead of the default Vue SPA in `frontend/dist/`. Older `WALLFACER_VUE_UI=false` is honoured for back-compat.
- `AUTH_JWKS_URL` / `AUTH_ISSUER` — JWT validation in cloud mode (auto-derived from `AUTH_URL` when unset)

**Cloud vs local partition.** `WALLFACER_CLOUD` is the single gate between local-only functionality and cloud surfaces. Cloud adds identity, not feature gates — task execution is identical in both modes. Full details in [`docs/cloud/README.md`](docs/cloud/README.md). Long-range design in [`specs/shared/authentication.md`](specs/shared/authentication.md).

## End-to-end integration tests

Two E2E scripts in `scripts/` exercise the full task lifecycle against a running wallfacer server with the real agent CLIs. Run after changes to `internal/runner/`, `internal/handler/tasks*.go`, `internal/handler/execute.go`, `internal/gitutil/`, harness/executor changes, or before releases.

```bash
wallfacer run &                              # requires a running server with valid credentials
make e2e-lifecycle                           # both sandboxes (Claude + Codex)
make e2e-lifecycle SANDBOX=claude            # one sandbox
make e2e-dependency-dag WORKSPACE=/path      # fan-out/fan-in DAG with conflict resolution
```

`e2e-lifecycle` checks task creation, execution, commit pipeline, archive, and process cleanup. `e2e-dependency-dag` checks batch creation with dependencies, autopilot promotion, max-parallel caps, conflict resolution during rebase, autosync, and autosubmit. Tunables: `WALLFACER_URL`, `WALLFACER_SERVER_API_KEY`, `WALLFACER_TEST_TIMEOUT`.

## Bug fixes require a reproducible test

Every bug fix MUST ship with a regression test that **fails without the fix and passes with it**. No exceptions — backend, frontend, CLI alike. Before applying the fix:

1. Write a test that reproduces the bug and confirm it fails on the unpatched code.
2. Apply the minimal fix.
3. Confirm the same test now passes.
4. Commit the test and the fix together (single logical change).

If a bug genuinely cannot be covered by an automated test (e.g. a rare race reproducible only under manual load), document in the commit message *why* and what manual reproduction steps were used.

## Implementation checklist

Every implementation task MUST complete all three steps before finishing:

1. **Add tests** — Cover the happy path and at least one error/edge case. **Bug fixes must always include a reproducible regression test** (see above). Run `make test` before committing — it wraps `make lint` plus backend + frontend tests, matching CI.

2. **Update docs** — If your change adds, removes, or modifies any API route, CLI flag, env variable, data model field, or user-visible behavior, update the corresponding documentation. The user manual lives in `docs/guide/` (indexed by `docs/guide/usage.md`); internals in `docs/internals/`. If a new feature doesn't fit any existing guide, create one and add it under `## Reading Order` in `usage.md` — the server parses the first `[Title](file.md)` link under each `###` heading to build the docs sidebar. Also update `AGENTS.md` and `CLAUDE.md` as needed.

3. **Reflect on codebase health** — After implementing, review the files you touched and their immediate surroundings. If you spot a small, safe refactor (dead code, unclear naming, duplicated logic, missing error handling) directly related to your change, include it. Keep refactoring minimal and scoped.

## Commit and push strategy

- Before committing, always run `make build` (or at minimum `make fmt && make lint`) and fix any issues they report. `make build` is the full gate.
- Keep commits small and focused on one logical change.
- Do not include unrelated changes in the same commit.
- Use scoped, imperative commit messages matching existing style, e.g. `internal/runner: ...`, `ui: ...`, `docs: ...`.
- Stage only the files required for that change, then commit once.
- Push only once after creating the commit. If follow-up work is needed, create a new small commit and push again.
