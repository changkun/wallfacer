# Development Setup

This guide is for contributors who want to build Wallfacer from source, run tests, and create releases.

## Prerequisites

- **Go 1.27+**, [go.dev](https://go.dev/)
- **`claude` CLI** (and optionally `codex`, `cursor-agent`, `opencode`, and `pi`) on your `PATH`, tasks exec the selected CLI directly as a host process
- **Bun**, for frontend install, build, typecheck, and tests
- **A Claude credential**, OAuth token (`claude setup-token`) or API key from [console.anthropic.com](https://console.anthropic.com/)

## Build from Source

```bash
# Clone the repository
git clone https://github.com/changkun/wallfacer.git
cd wallfacer

# Build the frontend, then the server binary
make build-binary
```

The Go binary embeds the built SPA from `frontend/dist/`. `make build-binary` builds the frontend first. A bare `go build .` on a fresh clone still compiles, because `frontend/dist/PLACEHOLDER.txt` is tracked, but the binary it produces serves no SPA. `make build` runs the full gate (fmt + frontend build + every lint + binary).

At runtime the server execs the selected CLI directly as a host process, with the task's git worktree as the working directory; the binary per task is set via the `WALLFACER_AGENT` env var (`claude`, `codex`, `cursor`, `opencode`, or `pi`).

## Configure Credentials

```bash
# Start the server once to create ~/.wallfacer/.env
wallfacer run
# Stop with Ctrl-C, then edit the env file:
```

```bash
# ~/.wallfacer/.env, set one of:
CLAUDE_CODE_OAUTH_TOKEN=<your-token>
# ANTHROPIC_API_KEY=sk-ant-...
```

Alternatively, start the server and configure via **Settings → Sandbox** in the browser.

## Run Tests

```bash
make check          # The shared Go bar: every gate lateregate holds, reported together
make test           # go vet and the Go suite (one gate of the bar)
make test-frontend  # Frontend tests: cd frontend && bun run test
make test-all       # Everything CI runs: the bar, the frontend suite and typecheck, the guardrails
```

The Go gates live in [`latere-ai/ci-gate`](https://github.com/latere-ai/ci-gate), pinned in `go.mod` as a tool. `go tool lateregate list` prints which gates apply and which are waived, with the reason; `go tool lateregate <gate>` runs one. The decisions this repository has made about the bar are in `.lateregate.yaml`.

### Tests that skip without setup

Some packages skip silently when their dependency is absent, so a green
`make test` does not mean every test ran:

| Tests | Skipped unless | Effect |
|---|---|---|
| `internal/store/postgres`, the coordinator comment-store contract tests | `WALLFACER_TEST_DATABASE_URL` points at a reachable PostgreSQL | The Postgres storage backend is exercised only through the filesystem-backed contract tests |
| Harness integration tests in `internal/harness` and `internal/executor` | `cursor-agent`, `opencode`, or `pi` is on `PATH` (and authenticated) | Real subprocess launches for those harnesses are not covered |
| `make e2e-lifecycle`, `make e2e-dependency-dag`, `make ui-test` | Run explicitly; they are not part of `make test` | End-to-end task execution and browser invariants are not covered |

CI runs without a database, so the Postgres tests skip there too. To run them:

```bash
WALLFACER_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/wallfacer_test?sslmode=disable \
  go test ./internal/store/postgres/ ./internal/coordinator/
```

## Make Targets

| Target | Description |
|---|---|
| `make build` | Full gate: fmt + every lint (Go + Vue typecheck + guardrails) + frontend build + binary |
| `make build-binary` | Build just the Go binary, skipping fmt/lint (accepts optional `VERSION=`) |
| `make server` | Build and run the server natively |
| `make fmt` | Run `gofmt -w` over the repository |
| `make check` | The shared Go bar: `go tool lateregate` |
| `make lint` | golangci-lint at the version lateregate pins, against the shared config it renders |
| `make lint-all` | Every lint: Go, frontend typecheck, otel transport, UTF-8 truncation |
| `make test` | `go vet` and the Go suite |
| `make test-frontend` | Frontend Vitest runner (`cd frontend && bun run test`) |
| `make test-all` | Everything CI runs: `check`, the frontend suite and typecheck, the skills mirror, the truncation guardrail |
| `make frontend-build` | Build the Vue SPA into `frontend/dist/` for embedding |
| `make api-contract` | Regenerate API route artifacts from `internal/apicontract/routes.go` |
| `make e2e-lifecycle` | E2E task-lifecycle test (supports `SANDBOX=claude\|codex`) |
| `make e2e-dependency-dag WORKSPACE=/path/to/repo` | E2E dependency DAG with conflict resolution |
| `make ui-test` | Boot against seeded demo data and assert UI invariants in a real browser (`SKIP_BUILD=1` reuses `./wallfacer`) |
| `make hooks` | Install the git hooks via `core.hooksPath`; the pre-commit runs `lateregate hook` (gofmt and the modernizers over the staged files) |

Lint sub-targets, all folded into `make lint-all`:

| Target | Description |
|---|---|
| `make lint` | `golangci-lint` through lateregate |
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

Not every E2E script has a make target. `scripts/e2e-auth-flow.sh` walks the
full cloud-mode auth chain (`/login` → `/authorize` → email OTP → `/callback` →
session → `/api/auth/me` → `/api/auth/orgs`), asserting every hop. It prompts
for an email address and the six-digit code, so it is run by hand rather than
from CI. It needs a local wallfacer with `WALLFACER_CLOUD=true` and `AUTH_URL`
pointing at a reachable auth service.

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
