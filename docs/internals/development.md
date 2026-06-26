# Development Setup

This guide is for contributors who want to build Wallfacer from source, run tests, and create releases.

## Prerequisites

- **Go 1.26+**, [go.dev](https://go.dev/)
- **`golangci-lint` 2.11.3**, pinned to match CI and local `make lint-go`
- **`claude` CLI** (and optionally `codex` and `cursor-agent`) on your `PATH`, tasks exec the selected CLI directly as a host process
- **Bun**, for frontend install, build, typecheck, and tests
- **A Claude credential**, OAuth token (`claude setup-token`) or API key from [console.anthropic.com](https://console.anthropic.com/)

## Build from Source

```bash
# Clone the repository
git clone https://github.com/changkun/wallfacer.git
cd wallfacer

# Build the server binary
go build -o wallfacer .
```

`make build` runs the full gate (fmt + lint + frontend build + binary). At runtime the server execs the selected CLI directly as a host process, with the task's git worktree as the working directory; the binary per task is set via the `WALLFACER_AGENT` env var (`claude`, `codex`, or `cursor`).

## Configure Credentials

```bash
# Start the server once to create ~/.wallfacer/.env
wallfacer run
# Stop with Ctrl-C, then edit the env file:
```

```bash
# wallfacer/.env, set one of:
CLAUDE_CODE_OAUTH_TOKEN=<your-token>
# ANTHROPIC_API_KEY=sk-ant-...
```

Alternatively, start the server and configure via **Settings → Sandbox** in the browser.

## Run Tests

```bash
make test           # All tests (backend + frontend)
make test-backend   # Go unit tests: go test ./...
make test-frontend  # Frontend tests: cd frontend && bunx vitest run
```

## Make Targets

| Target | Description |
|---|---|
| `make build` | Full gate: fmt + lint (Go + Vue typecheck) + frontend build + binary |
| `make build-binary` | Build just the Go binary, skipping fmt/lint (accepts optional `VERSION=`) |
| `make server` | Build and run the server natively |
| `make fmt` | Format Go in place |
| `make lint` | Lint only (`golangci-lint` 2.11.3 + frontend `vue-tsc` typecheck); fastest way to catch style regressions |
| `make test` | fmt + lint + backend tests + frontend tests |
| `make test-backend` | Go unit tests (`go test ./...`) |
| `make test-frontend` | Frontend Vitest runner (`cd frontend && bunx vitest run`) |
| `make frontend-build` | Build the Vue SPA into `frontend/dist/` for embedding |
| `make api-contract` | Regenerate API route artifacts from `apicontract/routes.go` |
| `make e2e-lifecycle` | E2E task-lifecycle test (supports `SANDBOX=claude\|codex`) |
| `make e2e-dependency-dag WORKSPACE=/path/to/repo` | E2E dependency DAG with conflict resolution |

## Frontend Dev Mode

The Vue SPA is built to `frontend/dist/` and embedded into the binary via
`go:embed`, so an edit normally requires a rebuild. During active frontend
work, run the Vite dev server instead:

```bash
make server                 # Go server on :8080 (serves the embedded SPA + the API)
cd frontend && bun run dev  # Vite on :5173 with hot-reload, proxies /api → :8080
```

Open `http://localhost:5173` and edit files under `frontend/src/`, Vite
hot-reloads in the browser with no rebuild. The Go server only needs
rebuilding when backend code changes.

## Release Workflow

The entire release is automated by a single workflow, `release.yml`. Push a
version tag (`v*`) and it runs end to end:

```bash
git tag v0.0.7
git push origin v0.0.7
```

The pipeline runs as ordered jobs:

| Job | What it does |
|---|---|
| `verify` | Frontend typecheck + SSG build, `go build`, `go vet` (the gate; tag pushes are not covered by `test.yml`) |
| `binary` | Builds `wallfacer-{linux,darwin,windows}-{amd64,arm64}` and uploads them as artifacts |
| `image` | Builds and pushes the `ghcr.io/changkun/wallfacerd` web-server image (semver + sha tags) |
| `deploy` | Rolls the new image into the `latere` k8s namespace, smokes `wf.latere.ai`, and uploads a release-evidence artifact |
| `release` | Publishes the GitHub release with notes + evidence and the binaries attached |

`release` depends on both `binary` and `deploy`, so the release only ships
once production is deployed and smoke-passing. Tags with a `-suffix`
(`v0.0.7-alpha.6`, `v1.0.0-rc.1`) are published as pre-releases.

**Version embedding.** CLI binaries are built with
`-ldflags "-X latere.ai/x/wallfacer/internal/cli.Version=X.Y.Z"`,
stamping the version for `wallfacer doctor` and usage output.

**Release evidence.** `tools/smoke/release.sh` checks `/`, `/healthz`, and
`/api/debug/health` against the live deployment and, when `OUTPUT_MD` is set,
writes a markdown evidence block (tag, commit, build/deploy links, served
asset, smoke result). The `deploy` job mirrors it to the run summary and
uploads it as the `release-evidence` artifact; the `release` job appends it to
the release body. Because the evidence only exists when deploy is green, its
presence on a release proves prod shipped before the release published.
`release_evidence_test.go` guards both the workflow wiring and the script.

**Release notes.** GitHub generates the changelog since the previous tag
(`gh api .../releases/generate-notes`); the `release` job appends the evidence
block and publishes via `--notes-file`. Hand-written notes for older releases
are archived under `docs/releases/`.
