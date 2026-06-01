# Development Setup

This guide is for contributors who want to build Wallfacer from source, run tests, and create releases.

## Prerequisites

- **Go 1.25+** — [go.dev](https://go.dev/)
- **`claude` CLI** (and optionally `codex`) on your `PATH` — tasks exec it directly
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

`make build` runs the full gate (fmt + lint + TypeScript build + binary). At runtime the server execs the `claude`/`codex` CLI directly; the binary selected per task is set via the `WALLFACER_AGENT` env var (`claude` or `codex`).

## Configure Credentials

```bash
# Start the server once to create ~/.wallfacer/.env
wallfacer run
# Stop with Ctrl-C, then edit the env file:
```

```bash
# wallfacer/.env — set one of:
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
| `make build` | Full gate: fmt + lint (Go + JS) + TypeScript build + binary |
| `make build-binary` | Build just the Go binary, skipping fmt/lint (accepts optional `VERSION=`) |
| `make build-desktop` | Build the native desktop app for the current platform (uses `go tool wails`) |
| `make build-desktop-darwin` / `-windows` / `-linux` | Cross-platform desktop builds |
| `make install-wails` | Install the Wails CLI tracked as a tool in go.mod |
| `make server` | Build and run the server natively |
| `make server-dev` | Build and run the server with `-ui-dir ./ui` so UI edits reload without rebuilding |
| `make fmt` | Format Go and JS in place |
| `make lint` | Lint only (golangci-lint + Biome); fastest way to catch style regressions |
| `make test` | Lint + backend tests + frontend tests |
| `make test-backend` | Go unit tests (`go test ./...`) |
| `make test-frontend` | Frontend Vitest runner |
| `make ui-css` | Regenerate Tailwind CSS from UI sources |
| `make api-contract` | Regenerate API route artifacts from `apicontract/routes.go` |
| `make e2e-lifecycle` | E2E task-lifecycle test (supports `SANDBOX=claude\|codex`) |
| `make e2e-dependency-dag WORKSPACE=/path/to/repo` | E2E dependency DAG with conflict resolution |
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

## Release Workflow

Releases are triggered by pushing a version tag (`v*`). Two GitHub Actions workflows run in parallel in this repository:

| Workflow | Artifact |
|---|---|
| `release-binary.yml` | `wallfacer-{linux,darwin,windows}-{amd64,arm64}` binaries on the GitHub Release |
| `release-desktop.yml` | Signed desktop apps (`Wallfacer-Desktop-*`) on the GitHub Release |

**Version embedding.** Release binaries are built with `-ldflags "-X changkun.de/x/wallfacer/internal/cli.Version=X.Y.Z"`. This stamps the wallfacer version for `wallfacer doctor` and usage output.

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
