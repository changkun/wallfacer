# Getting Started

This guide walks through installing Wallfacer, connecting it to credentials, and running your first task.

## Prerequisites

- **`claude` CLI** on your `PATH` (`npm i -g @anthropic-ai/claude-code`). Tasks exec it directly.
- **`codex` CLI** (optional, `npm i -g @openai/codex`) for Codex-typed tasks.
- **A Claude credential**, either a Claude Pro/Max OAuth token or an Anthropic API key (configured after install).
- **Git**, recommended; non-git directories work as workspaces but git features (worktrees, diff, auto-push) are unavailable.

## Step 1: Install Wallfacer

```bash
curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | sh
```

This detects your OS and architecture, downloads the latest binary, and places it in `/usr/local/bin` (or `~/.local/bin`). Set `WALLFACER_INSTALL_DIR` to override the location, or `WALLFACER_VERSION=v0.0.5` for a specific version.

> **Building from source?** See [Development Setup](../internals/development.md) for `go build` and `make` targets.

## Step 2: Start Wallfacer

```bash
wallfacer run
```

On startup, Wallfacer restores the most recently used workspace group from your previous session. If no saved group exists, it starts with no active workspaces, select them from the UI workspace picker.

On first run, Wallfacer auto-creates `~/.wallfacer/` and a template `.env` file. The browser opens automatically to `http://localhost:8080` showing a task board with four columns.

![The Wallfacer task board](images/board.png)

Each task runs as a host process: Wallfacer execs `claude` (or `codex`) directly in the task's git worktree, setting `WALLFACER_AGENT` to select the CLI.

> Host mode caps concurrency to 1 by default so the `claude` / `codex` CLIs don't race on their shared `~/.claude` and `~/.codex` state. Raise it with `WALLFACER_MAX_PARALLEL` once you've confirmed your CLI tolerates parallel runs (see [Configuration → Host mode](configuration.md#host-mode)).

## Step 3: Configure Your Credential

Open **Settings → Harness** in the browser and enter your credential:

**Option A: Sign in with Claude (easiest)**

Click **Sign in with Claude** in the Settings panel. Your browser opens to authenticate, and the token is stored automatically.

**Option B: OAuth token (manual paste)**

Paste your OAuth token into `CLAUDE_CODE_OAUTH_TOKEN`. To obtain a token, run `claude setup-token` in the `claude` CLI ([claude.ai/download](https://claude.ai/download) or `npm install -g @anthropic-ai/claude-code`).

**Option C: Anthropic API key**

Paste your API key into `ANTHROPIC_API_KEY`. Generate one at [console.anthropic.com](https://console.anthropic.com/) → API Keys. Keys start with `sk-ant-...`.

You only need one of the three. Changes take effect on the next task without a server restart.

> You can also edit `~/.wallfacer/.env` directly if you prefer.

### Optional: Enable Codex Sandbox

Wallfacer supports two Codex auth modes:

1. **Host auth cache (recommended)**
   If `~/.codex/auth.json` exists on your host machine, Wallfacer validates it at startup and enables Codex automatically.
2. **API key fallback**
   Set `OPENAI_API_KEY` in **Settings → Harness** and run **Test (Codex)** once.

See [Configuration → Host mode](configuration.md#host-mode) for how Wallfacer translates Codex's CLI output into the Claude-compatible event stream the runner expects.

## Step 4: Verify the Setup

Run the doctor command to check that everything is configured correctly:

```bash
wallfacer doctor
```

This checks configuration paths, your Claude (and optional Codex) credential, the `claude` / `codex` binaries on your `PATH`, and Git. Items marked `[ok]` are ready, `[!]` need attention, and `[ ]` are optional. For the full status-code reference, see [Configuration → wallfacer doctor](configuration.md#wallfacer-doctor).

Once all required checks pass, create a test task: click **+ New Task**, enter a short prompt, click Add, and drag the card to **In Progress**. The task starts running and live log output appears in the task detail panel.

If the task fails immediately, check:

- The credential is correct (re-check in **Settings → Harness**)
- The `claude` CLI is installed and on your `PATH` (`wallfacer doctor` reports its resolved path)

## Security

Tasks run as host processes with your user's permissions. A task agent can read or write any file your account can, not just its worktree. Run Wallfacer only on machines you trust. A warning banner appears in **Settings → Harness** while active.

## CLI Reference

```bash
wallfacer run [flags]                    # Start the task board server
wallfacer doctor                         # Check prerequisites and config
wallfacer status                         # Print board state to terminal
wallfacer status -watch                  # Live-updating board state
wallfacer status -json                   # Machine-readable JSON output
```

Common `run` flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:8080` | Listen address |
| `-no-browser` | `false` | Skip auto-opening the browser |
| `-env-file` | `~/.wallfacer/.env` | Env file with credentials and runtime settings |
| `-log-format` | `text` | Log format: `text` or `json` |

Run `wallfacer run -help` for the full flag list. For the complete configuration reference (env vars, sandbox routing, etc.), see [Configuration](configuration.md).

## Windows

### Native Windows

A pre-built `wallfacer-windows-amd64.exe` binary is available on the [releases page](https://github.com/changkun/wallfacer/releases). You can also install via Git Bash or MSYS2:

```bash
curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | sh
```

**Prerequisites:** install the `claude` CLI (and optionally `codex`) and make sure they are on your `PATH`. Tasks exec them directly.

### WSL2

Windows users can also run Wallfacer inside WSL2 with the same experience as native Linux:

1. **Install WSL2**: run `wsl --install` in an elevated PowerShell (requires Windows 10 2004+ or Windows 11)
2. **Inside WSL2**, install Go 1.25+ and the `claude` CLI (and optionally `codex`)
3. **Clone the repo into the WSL2 filesystem** (not `/mnt/c/`, cross-filesystem I/O is much slower)
4. Build and run:
   ```bash
   go build -o wallfacer . && ./wallfacer run
   ```
5. The browser opens automatically on the Windows host via `cmd.exe /c start`
6. Keep workspace repos on the WSL2 filesystem for best performance

## Next Steps

- [Usage Guide](usage.md): how to create tasks, handle feedback, use autopilot, and manage results
- [Configuration](configuration.md): env vars, sandbox routing, and advanced settings
- [Architecture](../internals/architecture.md): system design and internals for contributors
