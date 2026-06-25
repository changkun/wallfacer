---
title: Harness interface and package skeleton
status: archived
depends_on:
  - specs/shared/host-default.md
affects:
  - internal/harness/
  - internal/sandbox/sandbox.go
effort: small
created: 2026-06-01
updated: 2026-06-15
author: changkun
dispatched_task_id: null
---


# Harness interface and package skeleton

## Goal

Land `internal/harness/` as a new package with the `Harness` interface, canonical `Event` / `Request` / `Capabilities` types, and a registry. No production caller migrates in this task — that follows in [claude-and-codex-migration](claude-and-codex-migration.md). This task ships only the type definitions, a `Registry` with `Register` / `Lookup` / `All`, the `ID` enum constants for the five Tier-A harnesses, and tests against a fake harness.

## What to add

### `internal/harness/harness.go`

- `type ID string` with constants `Claude`, `Codex`, `Cursor`, `OpenCode`, `Pi`.
- `Harness` interface as specced in the parent ([harness-abstraction.md](../harness-abstraction.md)).
- `Request`, `Event`, `EventKind`, `Capabilities`, `Permission`, `Usage`, `ToolCall`, `MCPServer`, `AuthConfig` value types.
- `EventKind` constants: `KindSystemInit`, `KindAssistantText`, `KindToolCallStart`, `KindToolCallEnd`, `KindUserResult`, `KindResult`, `KindError`, `KindUnknown`.
- `Permission` constants: `PermissionReadOnly`, `PermissionEdit`, `PermissionFull`.

### `internal/harness/registry.go`

- Package-level registry keyed by `ID`.
- `Register(h Harness)` — panics on duplicate; called from each harness file's `init()`.
- `Lookup(id ID) (Harness, bool)`.
- `All() []ID` — sorted, deterministic.
- `Default() ID` — returns `Claude` for now; configurable via `WALLFACER_DEFAULT_HARNESS` env in a follow-up.

### `internal/harness/auth.go`

- `AuthConfig` shape — flat struct with named fields rather than `map[string]string`, so each harness reads what it needs without stringly-typed coupling. Initial fields:
  - `AnthropicAPIKey`, `ClaudeOAuthToken` (Claude)
  - `OpenAIAPIKey`, `CodexAuthFile` (Codex)
  - `CursorAPIKey` (Cursor)
  - `OpenCodeServerURL`, `OpenCodeServerPassword` (OpenCode)
  - `PiAPIKey` (Pi)
- A future spec migrates env-config to populate this from `~/.wallfacer/.env`.

### `internal/harness/testing.go` (build tag `!production` or test-only)

- `FakeHarness` — a programmable harness used by tests in dependent specs. Records calls, returns configured argv / events, satisfies the interface.

### `internal/sandbox/sandbox.go` — compat shim

- Keep `sandbox.Type`, `Claude`, `Codex` constants as type aliases pointing at `harness.ID`, `harness.Claude`, `harness.Codex`. Deprecation comments only; nothing migrates yet.
- This shim is removed by the cleanup phase of [claude-and-codex-migration](claude-and-codex-migration.md).

## What not to do here

- No production code calls `harness.Lookup`; the runner, handlers, env config are untouched.
- No `BuildArgv` / `ParseEvent` implementations land for real harnesses — only the `FakeHarness` test double.
- No UI changes.
- No docs additions beyond `internal/harness/doc.go`.

## Tests

- `harness_test.go`: registry round-trips; `All()` is sorted; duplicate `Register` panics; `Lookup` of unknown returns `false`.
- `events_test.go`: `EventKind` round-trips via JSON marshalling; `Event` zero value is valid.
- `fake_test.go`: `FakeHarness` records calls and produces configured output.
- `capabilities_test.go`: `Capabilities` zero value documents "nothing supported" (every bool is false) so callers can safely test against it without panic.

## Acceptance criteria

- `go build ./internal/harness/...` passes.
- `go test ./internal/harness/...` passes.
- Importing `harness` from anywhere outside `internal/harness/` is allowed but **no file outside the package imports it yet** — `grep -r '"latere.ai/x/wallfacer/internal/harness"' --include='*.go' .` returns only files under `internal/harness/`.
- The compat shim in `internal/sandbox/sandbox.go` makes existing tests pass unchanged.

## Why isolate this task

It carries no behavior change and no migration risk. Reviewers can focus on the interface shape — which is the highest-leverage decision in the harness work. Getting it wrong here costs a rewrite of every adapter; getting it right makes each adapter a ~150-line file.
