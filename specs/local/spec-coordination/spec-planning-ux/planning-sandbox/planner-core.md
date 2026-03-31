---
title: Create planner package with container lifecycle
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox/planning-activity.md
affects:
  - internal/planner/
  - internal/sandbox/
effort: large
created: 2026-03-30
updated: 2026-03-31
author: changkun
dispatched_task_id: null
---

# Create planner package with container lifecycle

## Goal

Create `internal/planner/` — the core package that manages the planning sandbox container lifecycle. The planner owns a singleton long-lived worker container keyed by workspace fingerprint, uses `podman exec` for each planning round, and builds planning-specific container specs (full workspace read-only + `specs/` read-write).

## What to do

1. Create `internal/planner/planner.go` with the `Planner` struct:
   ```go
   type Planner struct {
       backend   sandbox.Backend
       command   string          // container runtime path (podman/docker)
       mu        sync.Mutex
       container *planningWorker // nil when no planning session active
       fingerprint string       // workspace fingerprint for keying
       workspaces  []string     // current workspace paths
       envFile     string       // path to .env file
   }

   type Config struct {
       Backend    sandbox.Backend
       Command    string
       Workspaces []string
       EnvFile    string
       Fingerprint string
   }

   func New(cfg Config) *Planner
   ```

2. Create `internal/planner/worker.go` with `planningWorker` — a long-lived container that accepts exec commands. Model after `internal/sandbox/worker.go` (`taskWorker`) but keyed by workspace fingerprint instead of task ID:
   ```go
   type planningWorker struct {
       mu            sync.Mutex
       command       string
       containerName string
       createArgs    []string
       entrypoint    string
       alive         bool
   }

   func (w *planningWorker) ensureRunning(ctx context.Context) error
   func (w *planningWorker) exec(ctx context.Context, cmd []string, workDir string) (sandbox.Handle, error)
   func (w *planningWorker) stop()
   ```

3. Create `internal/planner/spec.go` with the container spec builder:
   ```go
   func (p *Planner) buildContainerSpec(containerName string, sb sandbox.Type) sandbox.ContainerSpec
   ```
   Mount configuration:
   - Each workspace directory mounted at `/workspace/<basename>` with options `z,ro` (read-only)
   - The `specs/` subdirectory of each workspace mounted at `/workspace/<basename>/specs` with options `z` (read-write, overriding the parent read-only mount)
   - Instructions file (`CLAUDE.md`) mounted read-only at `/workspace/CLAUDE.md` (or per-workspace if single workspace)
   - `claude-config` named volume at `/home/claude/.claude`
   - Working directory set via `workdirForBasenames()` pattern
   - Use the same sandbox image as task containers (reuse `sandboxImageForSandbox()` pattern)
   - Apply resource limits from env config (same as task containers initially; can be differentiated later)

4. Implement lifecycle methods on `Planner`:
   ```go
   func (p *Planner) Start(ctx context.Context) error     // Create worker container if not exists
   func (p *Planner) Stop()                                // Stop and remove worker container
   func (p *Planner) IsRunning() bool                      // Check if worker is alive
   func (p *Planner) Exec(ctx context.Context, cmd []string) (sandbox.Handle, error) // Run a command in the planning container
   func (p *Planner) UpdateWorkspaces(workspaces []string, fingerprint string) // Destroy and recreate on workspace switch
   ```

5. Create `internal/planner/planner_test.go` with unit tests.

## Tests

- `TestPlannerNew`: Verify `New()` returns a valid Planner with correct config fields.
- `TestPlannerBuildContainerSpec`: Verify the container spec has correct mounts:
  - Workspace directories are read-only (`z,ro`)
  - `specs/` subdirectories are read-write (`z`)
  - Instructions file is mounted read-only
  - `claude-config` named volume is present
  - Working directory is set correctly for single and multi-workspace cases
- `TestPlannerBuildContainerSpecMultiWorkspace`: Verify mount construction with 2+ workspaces (each gets read-only mount + specs read-write override).
- `TestPlannerStartStop`: Verify Start creates a worker, IsRunning returns true, Stop destroys it, IsRunning returns false.
- `TestPlannerUpdateWorkspaces`: Verify that calling UpdateWorkspaces while running stops the old container and allows starting a new one with different mounts.
- `TestPlannerWorkerEnsureRunning`: Verify the worker recreates itself if the container is not running (parallels `taskWorker` tests).
- `TestPlannerWorkerExec`: Verify exec returns a Handle that, when killed, does not stop the worker container (parallels `execHandle` behavior).

## Boundaries

- Do NOT add HTTP handler endpoints — that's the `planning-api` task.
- Do NOT wire into the server or CLI — that's the `planning-server-wiring` task.
- Do NOT implement conversation management or chat — that's the planning-chat-agent sub-design spec.
- Do NOT implement usage tracking or cost attribution — that's the progress-cost-tracking sub-design spec.
- The `planningWorker` struct is internal to the planner package — do not export it or add it to the sandbox package's `WorkerManager` interface.

## Implementation Notes

- **Delegates to sandbox.Backend**: The planner uses `Backend.Launch()` instead of managing containers directly via `cmdexec`. The ContainerSpec carries a stable `wallfacer.task.id=planning-sandbox` label so `LocalBackend` routes through its worker container mechanism automatically. This means the planner has no custom worker, handle, or exec types — the backend handles all of that. This also ensures cloud backends (K8s) work without changes.
- **No worker.go**: The original implementation had a custom `planningWorker` with `planningHandle`/`planningExecHandle` types duplicating sandbox internals. These were removed in favor of delegating to the backend.
- **Container naming**: Container name uses `truncFingerprint()` (first 12 chars) as a suffix.
