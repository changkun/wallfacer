# Suspected Bugs

Discovered during code comment review on 2026-03-27. These have NOT been fixed.

## internal/pkg/logpipe

- [x] **logpipe.go:160** — `Pipe.Close()` unconditionally calls `p.pr.Close()`, but `StartReader` (line 128) sets `pr` to `nil`. If a caller invokes `Close()` on a pipe created via `StartReader`, this will panic with a nil pointer dereference. Fixed by adding a nil guard. **Root cause:** Two constructors (`Start` and `StartReader`) produce the same `Pipe` type but with different invariants — `Start` always sets `pr`, `StartReader` leaves it nil. `Close()` assumed the `Start` invariant. **Lesson:** When a type has multiple constructors that leave fields in different states, methods must handle all valid states or the constructors should guarantee a uniform post-condition.
