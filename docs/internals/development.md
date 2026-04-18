# Development Setup

This guide is for contributors who want to build Wallfacer from source, run tests, and create releases.

## Prerequisites

- **Go 1.25+** — [go.dev](https://go.dev/)
- **Podman** or **Docker** — container runtime for sandbox images
- **Node.js 22+** — for frontend tests and Tailwind CSS regeneration
- **A Claude credential** — OAuth token (`claude setup-token`) or API key from [console.anthropic.com](https://console.anthropic.com/)

## Build from Source

```bash
# Clone the repository
git clone https://github.com/changkun/wallfacer.git
cd wallfacer

# Build the server binary
go build -o wallfacer .
```

Pull sandbox images (optional — auto-pulled from ghcr.io at runtime):

```bash
make pull-images    # Pull Claude and Codex sandbox images
```

`make build` builds the binary and pulls the unified sandbox image in one step. The sandbox image is maintained in a separate repository (`github.com/latere-ai/images`); for normal development the server pulls `ghcr.io/latere-ai/sandbox-agents:latest` automatically on first task run. The same image ships both the Claude Code and Codex CLIs; the entrypoint dispatches to the correct one based on `WALLFACER_AGENT` (`claude` or `codex`), which the runner sets per task.

## Configure Credentials

```bash
# Start the server once to create ~/.wallfacer/.env
wallfacer run
# Stop with Ctrl-C, then edit the env file:
```

```bash
# ~/.wallfacer/.env — set one of:
CLAUDE_CODE_OAUTH_TOKEN=<your-token>
# ANTHROPIC_API_KEY=sk-ant-...
```

Alternatively, start the server and configure via **Settings → Sandbox** in the browser.

## Run Tests

```bash
make test           # All tests (backend + frontend)
make test-backend   # Go unit tests: go test ./...
make test-frontend  # Frontend JS tests: cd ui && npx vitest@2 run
```

## Make Targets

| Target | Description |
|---|---|
| `make build` | Build binary + pull both sandbox images |
| `make build-binary` | Build just the Go binary (accepts optional `VERSION=`) |
| `make pull-images` | Pull Claude and Codex sandbox images |
| `make server` | Build and run the server |
| `make server-dev` | Build and run the server with `-ui-dir ./ui` so UI edits reload without rebuilding |
| `make shell` | Open a bash shell in a sandbox container |
| `make clean` | Remove all sandbox images |
| `make ui-css` | Regenerate Tailwind CSS |
| `make api-contract` | Regenerate API route artifacts from `apicontract/routes.go` |
| `make run PROMPT="…"` | Headless one-shot Claude execution |
| `make release-notes` | Generate an LLM prompt for release notes (requires `RELEASE_VERSION=`) |
| `make release` | Commit release notes, tag, push, create GitHub release (requires `RELEASE_VERSION=` and `RELEASE_NOTES=`) |

## Frontend Dev Mode

UI assets (HTML, CSS, JS) are normally embedded into the binary via `go:embed`, so a frontend edit usually requires a rebuild. During active frontend work, use the `-ui-dir` flag to serve `ui/` from disk instead:

```bash
wallfacer run -ui-dir ./ui
# or equivalently:
make server-dev
# or via environment variable:
UI_DIR=./ui wallfacer run
```

In this mode the server re-parses `index.html` and `partials/*.html` on every request and emits `Cache-Control: no-store` for `/css/`, `/js/`, and `/assets/`. Reloading the browser is enough to see edits — no binary rebuild, no server restart.

Do not use this flag in production: it disables template caching and browser caching, and points the server at a mutable directory.

## Sandbox Images

```bash
podman images sandbox-agents   # or: docker images sandbox-agents
```

The sandbox image is maintained in a separate repository ([github.com/latere-ai/images](https://github.com/latere-ai/images)). It is an Ubuntu 24.04 image bundling Go 1.25, Node.js 22, Python 3, and both agent CLIs (Claude Code and Codex). The entrypoint dispatches to the requested CLI based on `WALLFACER_AGENT`. The multi-arch image (amd64 + arm64) is published to `ghcr.io/latere-ai/sandbox-agents` on version tags via GitHub Actions.

To build sandbox images locally (e.g. for customization or offline use):

```bash
git clone https://github.com/latere-ai/images.git
cd images
make                   # Build the unified sandbox-agents image
make RUNTIME=docker    # Use Docker instead of Podman
```

Local builds are tagged as `sandbox-agents:latest`. Wallfacer prefers this local image over a network pull whenever the versioned GHCR tag (e.g. `ghcr.io/latere-ai/sandbox-agents:v0.0.6`) is not yet cached, and still uses it as a fallback if a pull fails. This keeps `wallfacer run` in sync with the Makefile's `pull-images` target, so a sibling latere-ai/images checkout works without network round-trips.

## Release Workflow

Releases are triggered by pushing a version tag (`v*`). Two GitHub Actions workflows run in parallel in this repository:

| Workflow | Artifact |
|---|---|
| `release-binary.yml` | `wallfacer-{linux,darwin,windows}-{amd64,arm64}` binaries on the GitHub Release |
| `release-desktop.yml` | Signed desktop apps (`Wallfacer-Desktop-*`) on the GitHub Release |

The unified sandbox image (`ghcr.io/latere-ai/sandbox-agents`) is built and published from [`github.com/latere-ai/images`](https://github.com/latere-ai/images) on its own release cadence.

**Version embedding.** Release binaries are built with `-ldflags "-X changkun.de/x/wallfacer/internal/cli.Version=X.Y.Z"`. This stamps the wallfacer version for `wallfacer doctor` and usage output. It does **not** determine the sandbox image tag.

**Sandbox image tag embedding.** The sandbox image is maintained in a separate repository (`github.com/latere-ai/images`) that releases independently of wallfacer. Its latest tag is resolved from that repo at build time and passed via `-ldflags "-X changkun.de/x/wallfacer/internal/cli.SandboxTag=vA.B.C"`. The `Makefile` resolves `SANDBOX_TAG` for local builds; the release workflows (`release-binary.yml`, `release-desktop.yml`) resolve it before the build step. If neither is set, the binary queries the GitHub API for the latest `latere-ai/images` release at runtime and falls back to `:latest` when the lookup fails.

**Image tagging.** Each `latere-ai/images` release produces three image tags on GHCR: `v<version>` (e.g. `v0.0.6`), `v<major>.<minor>` (e.g. `v0.0`), and `latest`.

**Creating a release:**

```bash
# 1. Generate release notes (pipes git diff through claude, writes to docs/releases/)
make release-notes RELEASE_VERSION=v0.0.6

# 2. Review and edit docs/releases/v0.0.6.md

# 3. Commit notes, tag, push, and create GitHub release
make release RELEASE_VERSION=v0.0.6
```

The `make release-notes` target runs `scripts/release-notes.sh`, which collects the commit log and diffstat since the previous tag, pipes them through the `claude` CLI with style instructions derived from the previous release notes, and writes the result to `docs/releases/v0.0.6.md`.

The `make release` target:
1. Commits `docs/releases/v0.0.6.md`
2. Creates an annotated git tag
3. Pushes the commit and tag to origin
4. Creates a GitHub release via `gh release create` using the same notes file

The pushed tag triggers the three CI workflows above. All release notes are archived in `docs/releases/`.
