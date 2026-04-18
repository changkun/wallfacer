package routine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeClock is a deterministic Clock that only fires timers when
// Advance is called. Not safe against its own Advance running in a
// goroutine, but race-safe for concurrent Register/Unregister callers.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	clock   *fakeClock
	due     time.Time
	fire    func()
	stopped bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, due: c.now.Add(d), fire: f}
	c.timers = append(c.timers, t)
	return t
}

// Advance moves the fake clock forward and fires any timer whose due
// time is now <= the new now. Each fire runs synchronously inside
// Advance. Timers armed during a fire callback are not fired in the same
// Advance call (the fire callback itself must re-Advance if needed).
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	due := c.now
	var toFire []*fakeTimer
	remaining := c.timers[:0]
	for _, t := range c.timers {
		if t.stopped {
			continue
		}
		if !t.due.After(due) {
			t.stopped = true
			toFire = append(toFire, t)
			continue
		}
		remaining = append(remaining, t)
	}
	c.timers = remaining
	c.mu.Unlock()

	for _, t := range toFire {
		t.fire()
	}
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

func TestFixedInterval_Next(t *testing.T) {
	now := time.Unix(100, 0)
	if got := (FixedInterval{D: 5 * time.Second}).Next(now); !got.Equal(now.Add(5 * time.Second)) {
		t.Fatalf("positive interval: got %v want %v", got, now.Add(5*time.Second))
	}
	if got := (FixedInterval{D: 0}).Next(now); !got.IsZero() {
		t.Fatalf("zero interval should disable, got %v", got)
	}
	if got := (FixedInterval{D: -time.Second}).Next(now); !got.IsZero() {
		t.Fatalf("negative interval should disable, got %v", got)
	}
}

// collectFires returns a channel that receives every routine id the
// engine's FireFunc sees. The engine's fire runs in its own goroutine;
// tests that need deterministic ordering should Advance step by step.
func collectFires(t *testing.T) (FireFunc, <-chan uuid.UUID) {
	t.Helper()
	ch := make(chan uuid.UUID, 16)
	return func(_ context.Context, id uuid.UUID) {
		ch <- id
	}, ch
}

func waitFire(t *testing.T, ch <-chan uuid.UUID) uuid.UUID {
	t.Helper()
	select {
	case id := <-ch:
		return id
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for fire")
		return uuid.Nil
	}
}

func assertNoFire(t *testing.T, ch <-chan uuid.UUID, within time.Duration) {
	t.Helper()
	select {
	case id := <-ch:
		t.Fatalf("unexpected fire for %s", id)
	case <-time.After(within):
	}
}

func TestEngine_RegisterFiresAfterInterval(t *testing.T) {
	clock := newFakeClock()
	fire, fires := collectFires(t)
	eng := NewEngine(context.Background(), clock, fire)

	id := uuid.New()
	eng.Register(id, FixedInterval{D: 30 * time.Second})

	// Before the interval elapses no fire should happen.
	clock.Advance(29 * time.Second)
	assertNoFire(t, fires, 50*time.Millisecond)

	clock.Advance(2 * time.Second)
	if got := waitFire(t, fires); got != id {
		t.Fatalf("fire id mismatch: got %s want %s", got, id)
	}

	// After firing, a fresh cycle should be armed.
	next := eng.NextRuns()[id]
	if next.IsZero() {
		t.Fatalf("expected next run re-armed after fire")
	}
}

func TestEngine_ReRegisterStopsOldTimer(t *testing.T) {
	clock := newFakeClock()
	fire, fires := collectFires(t)
	eng := NewEngine(context.Background(), clock, fire)

	id := uuid.New()
	eng.Register(id, FixedInterval{D: 10 * time.Second})
	// Replace with a longer interval before the original fires.
	eng.Register(id, FixedInterval{D: 60 * time.Second})

	clock.Advance(15 * time.Second)
	assertNoFire(t, fires, 50*time.Millisecond)

	clock.Advance(46 * time.Second)
	if got := waitFire(t, fires); got != id {
		t.Fatalf("fire id mismatch")
	}

	// Only one fire should occur per cycle despite re-registration.
	assertNoFire(t, fires, 50*time.Millisecond)
}

func TestEngine_Unregister(t *testing.T) {
	clock := newFakeClock()
	fire, fires := collectFires(t)
	eng := NewEngine(context.Background(), clock, fire)

	id := uuid.New()
	eng.Register(id, FixedInterval{D: 10 * time.Second})
	eng.Unregister(id)

	clock.Advance(60 * time.Second)
	assertNoFire(t, fires, 50*time.Millisecond)

	if _, present := eng.NextRuns()[id]; present {
		t.Fatalf("expected routine removed from NextRuns")
	}
}

