# ⚙️ Configuration & Customization

Wallfacer is configured through the Settings UI, environment variables in `~/.wallfacer/.env`, CLI flags, and file-based overrides for system prompts and workspace instructions. The `.env` file is auto-generated on first run with commented-out defaults; edit it directly or use the Settings UI. Most settings take effect on the next task run without restarting the server.

---

## 🚀 Essentials

### Opening Settings

Open the Settings modal by clicking the gear icon in the top-right corner of the task board. The modal contains six tabs: Appearance, Execution, Sandbox, Workspace, Trash, and About.

### 🎨 Appearance

**Theme** -- Choose between Light, Dark, or Auto (follows the operating system preference). The theme applies to the current browser session.

**Done Column** -- Toggle "Show archived tasks" to display or hide archived completed tasks on the board.

### 🔑 Setting Up Sandbox Credentials

At minimum, you need one of these credentials configured in **Settings > Sandbox**:

**Claude configuration:**
- **Sign in with Claude** button -- starts an OAuth flow: opens your browser to authenticate, then stores the token automatically. This is the easiest way to set up credentials.
- OAuth Token (`CLAUDE_CODE_OAUTH_TOKEN`) -- alternatively, paste a token from `claude setup-token`; takes precedence when both credentials are set
- API Key (`ANTHROPIC_API_KEY`) -- direct key from console.anthropic.com
- **Test** button -- runs a quick sandbox connectivity check to verify your credentials work. If the test detects an invalid or expired token and OAuth is available, a **Sign in again** button appears inline.

**Codex configuration:** similarly, use the **Sign in with OpenAI** button for OAuth or paste an API key manually.

