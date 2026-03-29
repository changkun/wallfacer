# Suspected Bugs

Discovered during code comment review on 2026-03-27. These have NOT been fixed.

## internal/pkg/logpipe

- [x] **logpipe.go:160** — `Pipe.Close()` unconditionally calls `p.pr.Close()`, but `StartReader` (line 128) sets `pr` to `nil`. If a caller invokes `Close()` on a pipe created via `StartReader`, this will panic with a nil pointer dereference. Fixed by adding a nil guard. **Root cause:** Two constructors (`Start` and `StartReader`) produce the same `Pipe` type but with different invariants — `Start` always sets `pr`, `StartReader` leaves it nil. `Close()` assumed the `Start` invariant. **Lesson:** When a type has multiple constructors that leave fields in different states, methods must handle all valid states or the constructors should guarantee a uniform post-condition.

## internal/pkg/circuitbreaker

- [x] **backoff.go:61** — Integer overflow in exponential backoff for extreme failure counts. When `b.failures` reaches 64+ (on 64-bit platforms), `1 << uint(b.failures-1)` overflows to 0, making `backoff = baseDelay * 0 = 0`. Since `0 > maxDelay` is false, the max-delay cap is bypassed, and `openUntil` is set to `now.Add(0)` = now. This means `IsOpen()` returns false immediately, effectively disabling the breaker after 64+ consecutive failures. Fixed by adding `backoff <= 0` to the cap condition. **Root cause:** The bit-shift `1 << uint(n)` silently wraps to zero for n >= 64 on 64-bit platforms, and the max-delay cap only checked the upper bound. **Lesson:** When using bit-shift for exponential growth, always guard against overflow producing zero or negative values — not just exceeding the cap.
