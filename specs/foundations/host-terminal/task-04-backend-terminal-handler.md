---
title: "Backend Terminal WebSocket Handler"
status: archived
depends_on:
  - specs/foundations/host-terminal/task-01-go-dependencies.md
  - specs/foundations/host-terminal/task-02-envconfig-terminal-enabled.md
affects:
  - internal/handler/terminal.go
  - internal/cli/server.go
  - internal/handler/middleware.go
effort: large
created: 2026-03-22
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 4: Backend Terminal WebSocket Handler

## Goal

Implement the server-side WebSocket terminal handler that spawns a PTY-backed shell process and relays I/O bidirectionally. Register the WebSocket route and wire up authentication.

## What to do

### Handler: `internal/handler/terminal.go`

1. Define `TerminalSession` struct:
   ```go
   type TerminalSession struct {
       id     string
       ptmx   *os.File
       cmd    *exec.Cmd
       cancel context.CancelFunc
   }
   ```

2. Implement `HandleTerminalWS(w http.ResponseWriter, r *http.Request)`:
   - Check `TerminalEnabled` via `envconfig.Parse(h.envFile)`. Return 403 JSON error if disabled.
   - Parse query params: `cols` (default 80), `rows` (default 24), `cwd` (validate against active workspaces, fall back to first workspace).
   - Accept WebSocket upgrade via `websocket.Accept(w, r, nil)`.
   - Determine shell: `os.Getenv("SHELL")` → `/bin/bash` → `/bin/sh`.
   - Create `exec.CommandContext` with the shell, set `cmd.Dir = cwd`, set `cmd.Env` from `os.Environ()`.
   - Spawn via `pty.StartWithSize(cmd, uint16(rows), uint16(cols))` using `internal/pty`.
   - Launch two goroutines:
     - **PTY→WS**: Read from `ptmx` with 32 KB buffer, write binary WebSocket messages. Exit on read error.
     - **WS→PTY**: Read WebSocket messages, JSON-decode to dispatch by `type` field (`input`, `resize`, `ping`). Write decoded base64 `data` to `ptmx` for `input`. Call `pty.Setsize()` for `resize`. Send `{"type":"pong"}` text message for `ping`.
   - Use `sync.WaitGroup` or `errgroup` to wait for both goroutines.
   - Cleanup on exit: cancel context, send SIGHUP to process group (`syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)`), wait 2s, SIGKILL if still alive, close `ptmx`.

3. Handle shell exit: when `cmd.Wait()` returns, close WebSocket with status 1000 (normal closure).

### Route registration: `internal/cli/server.go`

4. In `BuildMux`, register the terminal endpoint directly (not via `apicontract`), after the API contract route loop:
   ```go
   // WebSocket endpoints bypass REST request/response semantics.
   mux.HandleFunc("GET /api/terminal/ws", h.HandleTerminalWS)
   ```
   Add a comment explaining why it's not in `apicontract/routes.go`.

### Auth middleware: `internal/handler/middleware.go`

5. Add `/api/terminal/ws` to the `isSSEPath` closure so it accepts `?token=` query param auth:
   ```go
   if path == "/api/tasks/stream" || path == "/api/git/stream" || path == "/api/terminal/ws" {
       return true
   }
   ```

### Tests: `internal/handler/terminal_test.go`

6. Write tests using `httptest.NewServer` + `nhooyr.io/websocket` client:
   - **`TestTerminalWS_Disabled`**: Set `TerminalEnabled=false` in env, attempt WebSocket connect, assert 403.
   - **`TestTerminalWS_AuthRequired`**: Enable terminal, configure `WALLFACER_SERVER_API_KEY`, connect without token, assert 401.
   - **`TestTerminalWS_Connect`**: Enable terminal, connect, send `{"type":"input","data":"<base64 of 'echo hello\n'>"}`, read output, assert it contains `hello`. Close cleanly.
   - **`TestTerminalWS_Resize`**: Connect, send `{"type":"resize","cols":120,"rows":40}`, assert no error (resize is fire-and-forget).
   - **`TestTerminalWS_Ping`**: Connect, send `{"type":"ping"}`, read response, assert `{"type":"pong"}`.
   - **`TestTerminalWS_ShellExit`**: Connect, send `{"type":"input","data":"<base64 of 'exit\n'>"}`, assert WebSocket closes with status 1000.
   - **`TestTerminalWS_CwdValidation`**: Connect with `cwd=/nonexistent`, assert it falls back to a valid workspace path (or rejects with error if no workspaces configured).

## Boundaries

- Phase 1 only: one session per connection, no session registry
- Do NOT handle multiple sessions or tabs (Phase 2)
- Do NOT implement container exec (Phase 3)
- Do NOT add Windows-specific ConPTY handling — the `internal/pty` package stubs Windows in Task 1
- Do NOT modify any frontend files
