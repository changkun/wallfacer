# Task 1: Add Go Dependencies

**Status:** Todo
**Depends on:** None
**Phase:** Phase 1 — Single Terminal Session
**Effort:** Small

## Goal

Add `github.com/creack/pty` (PTY allocation) and `nhooyr.io/websocket` (stdlib-compatible WebSocket) to `go.mod`. These are prerequisites for the backend terminal handler.

## What to do

1. Run `go get github.com/creack/pty@latest` to add the PTY library.
2. Run `go get nhooyr.io/websocket@latest` to add the WebSocket library.
3. Run `go mod tidy` to clean up.
4. Verify `go build ./...` still succeeds (no import conflicts).

## Tests

- `go mod tidy` exits cleanly
- `go build ./...` succeeds
- `go.mod` lists both new `require` entries
- `go.sum` has corresponding checksums

## Boundaries

- Do NOT write any Go code that imports these packages yet
- Do NOT modify any existing source files
