---
title: wallfacer doctor reports host-mode readiness
status: validated
depends_on:
  - specs/shared/host-exec-mode/envconfig-host-option.md
affects:
  - internal/cli/doctor.go
  - internal/cli/doctor_test.go
effort: small
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# wallfacer doctor reports host-mode readiness

## Goal

`wallfacer doctor` should reflect the selected sandbox backend. In host mode, container-runtime and image-presence checks are irrelevant; instead, doctor should probe `claude --version` / `codex --version` and report the resolved binary paths.

## What to do

1. In `internal/cli/doctor.go` (`RunDoctor`):
   - After parsing the env file, read `vals["WALLFACER_SANDBOX_BACKEND"]` into a local `backend` string (default `"local"`).
   - Replace the current unconditional "Container command:" and "Sandbox image:" lines with a branch:
     - `local` (or default): keep existing output unchanged.
     - `host`: print
       - `Sandbox backend:   host`
       - `Claude binary:     <path or "NOT FOUND">` — resolve via `vals["WALLFACER_HOST_CLAUDE_BINARY"]` or `exec.LookPath("claude")`.
       - `Codex binary:      <path or "NOT FOUND">` — same for codex.
   - In the readiness-check section (where the current code checks the sandbox image exists), add a parallel `host`-mode branch:
     - For each binary present, spawn `<binary> --version` with a 2 s timeout via `cmdexec`.
     - Report `[ok] Claude CLI: <trimmed stdout>` or `[!] Claude CLI: <error>` and increment `issues` on failure.
     - Repeat for codex.
   - Keep the credentials, env file, and git checks unchanged — they apply in both modes.
   - Add a final note in the summary block: when backend=host, suggest `npm i -g @anthropic-ai/claude-code` / `npm i -g @openai/codex` installation commands if either binary is missing.

2. Extract a small helper `runCLIVersion(binary string) (string, error)` (or reuse `cmdexec.New(...).WithTimeout(...).Output()`) so tests can substitute a fake binary on `$PATH`.

3. Update `defaultSandboxImage()` callsites near the top of `RunDoctor` — skip printing "Sandbox image" entirely in host mode.

## Tests

In `internal/cli/doctor_test.go` (create if missing; follow the pattern in other `internal/cli/*_test.go` files):

- `TestRunDoctor_HostMode_BinariesPresent` — fake claude/codex scripts in `t.TempDir()`; env file sets backend=host and binary overrides pointing at them; capture stdout; assert it contains `Sandbox backend:   host`, both binary paths, both `[ok] CLI:` lines, and does NOT contain `Sandbox image:`.
- `TestRunDoctor_HostMode_ClaudeMissing` — env overrides point at nonexistent claude; assert output contains `[!]` for claude and `issues > 0`.
- `TestRunDoctor_LocalMode_Unchanged` — regression guard: no backend set in env; capture stdout; assert it still contains `Container command:` and `Sandbox image:` lines.

Use `os.Pipe` or `io.Pipe` with a buffered stdout redirect (`os.Stdout = w; defer restore`) for capture — follow any existing pattern in the repo.

## Boundaries

- Do **not** make doctor launch the agents for a real turn — `--version` is sufficient.
- Do **not** add a timeout longer than 2 s per probe; doctor is interactive and should stay fast.
- Do **not** change the structure of the doctor output for local mode — only add the host branch.
- Do **not** attempt to install missing binaries; only suggest the install commands.
