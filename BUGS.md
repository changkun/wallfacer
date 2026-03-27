# Suspected Bugs

Discovered during code comment review on 2026-03-27. These have NOT been fixed.

## internal/pkg/logpipe

- **logpipe.go:160** — `Pipe.Close()` unconditionally calls `p.pr.Close()`, but `StartReader` (line 128) sets `pr` to `nil`. If a caller invokes `Close()` on a pipe created via `StartReader`, this will panic with a nil pointer dereference. The fix would be to add a `if p.pr != nil` guard before the close call.