func TestEngine_DisabledSchedule(t *testing.T) {
	clock := newFakeClock()
	fire, fires := collectFires(t)
	eng := NewEngine(context.Background(), clock, fire)

	id := uuid.New()
	eng.Register(id, Disabled())

	next, present := eng.NextRuns()[id]
	if !present {
		t.Fatalf("disabled routine should still appear in NextRuns")
	}
	if !next.IsZero() {
		t.Fatalf("disabled routine should have zero next run, got %v", next)
	}

	clock.Advance(time.Hour)
	assertNoFire(t, fires, 50*time.Millisecond)
}

func TestEngine_DisabledViaFixedIntervalZero(t *testing.T) {
	clock := newFakeClock()
	fire, fires := collectFires(t)
	eng := NewEngine(context.Background(), clock, fire)

	id := uuid.New()
	eng.Register(id, FixedInterval{D: 0})

	clock.Advance(time.Hour)
	assertNoFire(t, fires, 50*time.Millisecond)
}

func TestEngine_TriggerFiresImmediately(t *testing.T) {
	clock := newFakeClock()
	fire, fires := collectFires(t)
	eng := NewEngine(context.Background(), clock, fire)

	id := uuid.New()
	eng.Register(id, FixedInterval{D: 60 * time.Second})
	eng.Trigger(id)

	if got := waitFire(t, fires); got != id {
		t.Fatalf("fire id mismatch")
	}

	// Scheduled cycle should be re-armed from "now", not from the
	// original register time.
	next := eng.NextRuns()[id]
	if next.IsZero() {
		t.Fatalf("expected next run re-armed after trigger")
	}
}

func TestEngine_TriggerUnknownIsNoOp(t *testing.T) {
	clock := newFakeClock()
	fire, fires := collectFires(t)
	eng := NewEngine(context.Background(), clock, fire)

	eng.Trigger(uuid.New())
	assertNoFire(t, fires, 50*time.Millisecond)
}

func TestEngine_FireReArms(t *testing.T) {
	clock := newFakeClock()
	fire, fires := collectFires(t)
	eng := NewEngine(context.Background(), clock, fire)

	id := uuid.New()
	eng.Register(id, FixedInterval{D: 10 * time.Second})

	// Fire three consecutive cycles.
	for i := range 3 {
		clock.Advance(11 * time.Second)
		if got := waitFire(t, fires); got != id {
			t.Fatalf("cycle %d: fire id mismatch", i)
		}
	}
}

func TestEngine_FireAfterUnregisterIsSuppressed(t *testing.T) {
	clock := newFakeClock()
	var fires atomic.Int32
	eng := NewEngine(context.Background(), clock, func(context.Context, uuid.UUID) {
		fires.Add(1)
	})

	id := uuid.New()
	eng.Register(id, FixedInterval{D: 5 * time.Second})
	eng.Unregister(id)

	// Advance past the original due time. Because Unregister called
	// Stop(), Advance should not include the timer in toFire.
	clock.Advance(10 * time.Second)

	time.Sleep(10 * time.Millisecond)
	if got := fires.Load(); got != 0 {
		t.Fatalf("expected 0 fires after unregister, got %d", got)
	}
}

func TestEngine_ConcurrentRegisterUnregister(t *testing.T) {
	clock := newFakeClock()
	eng := NewEngine(context.Background(), clock, func(context.Context, uuid.UUID) {})

	ids := make([]uuid.UUID, 16)
	for i := range ids {
		ids[i] = uuid.New()
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(2)
		go func(id uuid.UUID) {
			defer wg.Done()
			for i := range 100 {
				eng.Register(id, FixedInterval{D: time.Duration(i+1) * time.Second})
			}
		}(id)
		go func(id uuid.UUID) {
			defer wg.Done()
			for range 100 {
				eng.Unregister(id)
			}
		}(id)
	}
	wg.Wait()

	// Final state is race-dependent but must not panic; NextRuns must
	// return a valid map.
	if eng.NextRuns() == nil {
		t.Fatalf("NextRuns returned nil after concurrent mutation")
	}
}

func TestEngine_NilFireFuncDoesNotPanic(t *testing.T) {
	clock := newFakeClock()
	eng := NewEngine(context.Background(), clock, nil)

	id := uuid.New()
	eng.Register(id, FixedInterval{D: 1 * time.Second})
	clock.Advance(2 * time.Second)

	// Give the re-arm goroutine a moment to run.
	time.Sleep(10 * time.Millisecond)

	if eng.NextRuns()[id].IsZero() {
		t.Fatalf("expected re-arm after fire even with nil FireFunc")
	}
}

func TestEngine_NilClockDefaultsToSystem(t *testing.T) {
	eng := NewEngine(context.Background(), nil, nil)
	// The smoke test is that we can Register and NextRuns without a panic.
	id := uuid.New()
	eng.Register(id, FixedInterval{D: time.Hour})
	got := eng.NextRuns()[id]
	if got.IsZero() {
		t.Fatalf("expected non-zero next run with system clock")
	}
}
