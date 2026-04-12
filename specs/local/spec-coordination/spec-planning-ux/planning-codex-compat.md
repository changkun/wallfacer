---
title: Planning Chat Agent — Codex Compatibility
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-chat-agent.md
affects:
  - internal/planner/planner.go
  - internal/planner/spec.go
effort: small
created: 2026-04-03
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Planning Chat Agent — Codex Compatibility

## Current State

The task runner (`internal/runner/`) is fully Codex-compatible. The planner
(`internal/planner/`) is not — it hard-codes `sandbox.Claude` despite the
spec-assembly layer already being sandbox-aware. The following infrastructure
is already in place and does **not** need to change:

- **CLI flag translation** — The `sandbox-codex` image ships an entrypoint
  wrapper (`codex.sh`) that translates Claude Code flags (`-p`, `--verbose`,
  `--output-format stream-json`, `--model`) into Codex CLI invocations and
  wraps the output back into Claude-compatible stream-json. The planner's
  command construction in `planning.go` works unchanged for Codex.
- **`--resume` skipped transparently** — `codex.sh` explicitly skips the
  `--resume` flag (line 32-33); Codex manages multi-turn continuity via its
  own internal `session_id`. `planner/conversation.go`'s `ExtractSessionID()`
  already reads both `session_id` (Claude) and `thread_id` (Codex), so session
  persistence survives sandbox switching.
- **Instructions file** — `internal/planner/spec.go`'s `appendInstructionsMount()`
  already branches on `sandbox.Type`: Codex gets `AGENTS.md`, Claude gets
  `CLAUDE.md` (lines 97-99 of `spec.go`).
- **Mount layout compatibility** — The workspace-RO / `specs/`-RW overlay is
  a kernel-level bind-mount arrangement, not a sandbox concern. Works for both.

## Design Problem

The planner's `Start()` / `Exec()` path calls
`p.buildContainerSpec(containerName, sandbox.Claude)` unconditionally
(`planner.go` line 137). `buildContainerSpec` already accepts `sandbox.Type`
and handles AGENTS.md mounting, image rewriting, and entrypoint selection
correctly — the hard-code is the only gap.

Two concrete changes are needed:

1. **`planner.Config` needs a `Sandbox sandbox.Type` field** so callers can
   configure the planning sandbox the same way task activities are configured.
   The runner sets its sandbox via `envconfig` fields
   (`WALLFACER_SANDBOX_IMPLEMENTATION`, etc.); the planner needs an equivalent.

2. **Codex auth mount in the planning container spec** — the task runner
   applies `appendCodexAuthMount()` in `container.go` (lines 280-292) when
   `sb == sandbox.Codex`. The planner's `spec.go` does not. When the planning
   sandbox is Codex, it must also mount `~/.codex/auth.json` →
   `/home/codex/.codex` (read-only) so the Codex CLI can authenticate.
   The host path is resolved via `hostCodexAuthPath()` in `runner.go` (lines
   708-724) — the planner will need access to the same path, either via its
   `Config` or by reusing the helper.

## Remaining Work

1. **Add `Sandbox sandbox.Type` to `planner.Config`** (`internal/planner/planner.go`).
   Default to `sandbox.Claude` when the field is zero-value so existing callers
   are unaffected. Pass the configured value to `buildContainerSpec` instead of
   the hard-coded `sandbox.Claude`.

2. **Add Codex auth mount to planning spec** (`internal/planner/spec.go`).
   Mirror the runner's `appendCodexAuthMount()` pattern: when `sb == sandbox.Codex`
   and the host auth path resolves to a valid directory (contains `auth.json`),
   append a read-only bind mount of `~/.codex` to `/home/codex/.codex`. The
   host path should be passed through `planner.Config` (e.g., `CodexAuthPath string`)
   so the planner stays decoupled from the runner.

3. **Wire sandbox selection at call site** (`internal/handler/planning.go` or
   wherever `planner.New(cfg)` is constructed). Read the desired sandbox for
   planning from envconfig — either a new `WALLFACER_SANDBOX_PLANNING` variable
   or reuse `DefaultSandbox`. Pass it as `cfg.Sandbox` when constructing the
   planner.

4. **Tests** — add a test asserting that when `planner.Config.Sandbox = sandbox.Codex`
   the resulting container spec uses the Codex image, mounts `AGENTS.md`, and
   includes the Codex auth bind mount. Follow the pattern in
   `internal/runner/container_spec_test.go`.

## Affects

- `internal/planner/planner.go` — add `Sandbox sandbox.Type` (and `CodexAuthPath string`) to `Config`; replace hard-coded `sandbox.Claude` in `buildContainerSpec` call
- `internal/planner/spec.go` — add `appendCodexAuthMount` equivalent when `sb == sandbox.Codex`
