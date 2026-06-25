---
title: buildContainerSpecForSandbox respects host mode
status: archived
depends_on:
  - specs/shared/host-exec-mode/runner-host-switch.md
affects:
  - internal/runner/container.go
  - internal/runner/container_builder_test.go
effort: medium
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---


# buildContainerSpecForSandbox respects host mode

## Goal

When the runner is in host mode, produce a `ContainerSpec` that uses **host paths** (not `/workspace/<basename>`) and surfaces board / instructions / sibling-worktree context via `WALLFACER_*` environment variables instead of bind mounts. The `LocalBackend` code path must be unchanged for backend=local.

## What to do

1. In `internal/runner/container.go`, thread `r.HostMode()` into the builder. Inside `buildContainerSpecForSandbox` (line 87) and `buildBaseContainerSpec` (line 319), branch on `hostMode := r.HostMode()`:

   - **Workspace worktree mount** (loop around line 128): when `hostMode`, skip `append(spec.Volumes, ...)` but still compute `basenames` (used for `workdirForBasenames`).
   - **`.git` mirror mount for worktrees** (lines 141–158): skip when `hostMode` (the worktree's `.git` file points at an absolute host path, which is already correct on the host).
   - **`appendInstructionsMount`** (line 236): when `hostMode`, do not append a mount; instead, set `spec.Env["WALLFACER_INSTRUCTIONS_PATH"]` to the host instructions path (if it exists). Move the decision branch into a new helper `applyInstructionsForHost` so `appendInstructionsMount` stays focused on the mount path.
   - **Board context mount** (line 171): when `hostMode`, skip the volume; set `spec.Env["WALLFACER_BOARD_JSON"] = filepath.Join(boardDir, "board.json")` (the runner already writes `board.json` there).
   - **Sibling worktree mounts** (lines 186–203): when `hostMode`, skip the volumes; write a JSON manifest to a scratch file under `boardDir/sibling_worktrees.json` encoding the same `shortID → (repoPath → worktreePath)` structure, and set `spec.Env["WALLFACER_SIBLING_WORKTREES_JSON"]` to that path. Skip entirely if `siblingMounts` is empty.
   - **`claude-config` named volume** (line 332 in `buildBaseContainerSpec`): skip in `hostMode`.
   - **`appendCodexAuthMount`** (line 287): skip in `hostMode`.
   - **`appendDependencyCacheVolumes`** (line 362): skip in `hostMode`.
   - **`spec.WorkDir`** (line 208): in `hostMode`, set it to the host worktree path directly. Use `worktreeOverrides[ws]` if present, else `ws` itself. For multi-workspace, pick the first workspace's host path as CWD (matches the current "fallback CWD" behavior for single-workspace; document that host mode does not support the `/workspace` pseudo-root).
   - **`spec.CPUs` / `spec.Memory`** (lines 341–342): leave the fields set — `HostBackend` ignores them. Add a one-line Debug log when `hostMode && (CPUs!="" || Memory!="")` noting they will be ignored.
   - **`spec.Network`** (line 340): leave as-is; `HostBackend` ignores it.
   - **`spec.Entrypoint`** (line 339): in `hostMode`, clear it. The sandbox-agents entrypoint dispatches via `WALLFACER_AGENT`; on host mode the binary is already the right CLI.

2. Extract a helper `func (r *Runner) applyHostEnv(spec *sandbox.ContainerSpec, instrPath, boardDir string, siblingMounts map[string]map[string]string)` that performs the env-var injection and scratch manifest write. Keep the branching in `buildContainerSpecForSandbox` narrow (one `if hostMode` with the helper call).

3. Ensure `spec.Env` map is allocated before writes — currently `buildBaseContainerSpec` initializes it, so ordering is fine, but a new unit test guards against future regressions.

## Tests

In `internal/runner/container_builder_test.go` (alongside existing `buildContainerSpecForSandbox` coverage):

- `TestBuildContainerSpec_HostMode_WorktreeMountOmitted` — use a `MockSandboxBackend` wired for host mode (set `r.hostMode = true` via a new exported test helper `SetHostModeForTest(t, r, true)` or plumb via `runner.New`); assert `spec.Volumes` contains no `/workspace/*` entries.
- `TestBuildContainerSpec_HostMode_WorkDirIsHostPath` — with `worktreeOverrides` supplying a host path; assert `spec.WorkDir == <hostPath>`.
- `TestBuildContainerSpec_HostMode_InstructionsViaEnv` — with an existing instructions file; assert `spec.Env["WALLFACER_INSTRUCTIONS_PATH"] == hostPath` and no mount is appended.
- `TestBuildContainerSpec_HostMode_BoardJSONEnv` — with a boardDir containing `board.json`; assert the env var is set to the full host path and no `/workspace/.tasks/` mount appears.
- `TestBuildContainerSpec_HostMode_SiblingWorktreesManifest` — with two sibling entries; assert the manifest file is written under `boardDir/sibling_worktrees.json` with the expected JSON structure, and `spec.Env["WALLFACER_SIBLING_WORKTREES_JSON"]` points at it.
- `TestBuildContainerSpec_HostMode_SkipsClaudeConfigVolume` — assert no named volume with `Host == "claude-config"` is present.
- `TestBuildContainerSpec_HostMode_SkipsCodexAuthAndCaches` — codex sandbox; assert no codex `auth.json` mount and no dependency cache volumes.
- `TestBuildContainerSpec_HostMode_ClearsEntrypoint` — assert `spec.Entrypoint == ""`.
- `TestBuildContainerSpec_LocalMode_Unchanged` — regression guard: with `hostMode=false`, snapshot the full spec (volumes, env, workdir, entrypoint) and assert it matches the pre-change behavior for a representative fixture.

## Boundaries

- Do **not** change `ContainerSpec.Build()` — host mode does not call it.
- Do **not** touch `HostBackend` in this task — it already expects absolute `spec.WorkDir` and reads the `WALLFACER_INSTRUCTIONS_PATH` env var (per `host-backend.md`).
- Do **not** add backwards-compat aliases for the old `/workspace/*` paths inside host mode — the absence of container paths is deliberate.
- Do **not** introduce new `ContainerSpec` fields; all information flows through `Env` and `WorkDir`.
