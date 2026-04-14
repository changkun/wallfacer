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

`make build` builds the binary and pulls both sandbox images in one step. Sandbox images are maintained in a separate repository (`github.com/latere-ai/images`); for normal development the server pulls `ghcr.io/latere-ai/sandbox-claude:latest` automatically on first task run.

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
| `make shell` | Open a bash shell in a sandbox container |
| `make clean` | Remove all sandbox images |
| `make ui-css` | Regenerate Tailwind CSS |
| `make api-contract` | Regenerate API route artifacts from `apicontract/routes.go` |
| `make run PROMPT="…"` | Headless one-shot Claude execution |
| `make release-notes` | Generate an LLM prompt for release notes (requires `RELEASE_VERSION=`) |
| `make release` | Commit release notes, tag, push, create GitHub release (requires `RELEASE_VERSION=` and `RELEASE_NOTES=`) |

## Sandbox Images

```bash
podman images sandbox-claude   # or: docker images sandbox-claude
```

The sandbox images are maintained in a separate repository ([github.com/latere-ai/images](https://github.com/latere-ai/images)). They are Ubuntu 24.04 images bundling Go 1.25, Node.js 22, Python 3, and the respective agent CLI (Claude Code or Codex). Multi-arch images (amd64 + arm64) are published to `ghcr.io/latere-ai/sandbox-claude` and `ghcr.io/latere-ai/sandbox-codex` on version tags via GitHub Actions.

To build sandbox images locally (e.g. for customization or offline use):

```bash
git clone https://github.com/latere-ai/images.git
cd images
make                   # Build both sandbox-claude and sandbox-codex
make RUNTIME=docker    # Use Docker instead of Podman
```

Local builds are tagged as `sandbox-claude:latest` and `sandbox-codex:latest`, which wallfacer picks up automatically as the fallback when the GHCR image is unavailable.

## Release Workflow

Releases are triggered by pushing a version tag (`v*`). Two GitHub Actions workflows run in parallel in this repository:

| Workflow | Artifact |
|---|---|
| `release-binary.yml` | `wallfacer-{linux,darwin,windows}-{amd64,arm64}` binaries on the GitHub Release |
| `release-desktop.yml` | Signed desktop apps (`Wallfacer-Desktop-*`) on the GitHub Release |

Sandbox images (`ghcr.io/latere-ai/sandbox-claude` and `ghcr.io/latere-ai/sandbox-codex`) are built and published from [`github.com/latere-ai/images`](https://github.com/latere-ai/images) on its own release cadence.

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
