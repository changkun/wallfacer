# Development Setup

This guide is for contributors who want to build Wallfacer from source, run tests, and create releases.

## Prerequisites

- **Go 1.27+**, [go.dev](https://go.dev/)
- **`golangci-lint` 2.13.1**, pinned to match CI and local `make lint-go`
- **`claude` CLI** (and optionally `codex`, `cursor-agent`, `opencode`, and `pi`) on your `PATH`, tasks exec the selected CLI directly as a host process
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

`make build` runs the full gate (fmt + lint + frontend build + binary). At runtime the server execs the selected CLI directly as a host process, with the task's git worktree as the working directory; the binary per task is set via the `WALLFACER_AGENT` env var (`claude`, `codex`, `cursor`, `opencode`, or `pi`).

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
| `make lint` | Lint only (`golangci-lint` 2.13.1 + frontend `vue-tsc` typecheck); fastest way to catch style regressions |
| `make test` | fmt + lint + backend tests + frontend tests |
| `make test-backend` | Go unit tests (`go test ./...`) |
| `make test-frontend` | Frontend Vitest runner (`cd frontend && bun run test`) |
| `make frontend-build` | Build the Vue SPA into `frontend/dist/` for embedding |
| `make api-contract` | Regenerate API route artifacts from `apicontract/routes.go` |
| `make e2e-lifecycle` | E2E task-lifecycle test (supports `SANDBOX=claude\|codex`) |
| `make e2e-dependency-dag WORKSPACE=/path/to/repo` | E2E dependency DAG with conflict resolution |
| `make ui-test` | Boot against seeded demo data and assert UI invariants in a real browser (`SKIP_BUILD=1` reuses `./wallfacer`) |
| `make fmt-check` | Fail if any Go source is not gofmt-formatted |
| `make hooks` | Install the git hooks (pre-commit gofmt guard) via `core.hooksPath` |

Lint sub-targets, all folded into `make lint`:

| Target | Description |
|---|---|
| `make lint-go` | `golangci-lint` at the repo-pinned version |
| `make lint-js` | Frontend type check (`vue-tsc --noEmit`) |
| `make lint-otel` | Fail on any outbound `&http.Client{}` without the otel transport |
| `make lint-truncate` | Fail on byte-index truncation of strings, which can cut a multi-byte rune |

The `wallfacerd` web-server variant and the release/skills helpers:

| Target | Description |
|---|---|
| `make web-frontend` | Build the wallfacerd frontend for embedding |
| `make web-run` | Run wallfacerd locally on `:8080` with the embedded SPA |
| `make web-dev` | Dev stack: Go on `:8080` plus Vite on `:5173` |
| `make web-docker` | Build the `wallfacerd:dev` image from `Dockerfile.wallfacerd` |
| `make release-prod` | Build and publish the prod image (`VERSION=`, else the short HEAD sha) |
| `make commit-seq MSG=...` | Commit one staged scope with a required message |
| `make push-once` | Push once via `scripts/push-once.sh` (`REMOTE=`, `BRANCH=`) |
| `make skills-check` | Fail on any difference between `.claude/skills/` and upstream (CI gate) |
| `make skills-pull` | Adopt upstream skill changes into this repo |
| `make skills-push` | Promote local skill edits into `../claude-plugins` |

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
`release_evidence_test.go` runs the evidence script end to end.

**Release notes.** The `release` job prefers a hand-written note at
`docs/releases/<TAG>.md` and falls back to GitHub's generated changelog since
the previous tag (`gh api .../releases/generate-notes`) when that file is
absent. Either way it appends the evidence block and publishes via
`--notes-file`.
