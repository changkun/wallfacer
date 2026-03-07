# Getting Started

This guide walks through installing Wallfacer, connecting it to a Claude credential, and running your first task.

## Prerequisites

- **Go 1.25+** — [go.dev](https://go.dev/)
- **Podman** or **Docker** — Wallfacer auto-detects whichever is available
- **A Claude credential** — either a Claude Pro/Max OAuth token or an Anthropic API key (see below)
- **Git** — the projects you mount must be git repositories

## Step 1 — Get a Claude Credential

You need one of:

**Option A — OAuth token (Claude Pro or Max subscription)**

```bash
claude setup-token
```

This prints a token; copy it. If you do not have the `claude` CLI, install it first via [claude.ai/download](https://claude.ai/download) or `npm install -g @anthropic-ai/claude-code`.

**Option B — Anthropic API key**

Generate one at [console.anthropic.com](https://console.anthropic.com/) → API Keys. Keys start with `sk-ant-...`.

## Step 2 — Build the Sandbox Image

The sandbox image bundles Claude Code CLI, Go, Node, and Python inside an Ubuntu container. Build it once; rebuilding is only needed when the image definition changes.

```bash
make build
```

This builds both the Claude (`wallfacer:latest`) and OpenAI Codex (`wallfacer-codex:latest`) images. To build only the Claude image:

```bash
make build-claude
```

Building takes a few minutes the first time. Confirm the image exists afterward:

```bash
podman images wallfacer   # or: docker images wallfacer
```

## Step 3 — Build the Binary

```bash
go build -o wallfacer .
```

## Step 4 — Configure Your Credential

Run Wallfacer once to create the configuration directory and env file:

```bash
./wallfacer run
```

This creates `~/.wallfacer/.env` with placeholders. Stop the server (`Ctrl-C`) and edit the file:

```bash
# ~/.wallfacer/.env

# Option A — OAuth token (Claude Pro/Max)
CLAUDE_CODE_OAUTH_TOKEN=<your-token>

# Option B — API key
# ANTHROPIC_API_KEY=sk-ant-...
```

You only need to set one of the two credential variables. When both are set, the OAuth token takes precedence.

> You can also set credentials directly from the web UI under **Settings → API Configuration** — no file editing required.

## Step 5 — Start Wallfacer

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

The browser opens automatically to `http://localhost:8080`. You should see a Kanban board with five columns.

## Verify the Setup

1. Open **Settings → API Configuration** and confirm your credential is listed (tokens are masked).
2. Create a test task: click **+ New Task**, enter a short prompt, and click Add.
3. Drag the card to **In Progress**. A sandbox container starts; live log output appears in the task detail panel.

If the task fails immediately, check:

- The credential in `~/.wallfacer/.env` is correct
- The sandbox image was built (`wallfacer:latest` appears in `podman images` or `docker images`)
- The container runtime (Podman or Docker) is running and accessible to your user

## Configuration Reference

All configuration lives in `~/.wallfacer/.env`. The server re-reads this file before each container launch, so changes take effect on the next task without a server restart. You can also edit everything from **Settings → API Configuration** in the web UI.

### Claude Code Variables

| Variable | Required | Description |
|---|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | one of these two | OAuth token from `claude setup-token` (Claude Pro/Max) |
| `ANTHROPIC_API_KEY` | one of these two | API key from console.anthropic.com |
| `ANTHROPIC_BASE_URL` | no | Custom API endpoint (proxy, alternative provider). When set, Wallfacer queries `{base_url}/v1/models` to populate the model selection dropdown |
| `ANTHROPIC_AUTH_TOKEN` | no | Bearer token for LLM gateway proxy authentication |
| `WALLFACER_DEFAULT_MODEL` | no | Default model passed to task containers; omit to use the Claude Code default |
| `WALLFACER_TITLE_MODEL` | no | Model for background title generation; falls back to `WALLFACER_DEFAULT_MODEL` |
| `WALLFACER_MAX_PARALLEL` | no | Maximum number of tasks that run concurrently in autopilot mode (default: 5) |

### OpenAI Codex Variables (Optional)

Requires building the Codex image (`make build-codex`) and selecting the `wallfacer-codex` image per task.

| Variable | Required | Description |
|---|---|---|
| `OPENAI_API_KEY` | yes (for Codex) | OpenAI API key |
| `OPENAI_BASE_URL` | no | Custom OpenAI-compatible base URL (default: `https://api.openai.com/v1`) |
| `CODEX_DEFAULT_MODEL` | no | Default model for Codex tasks (e.g. `codex-mini-latest`) |
| `CODEX_TITLE_MODEL` | no | Title generation model; falls back to `CODEX_DEFAULT_MODEL` |

### Server Flags

```bash
./wallfacer run [flags] [workspace...]
```

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `-addr` | `ADDR` | `:8080` | Listen address |
| `-data` | `DATA_DIR` | `~/.wallfacer/data` | Task data directory |
| `-container` | `CONTAINER_CMD` | auto-detected | Container runtime command (`podman` or `docker`) |
| `-image` | `SANDBOX_IMAGE` | `wallfacer:latest` | Sandbox image name |
| `-env-file` | `ENV_FILE` | `~/.wallfacer/.env` | Env file passed to containers |
| `-no-browser` | — | `false` | Skip auto-opening the browser on start |

Run `./wallfacer run -help` for the full flag list.

The container runtime defaults to auto-detection: Wallfacer checks `/opt/podman/bin/podman`, then `podman` on `$PATH`, then `docker` on `$PATH`. Override with the `-container` flag or `CONTAINER_CMD` env var.

### Inspecting Configuration

```bash
./wallfacer env
```

Prints all recognized configuration variables and whether they are set, with credential values masked.

## Next Steps

- [Usage Guide](usage.md) — how to create tasks, handle feedback, use autopilot, and manage results
- [Architecture](internals/architecture.md) — system design and internals for contributors
