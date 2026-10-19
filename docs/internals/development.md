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
make test-frontend  # Frontend tests: cd frontend && bun run test
make test-all       # Everything CI runs: the bar, the frontend suite and typecheck, the guardrails
```

The Go gates live in [`latere-ai/ci-gate`](https://github.com/latere-ai/ci-gate), pinned in `go.mod` as a tool. `go tool lateregate list` prints which gates apply and which are waived, with the reason. `go tool lateregate <gate>` runs one gate on its own, and a single gate ignores the waivers, so the Makefile names only the whole bar. The decisions this repository has made about the bar are in `.lateregate.yaml`.

Tests run with an isolated home. Every package whose tests reach a per-user location (`internal/cli`, `internal/handler`, `internal/runner`) calls `testenv.RunIsolated` from its `TestMain`, which points `HOME` and the per-user config, data and cache directories at a temporary directory, so a test run never touches the latere sign-in token shared with the `latere` command line or `~/.wallfacer`. A guard test in `internal/cli` fails when the isolation is missing. `RunIsolated` also removes from `PATH` every directory that holds one of the agent CLIs wallfacer launches (`claude`, `codex`, `cursor-agent`, `opencode`, `pi`), as on a CI runner where none is installed, so a runner or handler built without an explicit binary path never starts a real agent on the developer's account; `internal/executor` calls it as well, and guard tests in `internal/testenv` and `internal/handler` fail when one of the CLIs still resolves.

### Tests that skip without setup

Some packages skip silently when their dependency is absent, so a green
`make check` does not mean every test ran:

| Tests | Skipped unless | Effect |
|---|---|---|
| Harness integration tests in `internal/harness` and `internal/executor` | `cursor-agent`, `opencode`, or `pi` is on `PATH` (and authenticated) | Real subprocess launches for those harnesses are not covered |
| `make e2e-lifecycle`, `make e2e-dependency-dag`, `make ui-test` | Run explicitly; they are not part of `make check` | End-to-end task execution and browser invariants are not covered |

## Make Targets

| Target | Description |
|---|---|
| `make build` | Full gate: fmt + the shared bar + Vue typecheck + guardrails + frontend build + binary |
| `make build-binary` | Build just the Go binary, skipping fmt/lint (accepts optional `VERSION=`) |
| `make server` | Build and run the server natively |
| `make fmt` | Run `gofmt -w` over the repository |
| `make check` | The shared Go bar: `go tool lateregate`, with the waivers in `.lateregate.yaml` |
| `make lint-all` | The bar, the frontend typecheck, and the UTF-8 truncation guardrail |
| `make test-frontend` | Frontend Vitest runner (`cd frontend && bun run test`) |
| `make test-all` | Everything CI runs: `check`, the frontend suite and typecheck, the skills mirror, the truncation guardrail |
| `make frontend-build` | Build the Vue SPA into `frontend/dist/` for embedding |
| `make api-contract` | Regenerate API route artifacts from `internal/apicontract/routes.go` |
| `make e2e-lifecycle` | E2E task-lifecycle test (supports `SANDBOX=claude\|codex`) |
| `make e2e-dependency-dag WORKSPACE=/path/to/repo` | E2E dependency DAG with conflict resolution |
| `make ui-test` | Boot against seeded demo data and assert UI invariants in a real browser (`SKIP_BUILD=1` reuses `./wallfacer`) |
| `make hooks` | Install the git hooks via `core.hooksPath`; the pre-commit runs `lateregate hook` (gofmt and the modernizers over the staged files) |

The repository's own checks, folded into `make lint-all` beside the bar (golangci-lint and the otel transport scan are gates of the bar):

| Target | Description |
|---|---|
| `make lint-js` | Frontend type check (`vue-tsc --noEmit`) |
| `make lint-truncate` | Fail on byte-index truncation of strings, which can cut a multi-byte rune |

The release and skills helpers:

| Target | Description |
|---|---|
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

The release is automated by one workflow, `release.yml`, which runs on a
version tag (`v*`). Cut a release with:

```bash
go tool lateregate release vX.Y.Z
```

It turns the `Unreleased` section of `CHANGELOG.md` into the tag's section,
commits, tags and pushes. The pipeline then runs as ordered jobs:

| Job | What it does |
|---|---|
| `verify` | Frontend typecheck and build, `go build` (the gate; tag pushes are not covered by `ci.yml`) |
| `binary` | Builds `wallfacer-{linux,darwin,windows}-{amd64,arm64}` and uploads them as workflow artifacts |
| `release` | Publishes the GitHub release with the binaries, SHA256 checksums, an SPDX SBOM and keyless cosign signatures over both |

Tags with a `-suffix` (`v0.0.7-alpha.6`, `v1.0.0-rc.1`) are published as
pre-releases.

**Version embedding.** CLI binaries are built with
`-ldflags "-X latere.ai/x/wallfacer/internal/cli.Version=X.Y.Z"`,
stamping the version for `wallfacer doctor` and usage output.

**Release notes.** The release body is the tag's section of `CHANGELOG.md`,
read by `go tool lateregate release-notes`. A tag without a section fails the
workflow before anything is published, and the pre-push hook refuses it.
Releases before that rule have their notes on GitHub.
