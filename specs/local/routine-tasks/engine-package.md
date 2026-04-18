---
title: Routine scheduler engine package
status: validated
depends_on: []
affects:
  - internal/routine/
effort: medium
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# Routine scheduler engine package

## Goal

Create a self-contained `internal/routine/` package providing the `Engine`
that multiplexes per-routine `time.AfterFunc` timers. This is pure plumbing
with no dependency on `internal/store` or `internal/handler` — it accepts a
`FireFunc` callback so callers stay in control of how instance tasks are
spawned. Unit-tested in isolation with an injectable clock.

## What to do

1. Create `internal/routine/engine.go` with:

   ```go
   package routine

   // Clock abstracts time.AfterFunc for tests.
   type Clock interface {
       Now() time.Time
       AfterFunc(d time.Duration, f func()) Timer
   }

   type Timer interface {
       Stop() bool
   }

   // Schedule yields the next wall-clock fire time relative to now. Zero
   // time means "never" (used for disabled routines).
   type Schedule interface {
       Next(now time.Time) time.Time
   }

   type FixedInterval struct{ D time.Duration }
   func (f FixedInterval) Next(now time.Time) time.Time {
       if f.D <= 0 { return time.Time{} }
       return now.Add(f.D)
   }

   type FireFunc func(ctx context.Context, routineID uuid.UUID)

   type entry struct {
       schedule Schedule
       nextRun  time.Time
       timer    Timer
   }

   type Engine struct {
       clock   Clock
       fire    FireFunc
       ctx     context.Context // passed to FireFunc
       mu      sync.Mutex
       entries map[uuid.UUID]*entry
   }

   func NewEngine(ctx context.Context, clock Clock, fire FireFunc) *Engine

   // Register installs or updates a routine. Disabled entries are registered
   // with a Schedule whose Next returns the zero time; Register then stops
   // any pending timer and records nextRun as zero.
   func (e *Engine) Register(id uuid.UUID, s Schedule)

   // Unregister stops any pending timer and drops the entry.
   func (e *Engine) Unregister(id uuid.UUID)

   // Trigger fires the routine immediately, independent of the schedule,
   // and re-arms the next scheduled cycle.
   func (e *Engine) Trigger(id uuid.UUID)

   // NextRuns returns a snapshot of each registered routine's next-run
   // timestamp (zero time for disabled).
   func (e *Engine) NextRuns() map[uuid.UUID]time.Time
   ```

2. Provide a default `realClock` implementation that wraps `time.Now` and
   `time.AfterFunc`. `NewEngine` defaults to it when the passed clock is nil.

3. When a timer fires: invoke `FireFunc(ctx, id)` in a new goroutine, then
   re-arm by calling `Register(id, <current schedule>)`. Guard against the
   entry having been unregistered between fire and re-arm.

4. Thread-safety: all mutations of `entries` go through `mu`. The `FireFunc`
   callback runs without the mutex held.

5. Create `internal/routine/engine_test.go` using a fake clock:

   ```go
   type fakeClock struct {
       mu      sync.Mutex
       now     time.Time
       pending []*fakeTimer
   }
   type fakeTimer struct {
       due     time.Time
       fire    func()
       stopped bool
   }
   func (c *fakeClock) Now() time.Time { ... }
   func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer { ... }
   func (c *fakeClock) Advance(d time.Duration) // fires all due timers
   ```

## Tests

- `TestFixedInterval_Next` — positive interval yields `now+D`, non-positive
  yields zero time.
- `TestEngine_Register_FiresAfterInterval` — register a routine with a
  1-minute interval; advance clock 1 minute; assert `FireFunc` called with
  the correct ID; `NextRuns` shows a new future time.
- `TestEngine_Register_Updates` — re-register same ID with new interval;
  assert old timer stopped, new timer armed, fire only once per cycle.
- `TestEngine_Unregister` — register then unregister; advance clock past
  interval; assert `FireFunc` not called; `NextRuns` empty.
- `TestEngine_DisabledSchedule` — schedule whose `Next` returns zero time;
  assert no timer armed and `NextRuns` entry is zero.
- `TestEngine_Trigger_ImmediateAndRearms` — Trigger fires once immediately,
  then re-arms the scheduled cycle.
- `TestEngine_FireRearms` — after a natural fire, the next cycle is armed
  automatically.
- `TestEngine_Concurrent` — run with `-race`: goroutines calling
  Register/Unregister/Trigger concurrently do not race.
- `TestEngine_FireAfterUnregister` — simulate a timer firing after
  Unregister was called; FireFunc must not run.

## Boundaries

- No dependency on `internal/store`, `internal/handler`, or `uuid.UUID` type
  beyond using it as an opaque key. The package is pure scheduling plumbing.
- No HTTP, no persistence, no task creation logic.
- Do not add cron-expression schedules in this task — `Schedule` is the
  extension point and `FixedInterval` is the only implementation for v1.
- Do not import this package anywhere else yet; it is verified in isolation
  first.
