# Task 2: Build Worker Create Args from ContainerSpec

**Status:** Done
**Depends on:** Task 1
**Phase:** 1 (Per-Task Worker Foundation)
**Effort:** Small

## Goal

Add a method to `ContainerSpec` that produces the `podman create` argument
list for a long-lived worker container (sleep entrypoint, no `--rm`).

## What to do

1. In `internal/sandbox/spec.go`, add:

   ```go
   // BuildCreate returns the argument slice for `podman create` with a sleep
   // entrypoint instead of the agent command. The container stays alive and
   // subsequent invocations use `podman exec`.
   func (s ContainerSpec) BuildCreate() []string
   ```

   This is similar to `Build()` but:
   - Uses `create` instead of `run`
   - Omits `--rm`
   - Replaces `Cmd` with `--entrypoint '["sleep","infinity"]'`
   - Keeps all volume mounts, labels, env, network, resource limits

2. Add a helper to build the `podman exec` argument list from a command:

   ```go
   // BuildExec returns the argument slice for `podman exec <name> <cmd...>`.
   func BuildExec(containerName string, cmd []string) []string
   ```

## Tests

- `TestBuildCreate` — verify output includes `create`, volume mounts,
  labels, env, sleep entrypoint; does NOT include `--rm` or the agent cmd.
- `TestBuildExec` — verify output is `["exec", "<name>", "cmd", "args"]`.
- `TestBuildCreatePreservesAllMounts` — spec with multiple volumes, verify
  all appear in create args.

## Boundaries

- Do NOT change `Build()` (the existing ephemeral method).
- Do NOT modify `LocalBackend`.
