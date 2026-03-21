# Getting Started

This guide walks through installing Wallfacer, connecting it to credentials, and running your first task.

## Prerequisites

- **Go 1.25+** — [go.dev](https://go.dev/) (only needed if building from source; pre-built binaries are available)
- **Podman** or **Docker** — Wallfacer auto-detects whichever is available
- **A Claude credential** — either a Claude Pro/Max OAuth token or an Anthropic API key (see below)
- **Optional Codex credential** — either host Codex auth cache (`~/.codex/auth.json`) or `OPENAI_API_KEY`
- **Git** — recommended; non-git directories work as workspaces but git features (worktrees, diff, auto-push) are unavailable

## Step 1 — Get a Claude Credential

You need one of:

**Option A — OAuth token (Claude Pro or Max subscription)**

```bash
claude setup-token
```

This prints a token; copy it. If you do not have the `claude` CLI, install it first via [claude.ai/download](https://claude.ai/download) or `npm install -g @anthropic-ai/claude-code`.

**Option B — Anthropic API key**

Generate one at [console.anthropic.com](https://console.anthropic.com/) → API Keys. Keys start with `sk-ant-...`.

## Step 2 — Get the Binary

**Option A — Download a pre-built binary (no Go required)**

Download the binary for your platform from the [latest release](https://github.com/changkun/wallfacer/releases/latest):

```bash
# Example for macOS ARM (Apple Silicon)
curl -L https://github.com/changkun/wallfacer/releases/latest/download/wallfacer-darwin-arm64 -o wallfacer
chmod +x wallfacer
```

Available binaries: `wallfacer-darwin-arm64`, `wallfacer-darwin-amd64`, `wallfacer-linux-arm64`, `wallfacer-linux-amd64`.

**Option B — Install via Go (requires Go 1.25+)**

```bash
go install changkun.de/x/wallfacer@latest
```

The binary is installed to `$GOPATH/bin` (or `$HOME/go/bin` by default).

**Option C — Build from source (requires Go 1.25+)**

```bash
git clone https://github.com/changkun/wallfacer.git
cd wallfacer
go build -o wallfacer .
```

## Step 3 — Start Wallfacer

Pass the directories of the projects you want to work on:

```bash
./wallfacer run ~/projects/myapp
```

Multiple workspaces:

```bash
./wallfacer run ~/projects/myapp ~/projects/mylib
```

No argument defaults to the current directory:

```bash
./wallfacer run
```

Start with no active workspaces (configure them later in the UI):

```bash
./wallfacer run -no-workspaces
```

The browser opens automatically to `http://localhost:8080`. You should see a task board with four columns.

The sandbox image (`ghcr.io/changkun/wallfacer:latest`) is pulled automatically the first time a task runs. This is a one-time download (~1 GB).

## Step 4 — Configure Your Credential

Open **Settings → API Configuration** in the browser and enter your credential:

- Paste your OAuth token into `CLAUDE_CODE_OAUTH_TOKEN`, or
- Paste your API key into `ANTHROPIC_API_KEY`

You only need one of the two. Changes take effect on the next task without a server restart.

> You can also edit `~/.wallfacer/.env` directly if you prefer.

### Optional: Enable Codex Sandbox

Wallfacer supports two Codex auth modes:

1. **Host auth cache (recommended)**
   If `~/.codex/auth.json` exists on your host machine, Wallfacer validates it at startup and enables Codex automatically.
2. **API key fallback**
   Set `OPENAI_API_KEY` in **Settings → API Configuration** and run **Test (Codex)** once.

## Verify the Setup

1. Open **Settings → API Configuration** and confirm your credential is listed (tokens are masked).
2. Create a test task: click **+ New Task**, enter a short prompt, and click Add.
3. Drag the card to **In Progress**. A sandbox container starts; live log output appears in the task detail panel.

If the task fails immediately, check:

- The credential is correct (re-check in **Settings → API Configuration**)
- The container runtime (Podman or Docker) is running and accessible to your user
- Network access is available (the sandbox image is pulled from `ghcr.io` on first use)

## Configuration Reference

All configuration lives in `~/.wallfacer/.env`, which is auto-generated on first run. The server re-reads this file before each container launch, so changes take effect on the next task without a server restart. You can edit variables directly in the file or from **Settings → API Configuration** in the web UI.

### Claude Code Variables

| Variable | Required | Description |
|---|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | one of these two | OAuth token from `claude setup-token` (Claude Pro/Max) |
| `ANTHROPIC_API_KEY` | one of these two | API key from console.anthropic.com |
| `ANTHROPIC_BASE_URL` | no | Custom API endpoint (proxy, alternative provider). When set, Wallfacer queries `{base_url}/v1/models` to populate the model selection dropdown |
| `ANTHROPIC_AUTH_TOKEN` | no | Bearer token for LLM gateway proxy authentication |
| `CLAUDE_DEFAULT_MODEL` | no | Default model passed to task containers; omit to use the Claude Code default |
| `CLAUDE_TITLE_MODEL` | no | Model for background title generation; falls back to `CLAUDE_DEFAULT_MODEL` |

### OpenAI Codex Variables (Optional)

Requires selecting Codex as the task sandbox. The Codex sandbox image is pulled automatically on first use.

| Variable | Required | Description |
|---|---|---|
| `OPENAI_API_KEY` | no* | OpenAI API key. Not required when valid host auth cache exists at `~/.codex/auth.json` |
| `OPENAI_BASE_URL` | no | Custom OpenAI-compatible base URL (default: `https://api.openai.com/v1`) |
| `CODEX_DEFAULT_MODEL` | no | Default model for Codex tasks (e.g. `codex-mini-latest`) |
| `CODEX_TITLE_MODEL` | no | Title generation model; falls back to `CODEX_DEFAULT_MODEL` |

\* If host auth cache is unavailable or invalid, `OPENAI_API_KEY` + successful **Test (Codex)** is required.

### Server & Operational Variables

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_SERVER_API_KEY` | — | Bearer token for server API authentication. When set, all API requests must include `Authorization: Bearer <key>` |
| `WALLFACER_MAX_PARALLEL` | `5` | Maximum concurrent tasks in autopilot mode |
| `WALLFACER_MAX_TEST_PARALLEL` | (inherits) | Maximum concurrent test runs |
| `WALLFACER_OVERSIGHT_INTERVAL` | `0` | Minutes between periodic oversight generation while a task runs (0 = only at completion) |
| `WALLFACER_AUTO_PUSH` | `false` | Enable automatic `git push` after task completion |
| `WALLFACER_AUTO_PUSH_THRESHOLD` | `1` | Minimum completed tasks before auto-push triggers |
| `WALLFACER_SANDBOX_FAST` | `true` | Enable fast-mode sandbox hints by default |
| `WALLFACER_CONTAINER_NETWORK` | — | Container network name |
| `WALLFACER_CONTAINER_CPUS` | — | Container CPU limit (e.g. `"2.0"`, empty = no limit) |
| `WALLFACER_CONTAINER_MEMORY` | — | Container memory limit (e.g. `"4g"`, empty = no limit) |
| `WALLFACER_WEBHOOK_URL` | — | Webhook URL for task state change notifications |
| `WALLFACER_WEBHOOK_SECRET` | — | HMAC secret for webhook signature verification |
| `WALLFACER_WORKSPACES` | — | Workspace paths (OS path-list separated); alternative to CLI arguments |
| `WALLFACER_ARCHIVED_TASKS_PER_PAGE` | (default) | Pagination size for archived tasks |
| `WALLFACER_TOMBSTONE_RETENTION_DAYS` | `7` | Days to retain soft-deleted task data before permanent removal |
| `WALLFACER_CONTAINER_CB_THRESHOLD` | `5` | Consecutive container runtime failures before the circuit breaker opens |
| `WALLFACER_CONTAINER_CB_OPEN_SECONDS` | `30` | Seconds the container circuit breaker stays open before probing |

### Sandbox Routing Variables

Route specific agent activities to different sandbox types (claude or codex):

| Variable | Description |
|---|---|
| `WALLFACER_DEFAULT_SANDBOX` | Default sandbox for all activities |
| `WALLFACER_SANDBOX_IMPLEMENTATION` | Sandbox for task implementation |
| `WALLFACER_SANDBOX_TESTING` | Sandbox for test verification |
| `WALLFACER_SANDBOX_REFINEMENT` | Sandbox for prompt refinement |
| `WALLFACER_SANDBOX_TITLE` | Sandbox for title generation |
| `WALLFACER_SANDBOX_OVERSIGHT` | Sandbox for oversight generation |
| `WALLFACER_SANDBOX_COMMIT_MESSAGE` | Sandbox for commit message generation |
| `WALLFACER_SANDBOX_IDEA_AGENT` | Sandbox for ideation agent |

### Server Flags

```bash
./wallfacer run [flags] [workspace...]
```

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `-addr` | `ADDR` | `:8080` | Listen address |
| `-data` | `DATA_DIR` | `~/.wallfacer/data` | Task data directory |
| `-container` | `CONTAINER_CMD` | auto-detected | Container runtime command (`podman` or `docker`) |
| `-image` | `SANDBOX_IMAGE` | `ghcr.io/changkun/wallfacer:latest` | Sandbox image name |
| `-env-file` | `ENV_FILE` | `~/.wallfacer/.env` | Env file passed to containers |
| `-no-browser` | — | `false` | Skip auto-opening the browser on start |
| `-no-workspaces` | — | `false` | Start with no active workspaces |
| `-log-format` | `LOG_FORMAT` | `text` | Log output format: `text` or `json` |

Run `./wallfacer run -help` for the full flag list.

The container runtime defaults to auto-detection: Wallfacer checks `/opt/podman/bin/podman`, then `podman` on `$PATH`, then `docker` on `$PATH`. Override with the `-container` flag or `CONTAINER_CMD` env var.

### Inspecting Configuration

```bash
./wallfacer env
```

Prints all recognized configuration variables and whether they are set, with credential values masked.

### Checking Board Status

```bash
./wallfacer status                          # Snapshot of current board state
./wallfacer status -watch                   # Live-updating view
./wallfacer status -json                    # Machine-readable JSON output
./wallfacer status -addr :9090              # Connect to a different server
```

### Attaching to a Running Task Container

```bash
./wallfacer exec <task-id-prefix>           # Attach an interactive shell to a running task container
./wallfacer exec <task-id-prefix> -- bash   # Explicit shell
./wallfacer exec --sandbox claude           # Open shell in a new sandbox container (no task)
```

The task ID prefix is the first few characters of the task UUID (shown on the card or in the detail panel).

## Next Steps

- [Usage Guide](usage.md) — how to create tasks, handle feedback, use autopilot, and manage results
- [Architecture](../internals/architecture.md) — system design and internals for contributors
