# Usage Guide

## Prerequisites

- [Podman](https://podman.io/) (or Docker)
- A Claude Code OAuth token (requires Claude Pro or Max subscription)

## Quick Start

### 1. Get an OAuth token

Run this locally (needs a browser):

```bash
claude setup-token
```

This opens a browser OAuth flow. After authenticating, copy the token.

### 2. Configure your token

```bash
cp sandbox/.env.example sandbox/.env
```

Edit `sandbox/.env` and set your token:

```
CLAUDE_CODE_OAUTH_TOKEN=your-oauth-token-here
```

### 3. Build the sandbox image

```bash
make build
```

### 4. Run Claude interactively

```bash
make run
```

This launches Claude Code's interactive TUI inside the sandbox with `--dangerously-skip-permissions` enabled. The repo root is mounted at `/workspace`.

## Make Targets

| Command | Description |
|---|---|
| `make build` | Build the sandbox image |
| `make run` | Start Claude interactively |
| `make shell` | Open a bash shell in the sandbox (useful for debugging) |
| `make stop` | Stop the running sandbox |
| `make clean` | Stop sandbox, remove volumes and image |

## Mounting a Different Repo

By default the sandbox mounts the current repo root. To point it at a different git repo, set `WORKSPACE` in `sandbox/.env`:

```
WORKSPACE=/absolute/path/to/your/repo
```

Or pass it inline:

```bash
WORKSPACE=/path/to/repo make run
```

## Headless Mode (Non-Interactive)

Pass a prompt with `-p` to run Claude in headless mode — it executes the task and exits:

```bash
make run ARGS='-p "fix the failing tests"'
```

## Using the Shell

To inspect the sandbox environment or run tools manually:

```bash
make shell
```

Inside the sandbox you have access to:

```bash
go version          # Go 1.24.x
node --version      # v22.x
python3 --version   # Python 3.x
git --version       # Git
claude --version    # Claude Code CLI

# Go tools
gopls version       # Language server
dlv version         # Delve debugger
golangci-lint --version
staticcheck -version
gosec --version
goimports -l .
gotests -h
gomodifytags -h
impl -h
```

## Session Persistence

Claude's configuration and session data are stored in a named Podman volume (`claude-config`). This persists across sandbox restarts so Claude retains context between runs.

To reset session data:

```bash
make clean   # removes the volume along with the image
make build   # rebuild
```

## Architecture

```
sandbox/
├── Dockerfile          # Ubuntu 24.04 + Go + Node + Python + Claude Code
├── entrypoint.sh       # Sets up git safe.directory, launches Claude
├── docker-compose.yml  # Service definition, volumes, env vars
├── .dockerignore       # Excludes .git, .env, node_modules from build
└── .env.example        # Template for environment variables
```

The sandbox runs as a non-root user `claude` (UID 1000) with passwordless sudo. This matches typical host user UIDs to minimize volume permission issues.

## Troubleshooting

**"dubious ownership" git errors** — Handled automatically by the entrypoint. If you still see them, the entrypoint may not be running; check with `make shell`.

**Permission denied on mounted files** — The sandbox user has UID 1000. If your host user has a different UID, rebuild with:

```bash
/opt/podman/bin/podman build -t wallfacer:latest \
  --build-arg USER_UID=$(id -u) \
  --build-arg USER_GID=$(id -g) \
  -f sandbox/Dockerfile sandbox/
```

**Claude can't authenticate** — Verify `CLAUDE_CODE_OAUTH_TOKEN` is set in `sandbox/.env` and the sandbox has network access. Regenerate the token with `claude setup-token` if it has expired.
