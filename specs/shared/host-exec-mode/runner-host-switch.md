---
title: Runner selects HostBackend when backend=host
status: validated
depends_on:
  - specs/shared/host-exec-mode/host-backend.md
  - specs/shared/host-exec-mode/envconfig-host-option.md
affects:
  - internal/runner/runner.go
  - internal/runner/runner_test.go
effort: small
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# Runner selects HostBackend when backend=host

## Goal

Wire `cfg.SandboxBackend == "host"` into `runner.New` so the runner constructs a `HostBackend` instead of a `LocalBackend`. Expose a `Runner.HostMode() bool` accessor so downstream builders (container spec, UI banner) can branch without inspecting the backend concrete type.

## What to do

1. In `internal/runner/runner.go` around line 416, extend the backend switch:

   ```go
   switch cfg.SandboxBackend {
   case "", "local":
       r.backend = sandbox.NewLocalBackend(r.command, localCfg)
   case "host":
       hb, err := sandbox.NewHostBackend(sandbox.HostBackendConfig{
           ClaudeBinary: parsed.HostClaudeBinary,
           CodexBinary:  parsed.HostCodexBinary,
       })
       if err != nil {
           return nil, fmt.Errorf("host sandbox backend: %w", err)
       }
       r.backend = hb
       r.hostMode = true
   default:
       logger.Runner.Warn("unknown sandbox backend, falling back to local", "backend", cfg.SandboxBackend)
       r.backend = sandbox.NewLocalBackend(r.command, localCfg)
   }
   ```

   Note: `parsed` is the already-parsed envconfig inside `runner.New` (grep for where `parsed.TaskWorkers` is read today — same variable).

2. Add a new field to `Runner`:

   ```go
   hostMode bool
   ```

   Document that when true, `buildContainerSpecForSandbox` must translate container paths to host paths and suppress mounts.

3. Add the accessor:

   ```go
   // HostMode reports whether the runner is using the host sandbox backend.
   func (r *Runner) HostMode() bool { return r.hostMode }
   ```

4. Update the `RunnerInterface` / `RunnerLike` interface in `internal/runner/interface.go` (grep `SandboxBackend() sandbox.Backend`) to add `HostMode() bool`. Implement it in `MockRunner` (`internal/runner/mock.go`) as `func (m *MockRunner) HostMode() bool { return m.hostMode }` with a corresponding `hostMode` field on the mock so tests can set it.

5. Propagate the `runner.New` signature if needed — no callsite should need to change because the selection is config-driven.

## Tests

In `internal/runner/runner_test.go` (or the closest existing test file that covers `runner.New`):

- `TestRunnerNew_HostBackend` — construct with `cfg.SandboxBackend="host"`, with a fake claude/codex on `$PATH`; assert `r.HostMode() == true` and `r.SandboxBackend()` is non-nil. Use the same fakeagent helper from `host-backend.md` — or a lighter build-time fake script on PATH.
- `TestRunnerNew_HostBackend_MissingBinary` — construct with `cfg.HostClaudeBinary="/nonexistent"`; assert `runner.New` returns an error mentioning "host sandbox backend".
- `TestRunnerNew_LocalBackend_HostModeFalse` — default backend; assert `r.HostMode() == false`.
- `TestRunnerNew_UnknownBackend_FallsBackToLocal` — `cfg.SandboxBackend="k8s"`; assert warn logged, `r.HostMode() == false`.

## Boundaries

- Do **not** add the `MAX_PARALLEL=1` default here — that is `host-parallel-cap.md`.
- Do **not** modify `buildContainerSpecForSandbox` in this task — `container-spec-host-mode.md` does that, using the `HostMode()` accessor this task provides.
- Do **not** change the `Runner` exported type or break existing callers; only add a field and a method.
- Do **not** read `WALLFACER_SANDBOX_BACKEND` directly — always go through `cfg`/`envconfig`.
