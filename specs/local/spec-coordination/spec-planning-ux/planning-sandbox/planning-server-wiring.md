---
title: Wire planner into server lifecycle
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox/planning-api.md
affects:
  - internal/cli/server.go
  - internal/handler/handler.go
  - internal/workspace/
effort: small
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Wire planner into server lifecycle

## Goal

Integrate the planner package into the server's startup, shutdown, and workspace-switching flows so the planning container is properly created, destroyed, and recreated when workspaces change.

## What to do

1. In `internal/cli/server.go`, during server initialization (where the runner and handler are constructed):
   - Create a `planner.Planner` instance using `planner.New(planner.Config{...})`.
   - Pass the same `sandbox.Backend` instance used by the runner.
   - Pass the container runtime command (`command` variable, e.g., `/opt/podman/bin/podman`).
   - Pass the initial workspace paths and fingerprint from the workspace manager.
   - Pass the env file path.
   - Pass the planner to the handler constructor.

2. In the server's graceful shutdown path (where `runner.Shutdown()` is called):
   - Call `planner.Stop()` to destroy the planning container before the sandbox backend is closed.
   - This must happen before the process exits to avoid orphaned containers.

3. In the workspace switching flow (where `PUT /api/workspaces` triggers a workspace change):
   - After the workspace manager updates the active workspace set, call `planner.UpdateWorkspaces(newPaths, newFingerprint)`.
   - This destroys the old planning container (if running) and allows a new one to be started with the updated mounts.
   - Look at how the runner handles workspace switches (the `storeMu` pattern in `runner.go`) for reference.

4. In `internal/handler/handler.go`, ensure the planner field is included in the handler's `Close()` or shutdown method if one exists.

## Tests

- `TestServerStartupCreatesPlanner`: Verify that server initialization creates a planner instance (integration test or constructor test).
- `TestServerShutdownStopsPlanner`: Verify that graceful shutdown calls `planner.Stop()`.
- `TestWorkspaceSwitchUpdatePlanner`: Verify that switching workspaces via `PUT /api/workspaces` calls `planner.UpdateWorkspaces()` with the new paths and fingerprint.
- `TestWorkspaceSwitchWhilePlanningRunning`: Verify that switching workspaces while the planning container is running stops the old container. After the switch, `IsRunning()` returns false and a new `Start()` uses the new workspace mounts.

## Boundaries

- Do NOT add new API endpoints — those are in the `planning-api` task.
- Do NOT modify the planner package itself — that's the `planner-core` task.
- Do NOT handle planning session persistence (conversation history) across workspace switches — that's the planning-chat-agent sub-design spec.
- Do NOT add planning-specific circuit breaker logic — the existing circuit breaker is for task containers only. Planning container failures are handled by the planner's own error returns.