The sign-in buttons are hidden when a custom base URL is configured (custom endpoints don't use standard OAuth). On first launch with no credentials for any provider, a prompt guides you to set up credentials.

All changes are written to `~/.wallfacer/.env` and take effect on the next task run. Leave token fields blank to preserve the existing value.

### Container Images

The **Container Images** section at the top of **Settings > Sandbox** shows whether each sandbox image (Claude and Codex) is cached locally. Images are pulled automatically on first task run, but you can also manage them manually:

- **Pull** -- Download a missing image from the registry (~1 GB).
- **Re-pull** -- Update a cached image to the latest version.
- Pull progress streams live in the settings panel with a phase summary (resolving, downloading layers, writing manifest) and a layer counter.

You can also check image status from the command line with `wallfacer doctor`.

### Key Environment Variables

All configuration lives in `~/.wallfacer/.env` (auto-generated on first run). The server re-reads this file before each container launch, so changes take effect on the next task without a server restart.

| Variable | Required | Description |
|---|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | one of these two | OAuth token from `claude setup-token` (Claude Pro/Max subscription) |
| `ANTHROPIC_API_KEY` | one of these two | API key from console.anthropic.com (starts with `sk-ant-...`) |
| `WALLFACER_MAX_PARALLEL` | no (default: `5`) | Maximum concurrent tasks auto-promoted to In Progress |

### CLI Basics

```bash
wallfacer run                           # Start server, restore last workspace group
wallfacer run -addr :9090 -no-browser   # Custom port, no browser
wallfacer status                        # Print board state to terminal
wallfacer status -watch                 # Live-updating board state
wallfacer doctor                        # Check prerequisites and config
```

### 📐 Plan Mode

See [Designing Specs](designing-specs.md) for the full Plan mode guide.

### 💬 Planning Chat

See [Exploring Ideas](exploring-ideas.md) for the full planning chat guide.

### ⌨️ Keyboard Shortcuts

| Key | Context | Action |
|---|---|---|
| `N` | Board | Open new task form |
| `?` | Global | Show keyboard shortcuts help |
| `E` | Global | Toggle file explorer |
| `P` | Global | Toggle board/Plan mode |
| `C` | Plan mode | Toggle chat pane |
| `D` | Plan mode | Dispatch focused spec as a task |
| `B` | Plan mode | Break down focused spec into sub-specs |
| `` ` `` | Global | Toggle terminal panel |

---

## 🔧 Advanced Topics

### ⚡ Execution Settings

**Parallel Tasks** -- Set the maximum number of tasks that run concurrently in the In Progress column (1--20). Corresponds to `WALLFACER_MAX_PARALLEL`.

**Archived Tasks** -- Number of archived task items to load per scroll page (1--200). Corresponds to `WALLFACER_ARCHIVED_TASKS_PER_PAGE`.

**Oversight Interval** -- Minutes between periodic oversight summary generation while a task is running (0--120). Setting this to 0 means oversight is only generated when the task reaches a terminal state. Corresponds to `WALLFACER_OVERSIGHT_INTERVAL`.

**Auto Push** -- Enable automatic `git push` after a task's commit pipeline completes. When enabled, an additional threshold field appears: push only triggers when the workspace is at least N commits ahead of upstream. Corresponds to `WALLFACER_AUTO_PUSH` and `WALLFACER_AUTO_PUSH_THRESHOLD`.

**Brainstorm** -- Enable the brainstorm (ideation) agent and set its recurrence interval. Options range from "immediately" to "every 24h". The brainstorm agent analyses repositories and proposes tasks tagged `idea-agent`. A "Run now" button triggers an immediate brainstorm.

**Task Titles** -- Select a batch limit (5, 10, 25, 50, or All) and click "Generate Missing" to auto-generate titles for untitled tasks using a lightweight model call.

**Trace Oversight** -- Select a batch limit and click "Generate Missing" to generate oversight summaries for tasks that lack them.

### Codex Sandbox Configuration

**Codex configuration:**
- API Key (`OPENAI_API_KEY`) -- optional when host `~/.codex/auth.json` is available
- Base URL (`OPENAI_BASE_URL`) -- optional custom OpenAI-compatible endpoint
- Default Model (`CODEX_DEFAULT_MODEL`) -- model for Codex tasks
- Title Model (`CODEX_TITLE_MODEL`) -- falls back to the Codex default model
- **Test** button -- runs a Codex connectivity check

### 📦 Sandbox Routing

**Global Sandbox Routing** -- Select the default sandbox type and override the sandbox for individual activities: Implementation, Testing, Refinement, Title generation, Oversight summary, Commit message, and Idea agent. Each dropdown offers the available sandbox types (claude, codex) or "default".

Wallfacer supports two sandbox types, both backed by the same unified container image (`sandbox-agents:latest`). The container's entrypoint dispatches to the right CLI based on the `WALLFACER_AGENT` env var the runner sets per task:

- **Claude** -- runs Claude Code CLI (`WALLFACER_AGENT=claude`). Requires either `CLAUDE_CODE_OAUTH_TOKEN` or `ANTHROPIC_API_KEY`.
- **Codex** -- runs OpenAI Codex CLI (`WALLFACER_AGENT=codex`). Requires `OPENAI_API_KEY` or host `~/.codex/auth.json`.

Each task can be assigned a specific sandbox type when created or edited. The task-level sandbox selection overrides the global default for that task's implementation run.

Different agent activities can be routed to different sandbox types. For example, you could run implementation tasks on Claude but use Codex for title generation. Configure this in **Settings > Sandbox > Global Sandbox Routing** or via the `WALLFACER_SANDBOX_*` environment variables.

The seven configurable activities are:
1. **Implementation** -- the main task execution
2. **Testing** -- test verification agent
3. **Refinement** -- prompt refinement agent
4. **Title generation** -- automatic task title generation
5. **Oversight summary** -- trace oversight analysis
6. **Commit message** -- commit message generation
7. **Idea agent** -- brainstorm/ideation agent

When an activity-specific override is not set, it falls back to `WALLFACER_DEFAULT_SANDBOX`. When that is also unset, the sandbox is determined by the task's own sandbox field or the server default (Claude).

Route specific agent activities to different sandbox types (`claude` or `codex`):

| Variable | Description |
|---|---|
| `WALLFACER_DEFAULT_SANDBOX` | Default sandbox for all activities |
| `WALLFACER_SANDBOX_IMPLEMENTATION` | Override for task implementation |
| `WALLFACER_SANDBOX_TESTING` | Override for test verification |
| `WALLFACER_SANDBOX_REFINEMENT` | Override for prompt refinement |
| `WALLFACER_SANDBOX_TITLE` | Override for title generation |
| `WALLFACER_SANDBOX_OVERSIGHT` | Override for oversight generation |
| `WALLFACER_SANDBOX_COMMIT_MESSAGE` | Override for commit message generation |
| `WALLFACER_SANDBOX_IDEA_AGENT` | Override for ideation agent |

### 🖥️ Container Resource Limits

Limit the CPU and memory available to each task container via `WALLFACER_CONTAINER_CPUS` and `WALLFACER_CONTAINER_MEMORY`. These correspond to the `--cpus` and `--memory` flags of `podman run` / `docker run`. Leave empty for no limit.

- Container CPUs (`WALLFACER_CONTAINER_CPUS`) -- e.g. `2.0`; leave empty for no limit
- Container Memory (`WALLFACER_CONTAINER_MEMORY`) -- e.g. `4g`; leave empty for no limit

### Fast Mode

When `WALLFACER_SANDBOX_FAST` is `true` (the default), Wallfacer passes fast-mode hints to the sandbox agent. Set to `false` to disable.

**Enable /fast** -- Toggle fast-mode sandbox hints (`WALLFACER_SANDBOX_FAST`).

### 📋 Full Environment Variables Reference

All configuration lives in `~/.wallfacer/.env` (auto-generated on first run). The server re-reads this file before each container launch, so changes take effect on the next task without a server restart.

#### Authentication

| Variable | Required | Description |
|---|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | one of these two | OAuth token from `claude setup-token` (Claude Pro/Max subscription) |
| `ANTHROPIC_API_KEY` | one of these two | API key from console.anthropic.com (starts with `sk-ant-...`) |
| `ANTHROPIC_AUTH_TOKEN` | no | Bearer token for LLM gateway proxy authentication |
| `ANTHROPIC_BASE_URL` | no | Custom Anthropic-compatible API endpoint; when set, Wallfacer queries `{base_url}/v1/models` to populate the model dropdown |

#### Models

| Variable | Default | Description |
|---|---|---|
| `CLAUDE_DEFAULT_MODEL` | (Claude Code default) | Default model passed to task containers |
| `CLAUDE_TITLE_MODEL` | (falls back to `CLAUDE_DEFAULT_MODEL`) | Model for background title generation |

#### OpenAI Codex

| Variable | Required | Description |
|---|---|---|
| `OPENAI_API_KEY` | no\* | OpenAI API key; not required when valid host auth cache exists at `~/.codex/auth.json` |
| `OPENAI_BASE_URL` | no | Custom OpenAI-compatible base URL |
| `CODEX_DEFAULT_MODEL` | no | Default model for Codex tasks |
| `CODEX_TITLE_MODEL` | no | Title generation model; falls back to `CODEX_DEFAULT_MODEL` |

\* If host auth cache is unavailable, `OPENAI_API_KEY` plus a successful **Test (Codex)** is required.

#### Concurrency

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_MAX_PARALLEL` | `5` | Maximum concurrent tasks auto-promoted to In Progress |
| `WALLFACER_MAX_TEST_PARALLEL` | (inherits from `WALLFACER_MAX_PARALLEL`) | Maximum concurrent test runs |

#### Sandbox backend

Wallfacer runs tasks through one of two backends, selected at server start via the `--backend` flag on `wallfacer run`:

| Backend | Flag | How it runs | When to use |
|---|---|---|---|
| Container (default) | `--backend container` | `podman run` / `docker run` against the unified `sandbox-agents` image | You want filesystem isolation; you have a container runtime installed |
| Host | `--backend host` | Execs `claude` (and optionally `codex`) directly on the host | You already have the CLIs installed and don't want to install a container runtime or pull the sandbox image |

> **Host mode has no isolation.** Tasks run with your user's permissions and can touch any file your account can. Recommended for trusted machines only. The Settings → Sandbox tab surfaces a warning banner while host mode is active. See [Host mode](#host-mode) below.

#### Container

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_CONTAINER_NETWORK` | -- | Container network name |
| `WALLFACER_CONTAINER_CPUS` | (no limit) | CPU limit per container, e.g. `"2.0"` |
| `WALLFACER_CONTAINER_MEMORY` | (no limit) | Memory limit per container, e.g. `"4g"` |
| `WALLFACER_CONTAINER_CB_THRESHOLD` | `5` | Consecutive container runtime failures before the circuit breaker opens |
| `WALLFACER_CONTAINER_CB_OPEN_SECONDS` | `30` | Seconds the circuit breaker stays open before probing |
| `WALLFACER_TASK_WORKERS` | `true` | Enable per-task worker containers for container reuse. Each task gets a long-lived container that is reused across agent invocations (implementation turns, title, oversight, commit message). Set to `false` to always use ephemeral containers. |
| `WALLFACER_DEPENDENCY_CACHES` | `false` | Mount named volumes for dependency caches (`~/.npm`, `~/.cache/pip`, `~/.cargo/registry`, `~/.cache/go-build`) that persist across container restarts. Scoped per workspace group. |

#### Host mode

Set when running `wallfacer run --backend host`. These variables are optional; defaults resolve via `$PATH`.

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_HOST_CLAUDE_BINARY` | `exec.LookPath("claude")` | Explicit path to the Claude CLI binary |
| `WALLFACER_HOST_CODEX_BINARY` | `exec.LookPath("codex")` | Explicit path to the Codex CLI binary (optional; codex-typed tasks require it) |

**Install requirements:**

- `npm i -g @anthropic-ai/claude-code` for Claude.
- `npm i -g @openai/codex` for Codex (optional; skip if you only run Claude tasks).

**Build the server for host mode:**

```bash
make build-host   # fmt + lint + ts build + binary, no image pull
./wallfacer run --backend host
```

**Verify readiness:**

```bash
wallfacer doctor --backend host
```

Reports the resolved binary paths and `--version` output for each CLI. Missing codex is a soft warning (tasks routed to codex will fail; claude-only workflows still work).

**Known limitations:**

- `--resume` is a no-op for codex (codex's `exec` subcommand has no stable resume flag).
- Concurrent tasks default to `WALLFACER_MAX_PARALLEL=1` in host mode to avoid races on `~/.claude/__store.db` and `~/.codex/` shared state. Override with an explicit value to opt in.
- Windows is not supported natively — use container mode with Docker/Podman Desktop, or run wallfacer inside WSL2.
- No write containment: an agent can touch any file your user account can.

#### Automation

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_OVERSIGHT_INTERVAL` | `0` | Minutes between periodic oversight generation while a task runs (0 = only at completion) |
| `WALLFACER_AUTO_PUSH` | `false` | Enable automatic `git push` after task completion |
| `WALLFACER_AUTO_PUSH_THRESHOLD` | `1` | Minimum commits ahead of upstream before auto-push triggers |
| `WALLFACER_PLANNING_WINDOW_DAYS` | `30` | Default window (in days) for the planning-cost analytics display. `0` means all time. Only affects the UI's default period selection; the server always returns the full record set until the UI requests a narrower window via `?days=N`. |
| `WALLFACER_SANDBOX_FAST` | `true` | Enable fast-mode sandbox hints |
| `WALLFACER_TERMINAL_ENABLED` | `true` | Enable the integrated host terminal panel. The Terminal button in the status bar opens an interactive shell running on the host machine via WebSocket + PTY. Supports multiple concurrent sessions with a tab bar — click "+" to add sessions, click tabs to switch, click x to close. Set to `false` to disable. |

#### Data & Pagination

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_WORKSPACES` | -- | Workspace paths (colon-separated on Unix, semicolon on Windows); alternative to CLI arguments. On Windows, host paths are automatically translated for container volume mounts (Docker Desktop uses `/c/` prefix, Podman Desktop uses `/mnt/c/`). |
| `WALLFACER_ARCHIVED_TASKS_PER_PAGE` | `20` | Pagination size for archived tasks |
| `WALLFACER_TOMBSTONE_RETENTION_DAYS` | `7` | Days to retain soft-deleted task data before permanent removal |

#### Security

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_SERVER_API_KEY` | -- | Bearer token for server API authentication; when set, all API requests must include `Authorization: Bearer <key>` |

#### Cloud mode

Wallfacer can optionally sign the user in to [latere.ai](https://latere.ai) and display their avatar + username in the status bar. Cloud-mode documentation lives in [`docs/cloud/`](../cloud/) — start with [`docs/cloud/README.md`](../cloud/README.md) for the env-var reference, deployment constraints, and the cloud/local partition. The sign-in badge is hidden entirely when `WALLFACER_CLOUD` is unset, so local-only deployments are unchanged.

### 📝 System Prompt Templates

Wallfacer uses eight built-in Go template files to instruct agent activities:

| Template | Purpose |
|---|---|
| `title.tmpl` | Task title generation |
| `commit.tmpl` | Commit message generation |
| `test.tmpl` | Test verification agent |
| `refinement.tmpl` | Prompt refinement agent |
| `oversight.tmpl` | Oversight summary generation |
| `ideation.tmpl` | Brainstorm/ideation agent |
| `conflict.tmpl` | Merge conflict resolution |
| `instructions.tmpl` | Workspace instructions (AGENTS.md) generation |

#### Viewing and Editing

Open **Settings > Workspace > System Prompt Templates > Manage** to view all templates. Each template shows whether a user override exists. Click a template name to view its content and edit it.

Overrides are validated as Go templates before saving. If the template is invalid, the save is rejected with a parse error message.

#### Override Storage

User overrides are stored as `.tmpl` files in `~/.wallfacer/prompts/`. For example, overriding the title template creates `~/.wallfacer/prompts/title.tmpl`.

#### Restoring Defaults

Delete a user override via the template editor or the API (`DELETE /api/system-prompts/{name}`) to restore the embedded default.

### Workspace Instructions (AGENTS.md)

#### What AGENTS.md Is

Each unique set of workspaces gets its own `AGENTS.md` file stored in `~/.wallfacer/instructions/`. This file is mounted read-only into every task container at `/workspace/AGENTS.md`, where the agent picks it up automatically as context.

#### How Fingerprinting Works

The instructions file is identified by a SHA-256 hash of the sorted, absolute workspace paths. This means switching to workspaces `~/a` and `~/b` (in any order) shares the same instructions file, while `~/a`, `~/b`, and `~/c` together gets a separate one.

#### Initial Generation

On first run with a new workspace set, Wallfacer creates the `AGENTS.md` from:

1. A built-in default template with general agent guidance.
2. A reference list of per-repository `AGENTS.md` (or legacy `CLAUDE.md`) file paths so agents can read them on demand.

#### Editing

Open **Settings > Workspace > AGENTS.md > Edit** to modify the instructions in the web UI. Changes are saved to the fingerprinted file in `~/.wallfacer/instructions/`.

#### Re-Initializing

Click **Re-init** (or call `POST /api/instructions/reinit`) to regenerate the instructions from the default template and current repository files, discarding any manual edits.

### Prompt Templates

Prompt templates are reusable named prompt fragments for common task patterns.

#### Creating a Template

Open **Settings > Workspace > Prompt Templates > Manage**. Enter a name and body, then save. Templates are stored in `~/.wallfacer/templates.json`.

#### Using Templates

When creating a new task, select a template from the template picker to insert its body into the prompt field. You can then edit the inserted text before submitting.

#### Managing Templates

From the template manager, you can view all templates sorted by creation date and delete templates you no longer need.

### 🖥️ CLI Reference

#### wallfacer run

Start the task board server and open the web UI.

```
wallfacer run [flags] [workspace...]
```

**Positional arguments:**
- `workspace` -- directories to mount in the sandbox (default: current directory)

**Flags:**

| Flag | Env Var | Default | Description |
|---|---|---|---|
| `-addr` | `ADDR` | `:8080` | Listen address |
| `-data` | `DATA_DIR` | `~/.wallfacer/data` | Task data directory |
| `-container` | `CONTAINER_CMD` | auto-detected | Container runtime command (`podman` or `docker`) |
| `-image` | `SANDBOX_IMAGE` | `ghcr.io/latere-ai/sandbox-agents:latest` | Sandbox image name (same image serves both Claude and Codex; `WALLFACER_AGENT` selects the CLI) |
| `-env-file` | `ENV_FILE` | `~/.wallfacer/.env` | Env file passed to containers |
| `-no-browser` | -- | `false` | Skip auto-opening the browser |
| `-log-format` | `LOG_FORMAT` | `text` | Log output format: `text` or `json` |

Container runtime auto-detection order: `/opt/podman/bin/podman`, then `podman` on `$PATH`, then `docker` on `$PATH`.

#### wallfacer status

Print the current board state to the terminal.

```
wallfacer status [flags]
```

| Flag | Default | Description |
|---|---|---|
| `-addr` | `http://localhost:8080` | Server address (or `ADDR` env var) |
| `-watch` | `false` | Re-render every 2 seconds until Ctrl-C |
| `-json` | `false` | Emit raw JSON from `/api/tasks` for scripting |

**Examples:**

```bash
wallfacer status                  # Snapshot of current state
wallfacer status -watch           # Live-updating view
wallfacer status -json            # Machine-readable JSON output
wallfacer status -addr :9090      # Connect to a different server
```

#### wallfacer doctor

Check prerequisites and configuration. Displays config paths, then verifies credentials, container runtime (including responsiveness), sandbox images, and Git.

```
wallfacer doctor
```

Output uses `[ok]` for passing checks, `[!]` for issues that need fixing, and `[ ]` for optional items that are not configured. Credential values are masked.

#### wallfacer exec

Attach an interactive shell to a running task container, or open a shell in a new sandbox container.

```
wallfacer exec <task-id-prefix> [-- command...]
wallfacer exec --sandbox <claude|codex> [-- command...]
```

- `<task-id-prefix>` -- the first 8 or more hex characters of the task UUID (shown on task cards)
- `--sandbox` -- open a fresh sandbox container with the current directory mounted, without attaching to an existing task
- `command` -- command to run inside the container (default: `bash`; falls back to `sh` if bash is unavailable)

**Examples:**

```bash
wallfacer exec a1b2c3d4              # Attach to running task container
wallfacer exec a1b2c3d4 -- sh       # Use sh instead of bash
wallfacer exec --sandbox claude      # Open shell in a new Claude sandbox
wallfacer exec --sandbox codex       # Open shell in a new Codex sandbox
```

### 🛡️ Security

#### Server API Key Authentication

Set `WALLFACER_SERVER_API_KEY` to require bearer-token authentication on all API requests. When configured, every request must include the header:

```
Authorization: Bearer <your-api-key>
```

SSE (Server-Sent Events) endpoints accept the token as a `?token=` query parameter instead, since EventSource does not support custom headers.

The root page (`GET /`) is exempt from authentication to allow loading the UI, which then uses the token for subsequent API calls.

#### CSRF Protection

Wallfacer validates the `Origin` or `Referer` header on all state-changing requests (POST, PUT, PATCH, DELETE). The header host must match the server's listen address. Requests without either header (e.g., from non-browser clients like `curl`) are allowed through.

#### SSRF Hardening

Custom API base URLs (`ANTHROPIC_BASE_URL`, `OPENAI_BASE_URL`) are validated before being persisted:

- Only HTTPS URLs are accepted
- Bare IP addresses are rejected
- Single-label hostnames (e.g., `localhost`) are rejected
- Hostnames that resolve to private, loopback, or link-local IP addresses are rejected

#### Request Body Size Limits

Request bodies are limited to prevent abuse:

| Endpoint Category | Limit |
|---|---|
| Default | 1 MiB |
| Instructions (AGENTS.md) | 5 MiB |
| Feedback | 512 KiB |

### 🗑️ Trash Management

View soft-deleted tasks that are within the retention window (default: 7 days, controlled by `WALLFACER_TOMBSTONE_RETENTION_DAYS`). Each entry shows the task prompt and deletion timestamp. You can:

- **Restore** a task to return it to its pre-deletion state
- Wait for the retention window to expire for automatic permanent removal

Access via **Settings > Trash**.

### About

Displays version information, the project link (github.com/changkun/wallfacer), and license details.

Access via **Settings > About**.

### 📂 Workspace Settings

**Active Workspaces** -- Lists the directories currently mounted into task containers. Click **Change** to open the workspace picker and select different directories.

**Saved Workspace Groups** -- Previously used workspace combinations are saved automatically. Switch back to any saved group without rebuilding the folder set.

---

## See Also

- [Getting Started](getting-started.md) -- initial setup and first task
- [Usage Guide](usage.md) -- task creation, feedback, autopilot, and results
- [Circuit Breakers](circuit-breakers.md) -- container launch failure protection
- [Refinement & Ideation](refinement-and-ideation.md) -- prompt refinement and brainstorm agents
- [Architecture](../internals/architecture.md) -- system design for contributors
