---
title: HostBackend implementation
status: validated
depends_on: []
affects:
  - internal/sandbox/host.go
  - internal/sandbox/host_test.go
  - internal/sandbox/testdata/fakeagent/
effort: medium
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# HostBackend implementation

## Goal

Add a `sandbox.Backend` implementation that runs `claude` / `codex` directly on the host via `os/exec`, so wallfacer can operate without a container runtime. All container-specific fields on `ContainerSpec` are reinterpreted by the backend — workspace mounts become CWD, env-file becomes `cmd.Env`, labels become an internal PID-map entry.

## What to do

1. Create `internal/sandbox/host.go` with:
   - `type HostBackend struct` holding:
     - `agentBinaries map[sandbox.Type]string` (resolved paths for claude / codex)
     - `supportsAppendSystemPrompt map[sandbox.Type]bool` (cached probe result)
     - `procs map[string]*hostHandle` keyed by container name, guarded by `sync.Mutex`
   - `type HostBackendConfig struct { ClaudeBinary, CodexBinary string }` — empty means `exec.LookPath`.
   - `NewHostBackend(cfg HostBackendConfig) (*HostBackend, error)`:
     - Resolve binaries (fail with actionable error if missing: `"claude binary not found; install with 'npm i -g @anthropic-ai/claude-code' or set WALLFACER_HOST_CLAUDE_BINARY"`).
     - For each resolved binary, run `<bin> --help` (short timeout, 2 s) and grep stdout+stderr for `--append-system-prompt`; cache result in `supportsAppendSystemPrompt`.
   - `Launch(ctx, spec)`:
     1. Pick binary from `spec.Env["WALLFACER_AGENT"]` (`claude` or `codex`; error on missing/unknown).
     2. Build `cmd.Env`: start from `os.Environ()`, overlay keys from `spec.EnvFile` parsed via `envconfig.Parse` (reuse existing parser; if the file is absent, skip), overlay `spec.Env` last.
     3. If `spec.Env["WALLFACER_INSTRUCTIONS_PATH"]` is set:
        - If `supportsAppendSystemPrompt[agent]` is true, append `--append-system-prompt <path>` to the argv (use the file path directly; Claude CLI reads it).
        - Else, read the file and prepend its content (delimited by `\n\n---\n\n`) to the value of the `-p` flag in `spec.Cmd`, producing a new argv slice. If no `-p` flag, log a warning and pass through unchanged.
     4. Resolve CWD: use `spec.WorkDir` directly when it is an absolute host path. If it still looks like a container path (`/workspace/...`), return an error — the runner is supposed to translate these.
     5. Build `cmd := exec.CommandContext(ctx, binary, argv...)`; set `Dir=cwd`, `Env=env`; wire stdout/stderr pipes; `cmd.Start()`; register in `procs`.
   - `type hostHandle struct` with `name, cmd, stdout, stderr, state atomic.Int32, backend *HostBackend` fields. Satisfy `Handle`:
     - `State()` / `Stdout()` / `Stderr()` / `Name()` — straightforward passthroughs.
     - `Wait()` — uses the existing `transition` helper from `backend.go:58` for state transitions; returns child exit code; on exit, removes self from `backend.procs`.
     - `Kill()` — `transition(StateStopping)`; send SIGTERM via `cmd.Process.Signal(syscall.SIGTERM)`; wait up to 5 s via a timer goroutine for `cmd.Wait()`; if not exited, SIGKILL; `transition(StateStopped)`; remove from `procs`.
   - `List(ctx)` — lock `procs`, snapshot entries, return `[]ContainerInfo` with `ID = shortName(name)`, `Image = "host"`, `State = "running"`, `Status = fmt.Sprintf("Host PID %d", pid)`, `TaskID` from `wallfacer.task.id` label if the handle kept it.
   - Compile-time interface assertions: `var _ Backend = (*HostBackend)(nil); var _ Handle = (*hostHandle)(nil)`. Do **not** implement `WorkerManager` — host launches are per-turn and stateless.

