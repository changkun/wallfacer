---
title: Planning Chat Agent — Codex Compatibility
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-chat-agent.md
affects:
  - internal/planner/planner.go
  - internal/planner/spec.go
  - internal/cli/server.go
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

The planner's `Exec()` path calls `p.buildContainerSpec(containerName, sandbox.Claude)`
unconditionally (`planner.go:137`). `buildContainerSpec` already accepts
`sandbox.Type` and handles AGENTS.md mounting, image rewriting, and entrypoint
selection correctly — the hard-code is the only gap.

Two concrete changes are needed:

1. **`planner.Config` needs a `Sandbox sandbox.Type` field** so callers can
   configure the planning sandbox the same way task activities are configured.
   The runner sets its sandbox via `envconfig` fields; the planner needs an
   equivalent.

2. **Codex auth mount in the planning container spec** — the task runner
   applies `appendCodexAuthMount()` in `container.go:280-292` when
   `sb == sandbox.Codex`. The planner's `spec.go` does not. When the planning
   sandbox is Codex, it must also mount `~/.codex` → `/home/codex/.codex`
   (read-only) so the Codex CLI can authenticate. The host path is resolved via
   `hostCodexAuthPath()` in `runner.go:708-724` — the planner will need access
   via its `Config`.

## Remaining Work

1. **Add `Sandbox sandbox.Type` and `CodexAuthPath string` to `planner.Config`**
   (`internal/planner/planner.go`). The `Config` struct (lines 24-36) currently
   has no sandbox field; `New()` does not store one. Default `Sandbox` to
   `sandbox.Claude` when zero-value so existing callers are unaffected. Store
   the field on `Planner` and pass it to `buildContainerSpec` at line 137
   instead of the hard-coded `sandbox.Claude`.

2. **Add Codex auth mount to planning spec** (`internal/planner/spec.go`).
   `appendInstructionsMount()` (lines 87-109) already branches on `sandbox.Type`
   for CLAUDE.md vs AGENTS.md — add a new `appendCodexAuthMount()` alongside
   it. When `sb == sandbox.Codex` and `CodexAuthPath` is non-empty and resolves
   to a directory containing `auth.json`, append a read-only bind mount of that
   path to `/home/codex/.codex`. Call it from `buildPlannerContainerSpec` after
   `appendInstructionsMount`.

3. **Wire sandbox selection at call site** (`internal/cli/server.go`).
   The planner is constructed in `server.go` with `planner.New(planner.Config{...})`.
   `codexAuthPath` is already computed (lines 140-143) and `envCfg` already
   carries sandbox configuration — both just need to be forwarded to
   `planner.Config`. Read the planning sandbox from `envCfg` (reuse
   `DefaultSandbox` or add `WALLFACER_SANDBOX_PLANNING` if independent
   control is needed); pass `Sandbox` and `CodexAuthPath` fields.

4. **Tests** — add `TestBuildContainerSpec_Codex` to
   `internal/planner/planner_test.go` asserting that when
   `Config.Sandbox = sandbox.Codex` the resulting spec uses the Codex image,
   mounts `AGENTS.md` (not `CLAUDE.md`), and includes the Codex auth bind mount.
   `TestAppendInstructionsMount_Codex` (lines 460-480) already tests the
   instructions branching — extend that pattern for the full spec.

## Affects

- `internal/planner/planner.go` — add `Sandbox sandbox.Type` and `CodexAuthPath string` to `Config` and `Planner`; replace hard-coded `sandbox.Claude` at line 137
- `internal/planner/spec.go` — add `appendCodexAuthMount` equivalent called from `buildPlannerContainerSpec`
- `internal/cli/server.go` — forward `codexAuthPath` and sandbox selection to `planner.Config`
