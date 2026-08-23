# Contributing to Wallfacer

This guide is for developers and contributors working on Wallfacer itself. If
you only want to *use* Wallfacer, start with the [User Manual](docs/guide/usage.md).

## Orientation

- **[Technical Internals](docs/internals/internals.md)** is the canonical map of
  the codebase: architecture, package layout, API routes, task lifecycle, and
  storage model. Read it first. The references below are written for maintainers,
  not end users.
- **[CLAUDE.md](CLAUDE.md)** holds the project's
  commit and workflow conventions for both humans and coding agents.
- **[Specs & Roadmap](specs/README.md)** tracks design work in progress and the
  dependency graph between tracks.

## Build & test

`make` targets run gofmt, golangci-lint, the `vue-tsc` typecheck, and the repo's
otel/truncation guardrails. Raw `go build`/`go vet` skip lint and can land code
that fails CI, so prefer the targets.

```bash
make build          # Full gate: fmt + lint + frontend build + binary
make test           # lint + backend tests + frontend tests (matches CI)
make lint           # Lint only (fastest style check)
make fmt            # Format Go in place
make server         # Build and run the Go server natively
```

See [Development Setup](docs/internals/development.md) for the full target list,
E2E scripts, and the release workflow.

On macOS, `go test ./internal/handler/` runs ~5m in isolation but can exceed the
10m default timeout under concurrent machine load (parallel builds or agents):
the explorer file-stream tests watch files via fsnotify, and when the CPU is
contended kqueue drops change events, so each stream test falls back to the 3s
poll ticker (`explorerFilePollInterval`) and the ~1100-test package overruns.
Each test passes in isolation, and CI (Linux/inotify, `-timeout 20m`) is green,
so this is a local-macOS ergonomics issue, not a correctness one. Locally, pass
`-timeout 20m` or scope to the package under test rather than `./...`.

## Conventions

- **Every bug fix ships with a regression test** that fails without the fix and
  passes with it. This holds across backend, frontend, and CLI.
- **Run `make build` before committing** to catch formatting, lint, and
  typecheck failures locally. `make test` adds the backend and frontend test
  suites and is what CI runs.
- **Keep commits small** and scoped to one logical change. Use imperative,
  scoped messages: `internal/runner: ...`, `frontend: ...`, `docs: ...`.
- **Update docs** when a change touches an API route, CLI flag, env variable,
  data model field, or user-visible behavior. User docs live in
  [`docs/guide/`](docs/guide/); internals in [`docs/internals/`](docs/internals/).

Commit and workflow conventions are in [CLAUDE.md](CLAUDE.md); the same file is
what coding agents working in this repository read.