2. Create `internal/sandbox/testdata/fakeagent/main.go`:
   - Tiny program that:
     - Parses `-p <prompt>` and `--append-system-prompt <path>` flags.
     - Reads `WALLFACER_AGENT` env to echo back.
     - Emits two NDJSON lines to stdout: an init event and a final result event with `stop_reason: "end_turn"`, `is_error: false`.
     - If `FAKEAGENT_SLEEP` env is set, sleeps for that many seconds (used for Kill tests).
     - If invoked with `--help`, prints a short help string — one variant includes `--append-system-prompt` (feature-probe positive), a second variant (via `FAKEAGENT_NO_APPEND=1`) omits it (probe negative).
   - Include a `go:build ignore` tag so `go test` does not try to compile it as part of the package; tests build it explicitly into a temp dir.

3. Create `internal/sandbox/host_test.go`:
   - Helper `buildFakeAgent(t)` that `go build`s the fakeagent into `t.TempDir()` and returns the path.
   - `TestNewHostBackend_MissingBinary` — pass a non-existent path; assert error message includes "not found" and install hint.
   - `TestNewHostBackend_ProbesAppendSupport` — parameterized over feature-on and feature-off fakeagent variants (via env var); assert `SupportsAppendSystemPrompt(sandbox.Claude)` matches.
   - `TestHostBackend_Launch_Argv` — spec with `Cmd = ["-p", "hello", "--model", "foo"]`; assert handle starts, NDJSON is readable from stdout, exit code is 0.
   - `TestHostBackend_Launch_ResumeFlag` — spec with `--resume <session>`; assert the fake echoes the flag through.
   - `TestHostBackend_Launch_EnvMerge` — `spec.EnvFile` sets A=1,B=2; `spec.Env` sets B=3,C=4; assert child sees A=1,B=3,C=4 (spec.Env wins).
   - `TestHostBackend_Launch_WorkDir` — `spec.WorkDir = tempDir`; assert fake agent's stdout shows it was launched from that dir (fake echoes `os.Getwd()`).
   - `TestHostBackend_Launch_RejectsContainerPath` — `spec.WorkDir = "/workspace/foo"`; assert error.
   - `TestHostBackend_AppendSystemPrompt_Supported` — probe-positive fakeagent; spec with `WALLFACER_INSTRUCTIONS_PATH=<tempfile>`; assert argv contains `--append-system-prompt <path>`.
   - `TestHostBackend_AppendSystemPrompt_Fallback` — probe-negative fakeagent; assert the `-p` flag value gets the instructions prepended with the `---` delimiter.
   - `TestHostBackend_Kill_SIGTERMThenSIGKILL` — `FAKEAGENT_SLEEP=10`; call `Kill()`; assert `Wait()` returns within 6 s; assert state goes to `StateStopped`.
   - `TestHostBackend_List` — launch two handles; assert `List(ctx)` returns two entries with correct Name and TaskID label promotion.
   - `TestHostBackend_Handle_RemovedOnWait` — launch, wait for exit; assert `procs` map no longer contains the name.

## Tests

Listed above. All test names begin with `Test`. Use `t.TempDir()` for scratch, never a hardcoded path.

## Boundaries

- Do **not** modify `internal/runner/` in this task — backend selection wiring lives in a separate task (`runner-host-switch.md`).
- Do **not** modify `ContainerSpec` struct — no new fields yet. If path translation needs metadata later, a follow-up task introduces `ContainerSpec.PathMap`.
- Do **not** implement `WorkerManager` — host mode is stateless per turn.
- Do **not** touch `envconfig` — binary-override env vars are read by the runner when constructing `HostBackendConfig`, not by this file.
- Do **not** gate on `WALLFACER_SANDBOX_BACKEND` inside `host.go` — selection is the runner's job.
