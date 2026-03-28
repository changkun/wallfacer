# Task 1: Add WebSocket Dependency and Inline PTY Helper

**Status:** Done
**Depends on:** None
**Phase:** Phase 1 — Single Terminal Session
**Effort:** Small

## Goal

Add `nhooyr.io/websocket` (stdlib-compatible WebSocket) to `go.mod` and create a small internal PTY helper package that wraps the POSIX syscalls directly, avoiding the `creack/pty` dependency.

## What to do

1. Run `go get nhooyr.io/websocket@latest` and `go mod tidy`.

2. Create `internal/pty/pty.go` with build tag `//go:build !windows`:
   ```go
   package pty

   // Open allocates a new PTY pair, returning the master and slave file descriptors.
   func Open() (master, slave *os.File, err error)

   // StartWithSize spawns cmd attached to a new PTY with the given window size.
   func StartWithSize(cmd *exec.Cmd, rows, cols uint16) (ptmx *os.File, err error)

   // Setsize sets the terminal window size on the PTY master.
   func Setsize(ptmx *os.File, rows, cols uint16) error
   ```

   Implementation (~60 LOC):
   - `Open()`: call `posix_openpt` via `syscall.Open("/dev/ptmx", O_RDWR|O_NOCTTY, 0)`, then `grantpt`/`unlockpt` via `syscall.Syscall` (or `unix.Grantpt`/`unix.Unlockpt` if using `golang.org/x/sys/unix`), then `ptsname` to get the slave path, open it.
   - `StartWithSize()`: call `Open()`, set `cmd.Stdin/Stdout/Stderr = slave`, set `cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}`, call `Setsize(master, rows, cols)`, start cmd, close slave in parent.
   - `Setsize()`: `TIOCSWINSZ` ioctl via `syscall.Syscall(syscall.SYS_IOCTL, ...)` with `winsize{Row: rows, Col: cols}`.

   Note: use `golang.org/x/sys/unix` only if the bare `syscall` package doesn't expose what's needed on Darwin. Check whether `go get golang.org/x/sys/unix` is needed — it may already be an indirect dependency. If not, the raw `syscall` package should suffice for `ioctl` and `/dev/ptmx` operations on macOS/Linux.

3. Create `internal/pty/pty_windows.go` with build tag `//go:build windows`:
   ```go
   package pty

   // Stub: terminal not supported on Windows in Phase 1.
   func Open() (*os.File, *os.File, error) { return nil, nil, errors.New("pty: not supported on windows") }
   func StartWithSize(*exec.Cmd, uint16, uint16) (*os.File, error) { return nil, errors.New("pty: not supported on windows") }
   func Setsize(*os.File, uint16, uint16) error { return errors.New("pty: not supported on windows") }
   ```

4. Create `internal/pty/pty_test.go`:
   - **`TestOpen`**: allocate a PTY pair, write to master, read from slave (and vice versa), close both.
   - **`TestStartWithSize`**: spawn `echo hello`, read output from master, assert contains `hello`.
   - **`TestSetsize`**: open PTY, call `Setsize(master, 40, 120)`, verify no error.

5. Verify `go build ./...` and `go test ./...` pass.

## Tests

- `TestOpen` — PTY pair created, bidirectional I/O works
- `TestStartWithSize` — shell process spawns and produces output
- `TestSetsize` — resize succeeds without error
- `go build ./...` succeeds on macOS/Linux
- `go mod tidy` exits cleanly

## Boundaries

- Do NOT implement Windows ConPTY (stub only)
- Do NOT write the WebSocket handler yet (Task 4)
- Keep the package minimal — only the three functions needed by the terminal handler
