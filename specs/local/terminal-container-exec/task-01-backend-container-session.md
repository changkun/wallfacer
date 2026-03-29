# Task 1: Backend Container Session Creation

**Status:** Done
**Depends on:** None
**Phase:** Backend
**Effort:** Medium

## Goal

Extend the terminal session registry to support spawning `podman exec -it <container> bash` sessions alongside host shell sessions. The `create_session` WebSocket message gains an optional `container` field.

## What to do

1. In `internal/handler/terminal.go`, add a `Container` field to `terminalMessage`:
   ```go
   Container string `json:"container,omitempty"`
   ```

2. Add a `container` field to `terminalSession` to track whether a session is a container exec:
   ```go
   container string // container name; empty for host shell sessions
   ```

3. Add a `createContainerExec(containerName string, cols, rows int) (string, error)` method on `sessionRegistry`. This method:
   - Builds the exec command: `exec.CommandContext(ctx, runtime, "exec", "-it", containerName, "bash")` where `runtime` is the container runtime path (podman/docker).
   - Starts the command with PTY via `pty.StartWithSize()`.
   - Creates the session with `outputCh`, reader goroutine, etc. — same pattern as `create()`.
   - Sets `sess.container = containerName`.

4. The registry needs access to the container runtime path. Add a `runtime string` field to `sessionRegistry`, populated from the runner's container runtime command. The handler already has `h.runner` — add a `ContainerRuntime() string` method to the runner interface, or pass the runtime path when constructing the registry.

5. Update the `create_session` case in `HandleTerminalWS` to check `msg.Container`:
   ```go
   case "create_session":
       var newID string
       var err error
       if msg.Container != "" {
           newID, err = registry.createContainerExec(msg.Container, cols, rows)
       } else {
           newID, err = registry.create(shell, cwd, cols, rows)
       }
       // ... rest unchanged
   ```

6. Include the `container` field in `sessionInfo` so the frontend knows which sessions are container sessions:
   ```go
   type sessionInfo struct {
       ID        string `json:"id"`
       Active    bool   `json:"active"`
       Container string `json:"container,omitempty"`
   }
   ```

7. Update `sendSessionsList` to populate the `Container` field from each session.

## Tests

- `TestTerminalWS_CreateContainerSession` — send `create_session` with a container name, verify `session_created` response. (Requires a running container — skip in CI if none available, or mock.)
- `TestTerminalWS_CreateSessionBackwardCompat` — send `create_session` without container field, verify host shell still works (existing test covers this).
- `TestTerminalWS_SessionsListIncludesContainer` — create a container session, verify `sessions` list includes the `container` field.
- `TestTerminalMessage_ContainerField` — verify JSON marshaling of the new field.

## Boundaries

- Do NOT add the container selector UI (Task 2).
- Do NOT add `Exec` method to `SandboxBackend` interface (cloud deployment is future work).
- Do NOT change the frontend.
- The container runtime path can be obtained from the runner or detected directly — keep it simple.
