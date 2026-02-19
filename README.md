# Wallfacer

A containerized Claude Code execution environment. Runs Claude Code headlessly in a Linux dev container with Go, Node.js, and Python pre-installed.

## Setup

```bash
# 1. Get an OAuth token (needs a browser)
claude setup-token

# 2. Configure
cp container/.env.example container/.env
# Edit container/.env and paste your token

# 3. Build
make build
```

## Usage

### Headless mode

```bash
make run PROMPT="fix the failing tests"
```

### Multiple workspaces

```bash
make run WORKSPACES="/path/to/repo-a /path/to/repo-b" PROMPT="compare these projects"
```

Each folder is mounted as `/workspace/<basename>` inside the container.

### Interactive TUI

```bash
make interactive
```

### Debug shell

```bash
make shell
```

## Make Targets

| Target | Description |
|---|---|
| `make build` | Build the container image |
| `make run PROMPT="..."` | Run Claude headlessly with a prompt |
| `make interactive` | Start Claude's interactive TUI |
| `make shell` | Open a bash shell in the container |
| `make stop` | Stop the running container |
| `make clean` | Remove container, volumes, and image |

## What's Inside

- **Ubuntu 24.04** base
- **Go 1.24** + tools (gopls, dlv, golangci-lint, staticcheck, gosec, goimports, ...)
- **Node.js 22 LTS**
- **Python 3** with pip and venv
- **Claude Code CLI** with `--dangerously-skip-permissions`
- git, ripgrep, jq, build-essential

## Project Structure

```
.
├── Makefile                  # Top-level convenience targets
├── container/
│   ├── Dockerfile            # Ubuntu 24.04 + Go + Node + Python + Claude Code
│   ├── entrypoint.sh         # git safe.directory fix, launches Claude
│   ├── docker-compose.yml    # Service definition (optional, for compose users)
│   ├── .env.example          # Template for environment variables
│   └── .dockerignore
├── docs/
│   └── usage.md              # Detailed usage guide
└── TODO.md                   # Deferred items
```

## Configuration

Set these in `container/.env`:

| Variable | Description |
|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | OAuth token from `claude setup-token` |
| `WORKSPACES` | Space-separated list of folders to mount (default: current dir) |

## Requirements

- [Podman](https://podman.io/) (or Docker)
- Claude Pro or Max subscription (for OAuth token)

## License

See [LICENSE](LICENSE).
