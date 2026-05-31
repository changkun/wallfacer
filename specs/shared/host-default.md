---
title: Host as the Only Local Backend
status: drafted
depends_on:
  - specs/shared/host-exec-mode.md
affects:
  - internal/sandbox/
  - internal/runner/container.go
  - internal/runner/runner.go
  - internal/runner/interface.go
  - internal/handler/handler.go
  - internal/handler/config.go
  - internal/handler/images.go
  - internal/handler/env.go
  - internal/handler/sandbox_gate.go
  - internal/handler/planning.go
  - internal/envconfig/envconfig.go
  - internal/cli/server.go
  - internal/cli/doctor.go
  - internal/constants/
  - Makefile
  - Dockerfile
  - deploy/
  - scripts/e2e-lifecycle.sh
  - docs/guide/
  - ui/partials/settings-tab-sandbox.html
  - ui/js/images.js
effort: large
created: 2026-06-01
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Host as the Only Local Backend

## Problem

Wallfacer's local mode runs every harness invocation inside a podman/docker container. The container does four jobs today:

1. Process isolation between concurrent tasks.
2. Filesystem write boundary (worktree mount as `/workspace/<basename>`).
3. Reproducible toolchain (node, git, jq, `codex-agent.sh`).
4. Network policy — except the container already runs `--network=host`, so this is moot.

It has also become the top onboarding friction: GHCR pulls fail on flaky networks, image tags drift, users on machines that already have `claude` / `codex` installed must still install a container runtime, and the Codex container needs a custom `codex-agent.sh` wrapper script. [host-exec-mode.md](host-exec-mode.md) shipped `--backend host` as an opt-in escape hatch for exactly these reasons; in practice it's now the path that "just works" for new users while the container path is a permanent maintenance tax.

Modern coding-agent CLIs all have a documented headless / auto-permission mode that obviates job (1) and (2) in practice:

| Harness | Headless flag |
|---|---|
| Claude Code | `--permission-mode auto` / `bypassPermissions` |
| Codex | `--ask-for-approval never --sandbox workspace-write` (Seatbelt / Landlock at the OS layer) |
| Cursor | `--force --trust --approve-mcps` |
| OpenCode | `build` mode |
| Pi | `--mode json` + tool gating |

Wallfacer's per-task git worktree already bounds blast radius to a single directory. The container is no longer pulling its weight in local mode.

## Decision

**Remove the container backend from local mode entirely.** Host becomes the only execution path; the `--backend` flag goes away. Cloud / multi-tenant execution is unaffected — it has its own runtime ([cella-runtime.md](../cloud/latere-integration/cella-runtime.md)), which speaks to Cella's API, not the local container backend.

## Scope

### What goes away

- `internal/sandbox/local.go`, `worker.go` — `LocalBackend` and the per-task worker-container plumbing.
- The `--backend` CLI flag and `SandboxBackend` env knob.
- Container-spec building specific to image execution: `buildContainerSpecForSandbox`, `/workspace/<basename>` path translation, `--network=host`, `--mount type=bind,readonly`, named-volume management.
- The `codex-agent.sh` script and its container baking — Codex argv translation already lives on the host path in `host_codex.go`.
- Image cache / pull UI surface: `/api/images*` routes, settings → sandbox image picker, `make pull-sandbox-images`.
- Doctor checks for podman/docker reachability and image presence.
- `Dockerfile` (the agent image — not `Dockerfile.web` which builds wallfacer itself).
- The container-mode lane of `scripts/e2e-lifecycle.sh`.

### What stays

- `internal/sandbox/host.go` and `host_codex.go` — promoted to the only path. Both lose their "fall back to container" wrappers and get renamed/moved as part of [harness-abstraction.md](harness-abstraction.md).
- `ContainerSpec` as the launch struct (renamed in the harness work, not here) — the value object is reused by host launch; only the build-for-podman semantics go.
- `sandbox.Type` enum during this spec; renamed to `harness.ID` in the follow-up work.
- Cloud / Cella execution paths.
- Worktree-per-task isolation. Per-task worker containers were a container-mode optimization; on host we already get fresh processes for free.

### What this spec does NOT touch

- `Harness` interface and per-harness adapters live in [harness-abstraction.md](harness-abstraction.md). This spec keeps the current Claude/Codex code paths running unchanged in host-only form.
- Remote / Topos execution lives under cloud. This spec is local-only.

## Migration Plan

1. **Flip the default and warn.** Make `--backend host` the default. If the user explicitly passes `--backend container` (or sets the env var), log a deprecation warning pointing to this spec.
2. **Remove the runtime switch.** Drop `r.backend` indirection in `internal/runner/runner.go`; call host launch directly. Update `runner.Interface` and `mock.go`.
3. **Delete container code.** Remove `local.go`, `worker.go`, the spec-builder branches that emit `/workspace/<basename>`, network/CPU/memory container flags, the `--mount` bind logic in `spec.go`.
4. **Strip the UI.** Settings → Sandbox tab loses the image-cache panel and the backend toggle; doctor loses the podman/docker check; `/api/images*` and `/api/containers` routes return 410 Gone for one release then are removed.
5. **Strip the build.** Drop `Dockerfile`, `pull-sandbox-images` and related Makefile targets, GHCR publish workflow for the agent image.
6. **Strip the script.** E2E lifecycle drops its container lane; the host lane is renamed to the canonical lane.
7. **Documentation pass.** Update `docs/guide/usage.md`, `docs/guide/configuration.md`, `docs/internals/*` to reflect the host-only architecture.

Each step is a separable commit; the order keeps the tree green at every point.

## Risks

| Risk | Mitigation |
|---|---|
| User has neither claude nor codex on PATH | `wallfacer doctor` already enumerates host-installed CLIs; promote it to a hard gate on first run. |
| User runs multiple concurrent tasks editing the same worktree | Per-task worktrees already prevent this; unchanged. |
| Harness goes rogue and writes outside the worktree | Mostly bounded by harness's own sandbox/permission mode. For users who want OS-level enforcement, document Codex's `--sandbox workspace-write` (kept verbatim from current host path) and Claude's `--add-dir`-only mount list. Hard sandboxing is the cloud Cella path. |
| Lost reproducibility — host toolchain version drift | Doctor reports `claude --version` / `codex --version`; record in task metadata for postmortem. |
| Users currently on `--backend container` get broken | Deprecation warning for one release before removal; documented migration note. |

## Open Questions

- Do we keep `Dockerfile.web` (the wallfacer-server image used for cloud deploys)? Yes — it's separate from the agent image and is the cloud entrypoint.
- Does removing `/api/containers` break the planning sandbox? The planning sandbox is itself a container today (`internal/planner/`); that's tracked separately under cloud / planning-ux specs and is out of scope here. Initial implementation keeps planning's container path; a follow-up spec migrates it to host process.

## Test Plan

- `make test` must stay green at every commit.
- E2E lifecycle script (host lane) must continue passing for both Claude and Codex.
- New regression test: starting the server with `--backend container` emits a deprecation warning and either still works (during transition) or exits with a clear pointer to this spec (after removal).
- Doctor exits non-zero when neither harness CLI is installed.

## Why a separate spec from harness abstraction

Removing the container is an orthogonal change from refactoring Claude/Codex into a `Harness` interface. They can ship independently; this spec is the prerequisite that makes the harness work clean (no `/workspace/<basename>` translation to carry through). Bundling them would conflate "we're dropping containerization" with "we're abstracting over harnesses" — two distinct decisions reviewers should be able to accept or reject independently.
