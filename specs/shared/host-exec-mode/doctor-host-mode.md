---
title: wallfacer doctor reports host-mode readiness
status: complete
depends_on:
  - specs/shared/host-exec-mode/envconfig-host-option.md
affects:
  - internal/cli/doctor.go
  - internal/cli/doctor_test.go
  - main.go
effort: small
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# wallfacer doctor reports host-mode readiness

## Goal

`wallfacer doctor` should reflect the sandbox backend the user intends to run with. Since backend selection is a CLI flag (not an env var), `doctor` takes its own `--backend` flag that mirrors `wallfacer run`. In host mode, container-runtime and image-presence checks are replaced with `claude --version` / `codex --version` probes.

## What to do

1. Change `RunDoctor`'s signature from `RunDoctor(configDir string)` to `RunDoctor(configDir string, args []string)` — mirroring `RunServer` / `RunExec` which already take `args`. Update the `main.go` switch (cases `"doctor"` and `"env"`) to pass `args` through.

2. Inside `RunDoctor`, parse flags:

   ```go
   fs := flag.NewFlagSet("doctor", flag.ExitOnError)
   backendFlag := fs.String("backend", "container",
       `sandbox backend to check: "container" (default) or "host"`)
   _ = fs.Parse(args)

   backend := strings.ToLower(strings.TrimSpace(*backendFlag))
   switch backend {
   case "", "container", "local":
       backend = "local"
   case "host":
       // pass through
   default:
       fmt.Fprintf(os.Stderr, "wallfacer doctor: unknown --backend %q (want \"container\" or \"host\")\n", *backendFlag)
       os.Exit(2)
   }
   ```

3. In the rest of `RunDoctor`:
   - Use the parsed `backend` string.
   - Replace the current unconditional "Container command:" and "Sandbox image:" lines with a branch on the parsed `backend`:
     - `local`: keep existing output unchanged.
     - `host`: print
       - `Sandbox backend:   host`
       - `Claude binary:     <path or "NOT FOUND">` — resolve via `vals["WALLFACER_HOST_CLAUDE_BINARY"]` (from the parsed env file) or `exec.LookPath("claude")`.
       - `Codex binary:      <path or "NOT FOUND">` — same for codex.
   - In the readiness-check section (where the current code checks the sandbox image exists), add a parallel `host`-mode branch:
     - For each binary present, spawn `<binary> --version` with a 2 s timeout via `cmdexec`.
     - Report `[ok] Claude CLI: <trimmed stdout>` or `[!] Claude CLI: <error>` and increment `issues` on failure.
     - Repeat for codex.
   - Keep the credentials, env file, and git checks unchanged — they apply in both modes.
   - Add a final note in the summary block: when backend=host and a binary is missing, suggest `npm i -g @anthropic-ai/claude-code` / `npm i -g @openai/codex`.

3. **Do not** read `WALLFACER_SANDBOX_BACKEND` from env. The flag is the only source of truth.

4. Extract a small helper `runCLIVersion(binary string) (string, error)` (or reuse `cmdexec.New(...).WithTimeout(...).Output()`) so tests can substitute a fake binary on `$PATH`.

5. Update `defaultSandboxImage()` callsites near the top of `RunDoctor` — skip printing "Sandbox image" entirely in host mode.

6. Update `wallfacer doctor` help text / `fs.Usage` to mention the new flag.

## Tests

In `internal/cli/doctor_test.go`:

- `TestRunDoctor_HostMode_BinariesPresent` — fake claude/codex scripts in `t.TempDir()`; env file sets the binary-path overrides pointing at them; invoke `RunDoctor(cfgDir, "host")`; capture stdout; assert it contains `Sandbox backend:   host`, both binary paths, both `[ok] CLI:` lines, and does NOT contain `Sandbox image:`.
- `TestRunDoctor_HostMode_ClaudeMissing` — overrides point at nonexistent claude; invoke with `"host"`; assert output contains `[!]` for claude and `issues > 0`.
- `TestRunDoctor_ContainerMode_Unchanged` — regression guard: invoke `RunDoctor(cfgDir, "local")`; capture stdout; assert it still contains `Container command:` and `Sandbox image:` lines.
- `TestDoctorDispatch_BackendFlag` — exercise the subcommand dispatcher with `--backend host` and `--backend container` and an invalid value; assert the flag translation produces the expected `backend` argument or a descriptive error.

Use `os.Pipe` or `io.Pipe` with a buffered stdout redirect (`os.Stdout = w; defer restore`) for capture — follow any existing pattern in the repo.

## Boundaries

- Do **not** make doctor launch the agents for a real turn — `--version` is sufficient.
- Do **not** add a timeout longer than 2 s per probe; doctor is interactive and should stay fast.
- Do **not** change the structure of the doctor output for container mode — only add the host branch.
- Do **not** read `WALLFACER_SANDBOX_BACKEND` from env; the flag is the only source of truth.
- Do **not** attempt to install missing binaries; only suggest the install commands.
