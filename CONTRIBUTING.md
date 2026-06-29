# Contributing to Wallfacer

This guide is for developers and contributors working on Wallfacer itself. If
you only want to *use* Wallfacer, start with the [User Manual](docs/guide/usage.md).

## Orientation

- **[Technical Internals](docs/internals/internals.md)** is the canonical map of
  the codebase: architecture, package layout, API routes, task lifecycle, and
  storage model. Read it first. The references below are written for maintainers,
  not end users.
- **[AGENTS.md](AGENTS.md)** (symlinked as `CLAUDE.md`) holds the project's
  commit and workflow conventions for both humans and coding agents.
- **[Specs & Roadmap](specs/README.md)** tracks design work in progress and the
  dependency graph between tracks.

## Technical Internals

These references live in [`docs/internals/`](docs/internals/) and explain
implementation detail, architecture, and the APIs that connect everything.

| # | Reference | Topics |
|---|-----------|--------|
| 1 | [Architecture](docs/internals/architecture.md) | System design, package map, handler organization, end-to-end walkthrough |
| 2 | [Data & Storage](docs/internals/data-and-storage.md) | Data models, persistence, event sourcing, spec document model |
| 3 | [Task Lifecycle](docs/internals/task-lifecycle.md) | State machine, turn loop, dependencies, failure categorization |
| 4 | [Git Operations](docs/internals/git-worktrees.md) | Worktree lifecycle, commit pipeline, branch management |
| 5 | [API & Transport](docs/internals/api-and-transport.md) | HTTP route reference, SSE, WebSocket terminal, middleware |
| 6 | [Automation](docs/internals/automation.md) | Background watchers, autoimplement, circuit breakers, ideation |
| 7 | [Workspaces & Config](docs/internals/workspaces-and-config.md) | Workspace manager, harness routing, templates, env config |
| 8 | [Development Setup](docs/internals/development.md) | Building, testing, make targets, release workflow |

## Build & test

`make` targets run gofmt, golangci-lint, and Biome. Raw `go build`/`go vet`
skip lint and can land code that fails CI, so prefer the targets.

```bash
make build          # Full gate: fmt + lint + frontend build + binary
make test           # lint + backend tests + frontend tests (matches CI)
make lint           # Lint only (fastest style check)
make fmt            # Format Go in place
make server         # Build and run the Go server natively
```

See [Development Setup](docs/internals/development.md) for the full target list,
E2E scripts, and the release workflow.

## Conventions

- **Every bug fix ships with a regression test** that fails without the fix and
  passes with it. No exceptions across backend, frontend, and CLI.
- **Run `make build` before committing.** It is the full gate.
- **Keep commits small** and scoped to one logical change. Use imperative,
  scoped messages: `internal/runner: ...`, `ui: ...`, `docs: ...`.
- **Update docs** when a change touches an API route, CLI flag, env variable,
  data model field, or user-visible behavior. User docs live in
  [`docs/guide/`](docs/guide/); internals in [`docs/internals/`](docs/internals/).

The commit and workflow conventions every change must follow are in
[AGENTS.md](AGENTS.md) (symlinked as `CLAUDE.md`).
