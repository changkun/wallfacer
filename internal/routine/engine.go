package routine

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Clock abstracts time.Now and time.AfterFunc so tests can drive the engine
// deterministically without real sleeps.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is the minimal subset of *time.Timer the engine uses. Stop is
// expected to return true if the timer was stopped before firing.
type Timer interface {
	Stop() bool
}

// Schedule produces the next wall-clock fire time relative to now. A zero
// time means the routine is disabled (never fires).
type Schedule interface {
	Next(now time.Time) time.Time
}

// FixedInterval fires every D after the previous arm. A non-positive D is
// treated as disabled so callers can pause a routine without switching
// schedule types.
type FixedInterval struct{ D time.Duration }

// Next implements [Schedule].
func (f FixedInterval) Next(now time.Time) time.Time {
	if f.D <= 0 {
		return time.Time{}
	}
	return now.Add(f.D)
}

// disabled is a sentinel [Schedule] that never fires. Used internally so
// [Engine.Register] always stores a non-nil Schedule.
type disabled struct{}

// Next implements [Schedule].
func (disabled) Next(time.Time) time.Time { return time.Time{} }

// Disabled returns a schedule that never fires. Equivalent to registering
// a FixedInterval{D: 0} and intended to make "pause this routine" explicit
// at call sites.
func Disabled() Schedule { return disabled{} }

// FireFunc is invoked when a registered routine's timer elapses or when
// [Engine.Trigger] is called. It runs in its own goroutine with the engine
// mutex released, so it may perform arbitrary I/O.
type FireFunc func(ctx context.Context, routineID uuid.UUID)

// entry is the engine's per-routine bookkeeping: the currently-armed timer
// (nil when disabled), the schedule that armed it, and the cached next-run
// time the timer is waiting on.
type entry struct {
	schedule Schedule
	nextRun  time.Time
	timer    Timer
}

// Engine multiplexes one timer per registered routine ID. Timers are armed
// lazily via [Engine.Register] and re-armed automatically after each fire.
type Engine struct {
	clock Clock
	fire  FireFunc
	ctx   context.Context

	mu      sync.Mutex
	entries map[uuid.UUID]*entry
}

// NewEngine constructs an engine. A nil clock defaults to [SystemClock].
// The supplied ctx is passed to every [FireFunc] invocation; cancelling it
// does not by itself stop pending timers — callers should [Engine.Unregister]
// routines on shutdown if cleanup matters.
func NewEngine(ctx context.Context, clock Clock, fire FireFunc) *Engine {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Engine{
		clock:   clock,
		fire:    fire,
		ctx:     ctx,
		entries: make(map[uuid.UUID]*entry),
	}
}

// Register installs or updates a routine. If the id is already registered
// with a schedule equal (by reflect.DeepEqual) to s, Register is a no-op —
// existing timers are preserved. Otherwise any previously-armed timer for
// the same id is stopped and a fresh timer is armed based on s's next
// fire time. Registering a disabled schedule (one whose Next returns the
// zero time) keeps the entry but leaves it un-armed — the id still
// appears in [Engine.NextRuns] with a zero time.
//
// Idempotence matters: callers like the handler's routine reconciler run
// on every store change, so Register is invoked repeatedly with the same
// schedule for every routine. Without the equality check, each call would
// stop and re-arm the timer, resetting NextRun and creating a cascade as
// the NextRun write fans out through the watcher.
func (e *Engine) Register(id uuid.UUID, s Schedule) {
	if s == nil {
		s = disabled{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if ent, ok := e.entries[id]; ok && reflect.DeepEqual(ent.schedule, s) {
		return
	}
	e.armLocked(id, s)
}

// Unregister stops any pending timer for the routine and drops it from
// the registry. Calling Unregister on an unknown id is a no-op.
func (e *Engine) Unregister(id uuid.UUID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ent, ok := e.entries[id]; ok {
		if ent.timer != nil {
			ent.timer.Stop()
		}
		delete(e.entries, id)
	}
}

// Trigger fires the routine immediately (on a fresh goroutine) without
// waiting for the scheduled time, then re-arms the next scheduled cycle.
// If the routine is unknown, Trigger is a no-op.
func (e *Engine) Trigger(id uuid.UUID) {
	e.mu.Lock()
	ent, ok := e.entries[id]
	if !ok {
		e.mu.Unlock()
		return
	}
	// Stop the pending scheduled timer — Trigger supersedes this cycle.
	if ent.timer != nil {
		ent.timer.Stop()
		ent.timer = nil
		ent.nextRun = time.Time{}
	}
	e.mu.Unlock()

	if e.fire != nil {
		go e.fire(e.ctx, id)
	}

	e.reArmAfterFire(id)
}

// NextRuns returns a snapshot of each registered routine's next-run
// timestamp. Disabled routines appear with a zero [time.Time].
func (e *Engine) NextRuns() map[uuid.UUID]time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[uuid.UUID]time.Time, len(e.entries))
	for id, ent := range e.entries {
		out[id] = ent.nextRun
	}
	return out
}

// armLocked replaces any existing timer for id with a fresh one based on
// the current schedule. Must be called with e.mu held.
func (e *Engine) armLocked(id uuid.UUID, s Schedule) {
	ent, ok := e.entries[id]
	if !ok {
		ent = &entry{}
		e.entries[id] = ent
	}
	if ent.timer != nil {
		ent.timer.Stop()
		ent.timer = nil
	}
	ent.schedule = s
	ent.nextRun = time.Time{}

	now := e.clock.Now()
	next := s.Next(now)
	if next.IsZero() {
		// Disabled: leave un-armed, keep entry so NextRuns reports it.
		return
	}
	delay := max(next.Sub(now), 0)
	ent.nextRun = next
	ent.timer = e.clock.AfterFunc(delay, func() { e.onFire(id) })
}

// onFire runs when a scheduled timer elapses. It validates that the entry
// is still present (in case Unregister raced), dispatches the fire
// callback on its own goroutine, and re-arms the next cycle.
func (e *Engine) onFire(id uuid.UUID) {
	e.mu.Lock()
	ent, ok := e.entries[id]
	if !ok {
		e.mu.Unlock()
		return
	}
	ent.timer = nil
	ent.nextRun = time.Time{}
	e.mu.Unlock()

	if e.fire != nil {
		go e.fire(e.ctx, id)
	}

	e.reArmAfterFire(id)
}

// reArmAfterFire installs the next scheduled cycle after a fire or manual
// Trigger completes. It re-reads the current schedule under the lock so
// that a Register call issued during the fire (for example, by the
// handler's reconciler writing back state) is not overwritten by the
// stale schedule captured before the fire dispatched. It also skips the
// re-arm when another goroutine already installed a timer in the
// meantime, preventing the double-arm race the earlier implementation
// had when Register and fire interleaved.
func (e *Engine) reArmAfterFire(id uuid.UUID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ent, ok := e.entries[id]
	if !ok {
		return
	}
	if ent.timer != nil {
		// Another caller (typically Register invoked from a reconcile
		// callback triggered by state written during the fire) already
		// armed a fresh timer. Respect their decision.
		return
	}
	e.armLocked(id, ent.schedule)
}
