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

```bash
wallfacer run
```

On startup, Wallfacer restores the most recently used workspace group from your previous session. If no saved group exists, it starts with no active workspaces — select them from the UI workspace picker.

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
wallfacer run [flags]                    # Start the task board server
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
| `-container` | auto-detected | Container runtime (`podman` or `docker`) |
| `-log-format` | `text` | Log format: `text` or `json` |

Run `wallfacer run -help` for the full flag list. For the complete configuration reference (env vars, sandbox routing, etc.), see [Configuration](configuration.md).

## Windows

### Native Windows

A pre-built `wallfacer-windows-amd64.exe` binary is available on the [releases page](https://github.com/changkun/wallfacer/releases). You can also install via Git Bash or MSYS2:

```bash
curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | sh
```

### WSL2

Windows users can also run Wallfacer inside WSL2 with the same experience as native Linux:

1. **Install WSL2** — run `wsl --install` in an elevated PowerShell (requires Windows 10 2004+ or Windows 11)
2. **Inside WSL2**, install Go 1.25+ and Podman (or Docker Engine)
3. **Clone the repo into the WSL2 filesystem** (not `/mnt/c/` — cross-filesystem I/O is much slower)
4. Build and run:
   ```bash
   go build -o wallfacer . && ./wallfacer run
   ```
5. The browser opens automatically on the Windows host via `cmd.exe /c start`
6. Keep workspace repos on the WSL2 filesystem for best performance

The container runtime override `CONTAINER_CMD` works on all platforms if the auto-detection picks the wrong binary.

## Next Steps

- [Usage Guide](usage.md) — how to create tasks, handle feedback, use autopilot, and manage results
- [Configuration](configuration.md) — env vars, sandbox routing, and advanced settings
- [Architecture](../internals/architecture.md) — system design and internals for contributors
