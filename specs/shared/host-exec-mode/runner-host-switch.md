---
title: `wallfacer run --backend host` selects HostBackend
status: complete
depends_on:
  - specs/shared/host-exec-mode/host-backend.md
  - specs/shared/host-exec-mode/envconfig-host-option.md
affects:
  - internal/cli/server.go
  - internal/cli/server_test.go
  - internal/runner/runner.go
  - internal/runner/runner_test.go
effort: small
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# `wallfacer run --backend host` selects HostBackend

## Goal

Add a `--backend` flag to `wallfacer run` that selects the sandbox backend at startup. Default stays `container` (the podman/docker path, internally `"local"`). `host` constructs `HostBackend` and flips the runner into host mode. Expose a `Runner.HostMode() bool` accessor so downstream builders (container spec, UI banner) can branch without inspecting the backend concrete type.

## What to do

1. In `internal/cli/server.go`, inside `runServer` (around line 435 where `fs := flag.NewFlagSet("run", ...)` is set up):

   ```go
   backend := fs.String("backend", "container",
       `sandbox backend: "container" (default, podman/docker) or "host" (exec claude/codex directly; no isolation)`)
   ```

   After `fs.Parse(args)`, translate the flag value to the internal `SandboxBackend` string:

   ```go
   sandboxBackend := strings.ToLower(strings.TrimSpace(*backend))
   switch sandboxBackend {
   case "", "container", "local":
       sandboxBackend = "local" // internal name, unchanged
   case "host":
       // pass through
   default:
       return fmt.Errorf(`unknown --backend value %q: want "container" or "host"`, *backend)
   }
   ```

   Thread `sandboxBackend` into `ServerConfig` (add a `SandboxBackend string` field if missing) so `initServer` can hand it to the runner constructor.

2. In `internal/cli/server.go`, locate where `ServerConfig` is mapped into the runner's own config struct (grep for where `cfg.SandboxBackend` is assigned today, or for the `runner.New` callsite inside `initServer`). Pass the flag-derived value through.

3. In `internal/runner/runner.go` around line 416, extend the backend switch:

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

4. **Do not** read `WALLFACER_SANDBOX_BACKEND` from the env file. If `envconfig` still exposes a `SandboxBackend` field from historical code, do not populate it from the env file in this task. Backend selection is CLI-only.

5. Add a new field to `Runner`:

   ```go
   hostMode bool
   ```

   Document that when true, `buildContainerSpecForSandbox` must translate container paths to host paths and suppress mounts.

6. Add the accessor:

   ```go
   // HostMode reports whether the runner is using the host sandbox backend.
   func (r *Runner) HostMode() bool { return r.hostMode }
   ```

7. Update the `RunnerInterface` / `RunnerLike` interface in `internal/runner/interface.go` (grep `SandboxBackend() sandbox.Backend`) to add `HostMode() bool`. Implement it in `MockRunner` (`internal/runner/mock.go`) as `func (m *MockRunner) HostMode() bool { return m.hostMode }` with a corresponding `hostMode` field on the mock.

8. Update the `wallfacer run` help text (`fs.Usage`) to mention the new flag with a one-line isolation warning.

## Tests

In `internal/cli/server_test.go` (or the closest existing test that exercises `run` flag parsing):

- `TestRun_BackendFlag_Default` — no flag; assert the parsed `ServerConfig.SandboxBackend == "local"`.
- `TestRun_BackendFlag_Host` — `--backend host`; assert `ServerConfig.SandboxBackend == "host"`.
- `TestRun_BackendFlag_ContainerAlias` — `--backend container`; assert `ServerConfig.SandboxBackend == "local"`.
- `TestRun_BackendFlag_Invalid` — `--backend k8s`; assert the error string contains the hint "want \"container\" or \"host\"".

In `internal/runner/runner_test.go`:

- `TestRunnerNew_HostBackend` — construct with `cfg.SandboxBackend="host"`, with a fake claude/codex on `$PATH`; assert `r.HostMode() == true` and `r.SandboxBackend()` is non-nil.
- `TestRunnerNew_HostBackend_MissingBinary` — construct with `cfg.HostClaudeBinary="/nonexistent"`; assert `runner.New` returns an error mentioning "host sandbox backend".
- `TestRunnerNew_LocalBackend_HostModeFalse` — default backend; assert `r.HostMode() == false`.
- `TestRunnerNew_UnknownBackend_FallsBackToLocal` — `cfg.SandboxBackend="k8s"`; assert warn logged, `r.HostMode() == false` (defense in depth; CLI catches this first, but the runner shouldn't panic if a caller bypasses the CLI).

## Boundaries

- Do **not** add or keep a `WALLFACER_SANDBOX_BACKEND` env var. Backend selection is CLI-only.
- Do **not** add the `MAX_PARALLEL=1` default here — that is `host-parallel-cap.md`.
- Do **not** modify `buildContainerSpecForSandbox` in this task — `container-spec-host-mode.md` does that, using the `HostMode()` accessor this task provides.
- Do **not** rename the internal `"local"` string literal. The CLI flag is the user-facing name (`container`); the internal representation is unchanged so future K8s/native backends can add their own values.
- Do **not** change the `Runner` exported type or break existing callers; only add a field and a method.
