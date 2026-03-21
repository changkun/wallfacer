# Getting Started

This guide walks through installing Wallfacer, connecting it to credentials, and running your first task.

## Prerequisites

- **Podman** or **Docker** — Wallfacer auto-detects whichever is available
- **A Claude credential** — either a Claude Pro/Max OAuth token or an Anthropic API key (configured after install)
- **Git** — recommended; non-git directories work as workspaces but git features (worktrees, diff, auto-push) are unavailable

## Step 1 — Install Wallfacer

```bash
curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | sh
```

This detects your OS and architecture, downloads the latest binary, and places it in `/usr/local/bin` (or `~/.local/bin`). Set `WALLFACER_INSTALL_DIR` to override the location, or `WALLFACER_VERSION=v0.0.5` for a specific version.

> **Building from source?** See [Development Setup](../internals/development.md) for `go install`, `go build`, `make` targets, and sandbox image builds.

## Step 2 — Start Wallfacer

Pass the directories of the projects you want to work on:

```bash
wallfacer run ~/projects/myapp
```

Multiple workspaces:

```bash
wallfacer run ~/projects/myapp ~/projects/mylib
```

No argument defaults to the current directory:

```bash
wallfacer run
```

Start with no active workspaces (configure them later in the UI):

```bash
wallfacer run -no-workspaces
```

On first run, Wallfacer auto-creates `~/.wallfacer/` and a template `.env` file. The browser opens automatically to `http://localhost:8080` showing a task board with four columns.

The sandbox image (`ghcr.io/changkun/wallfacer:latest`) is pulled automatically the first time a task runs. This is a one-time download (~1 GB).

## Step 3 — Configure Your Credential

Open **Settings → Sandbox** in the browser and enter your credential:

**Option A — OAuth token (Claude Pro or Max subscription)**

Paste your OAuth token into `CLAUDE_CODE_OAUTH_TOKEN`. To obtain a token, run `claude setup-token` in the `claude` CLI ([claude.ai/download](https://claude.ai/download) or `npm install -g @anthropic-ai/claude-code`).

**Option B — Anthropic API key**

Paste your API key into `ANTHROPIC_API_KEY`. Generate one at [console.anthropic.com](https://console.anthropic.com/) → API Keys. Keys start with `sk-ant-...`.

You only need one of the two. Changes take effect on the next task without a server restart.

> You can also edit `~/.wallfacer/.env` directly if you prefer.

### Optional: Enable Codex Sandbox

Wallfacer supports two Codex auth modes:

1. **Host auth cache (recommended)**
   If `~/.codex/auth.json` exists on your host machine, Wallfacer validates it at startup and enables Codex automatically.
2. **API key fallback**
   Set `OPENAI_API_KEY` in **Settings → Sandbox** and run **Test (Codex)** once.

## Step 4 — Verify the Setup

Run the doctor command to check that everything is configured correctly:

```bash
wallfacer doctor
```

This checks configuration paths, credentials, container runtime, sandbox images, and Git. Items marked `[ok]` are ready, `[!]` need attention, and `[ ]` are optional.

Once all required checks pass, create a test task: click **+ New Task**, enter a short prompt, click Add, and drag the card to **In Progress**. A sandbox container starts and live log output appears in the task detail panel.

If the task fails immediately, check:

- The credential is correct (re-check in **Settings → Sandbox**)
- The container runtime (Podman or Docker) is running and accessible to your user
- Network access is available (the sandbox image is pulled from `ghcr.io` on first use)

## CLI Reference

```bash
wallfacer run [flags] [workspace...]     # Start the task board server
wallfacer doctor                         # Check prerequisites and config
wallfacer status                         # Print board state to terminal
wallfacer status -watch                  # Live-updating board state
wallfacer status -json                   # Machine-readable JSON output
wallfacer exec <task-id-prefix>          # Attach to a running task container
wallfacer exec --sandbox claude          # Open shell in a new sandbox
```

Common `run` flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:8080` | Listen address |
| `-no-browser` | `false` | Skip auto-opening the browser |
| `-no-workspaces` | `false` | Start with no active workspaces |
| `-container` | auto-detected | Container runtime (`podman` or `docker`) |
| `-log-format` | `text` | Log format: `text` or `json` |

Run `wallfacer run -help` for the full flag list. For the complete configuration reference (env vars, sandbox routing, webhooks, etc.), see [Configuration](configuration.md).

## Next Steps

- [Usage Guide](usage.md) — how to create tasks, handle feedback, use autopilot, and manage results
- [Configuration](configuration.md) — env vars, sandbox routing, webhooks, and advanced settings
- [Architecture](../internals/architecture.md) — system design and internals for contributors
