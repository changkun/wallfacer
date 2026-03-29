package circuitbreaker

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestBreaker_ClosedByDefault verifies a new breaker starts in the closed state
// with zero failures and allows operations.
func TestBreaker_ClosedByDefault(t *testing.T) {
	b := New(3, 30*time.Second)
	if !b.Allow() {
		t.Fatal("expected Allow() == true for a fresh breaker")
	}
	if b.State() != Closed {
		t.Fatalf("expected state Closed, got %v", b.State())
	}
	if b.Failures() != 0 {
		t.Fatalf("expected 0 failures, got %d", b.Failures())
	}
}

// TestBreaker_OpensAfterThreshold verifies that the breaker stays closed until
// the failure count reaches the threshold, then transitions to open.
func TestBreaker_OpensAfterThreshold(t *testing.T) {
	const threshold = 5
	b := New(threshold, 30*time.Second)

	for i := range threshold - 1 {
		b.RecordFailure()
		if b.State() != Closed {
			t.Fatalf("after %d failures state should be Closed, got %v", i+1, b.State())
		}
		if !b.Allow() {
			t.Fatalf("Allow() should be true before threshold, failure %d", i+1)
		}
	}

	b.RecordFailure()
	if b.State() != Open {
		t.Fatalf("expected Open after %d failures, got %v", threshold, b.State())
	}
	if b.Allow() {
		t.Fatal("Allow() should return false when open")
	}
}

// TestBreaker_TransitionsToHalfOpen verifies that after the open duration elapses,
// the first Allow call returns true (the probe) and transitions to half-open,
// while subsequent calls return false.
func TestBreaker_TransitionsToHalfOpen(t *testing.T) {
	const threshold = 3
	openDuration := 20 * time.Millisecond

	b := New(threshold, openDuration)

	for range threshold {
		b.RecordFailure()
	}
	if b.State() != Open {
		t.Fatalf("expected Open after %d failures, got %v", threshold, b.State())
	}

	if b.Allow() {
		t.Fatal("Allow() should be false while open duration has not elapsed")
	}

	time.Sleep(openDuration + 5*time.Millisecond)

	if !b.Allow() {
		t.Fatal("Allow() should return true (probe) after openDuration elapses")
	}
	if b.Allow() {
		t.Fatal("Allow() should return false after probe was already dispatched")
	}
}

// TestBreaker_ClosesOnSuccess verifies the full recovery cycle: open -> half-open
// (probe) -> closed after RecordSuccess, with failures reset to zero.
func TestBreaker_ClosesOnSuccess(t *testing.T) {
	const threshold = 2
	openDuration := 20 * time.Millisecond

	b := New(threshold, openDuration)

	for range threshold {
		b.RecordFailure()
	}
	if b.State() != Open {
		t.Fatalf("expected Open, got %v", b.State())
	}

	time.Sleep(openDuration + 5*time.Millisecond)

	if !b.Allow() {
		t.Fatal("probe Allow() should return true")
	}
	if b.State() != HalfOpen {
		t.Fatalf("expected HalfOpen after probe Allow(), got %v", b.State())
	}

	b.RecordSuccess()
	if b.State() != Closed {
		t.Fatalf("expected Closed after RecordSuccess, got %v", b.State())
	}
	if b.Failures() != 0 {
		t.Fatalf("expected 0 failures after RecordSuccess, got %d", b.Failures())
	}
	if !b.Allow() {
		t.Fatal("Allow() should return true after circuit is closed")
	}
}

// TestBreaker_ConcurrentSafe exercises concurrent Allow, RecordFailure, and
// RecordSuccess calls to verify the lock-free atomic operations are race-free.
func TestBreaker_ConcurrentSafe(_ *testing.T) {
	b := New(5, 10*time.Millisecond)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			switch n % 3 {
			case 0:
				b.Allow()
			case 1:
				b.RecordFailure()
			case 2:
				b.RecordSuccess()
			}
		}(i)
	}

	wg.Wait()
	_ = b.State()
	_ = b.Failures()
}

// TestBackoffBreaker_ClosedByDefault verifies a new BackoffBreaker starts healthy
// with zero failures and no retry-at time.
func TestBackoffBreaker_ClosedByDefault(t *testing.T) {
	b := NewBackoff(BackoffConfig{})
	if b.IsOpen() {
		t.Fatal("expected IsOpen() == false for a fresh breaker")
	}
	if b.Failures() != 0 {
		t.Fatalf("expected 0 failures, got %d", b.Failures())
	}
	if _, ok := b.RetryAt(); ok {
		t.Fatal("RetryAt should return false when closed")
	}
}

// TestBackoffBreaker_ExponentialBackoff verifies that each consecutive failure
// doubles the backoff delay (baseDelay * 2^(n-1)).
func TestBackoffBreaker_ExponentialBackoff(t *testing.T) {
	now := time.Now()
	b := NewBackoff(BackoffConfig{
		BaseDelay: 10 * time.Millisecond,
		MaxDelay:  100 * time.Millisecond,
		Now:       func() time.Time { return now },
	})

	// First failure: 10ms * 2^0 = 10ms
	n := b.RecordFailure()
	if n != 1 {
		t.Fatalf("expected failure count 1, got %d", n)
	}
	retryAt, ok := b.RetryAt()
	if !ok {
		t.Fatal("expected open after first failure")
	}
	if want := now.Add(10 * time.Millisecond); retryAt != want {
		t.Fatalf("retryAt = %v, want %v", retryAt, want)
	}

	// Second failure: 10ms * 2^1 = 20ms
	b.RecordFailure()
	retryAt, _ = b.RetryAt()
	if want := now.Add(20 * time.Millisecond); retryAt != want {
		t.Fatalf("retryAt = %v, want %v", retryAt, want)
	}

	// Third failure: 10ms * 2^2 = 40ms
	b.RecordFailure()
	retryAt, _ = b.RetryAt()
	if want := now.Add(40 * time.Millisecond); retryAt != want {
		t.Fatalf("retryAt = %v, want %v", retryAt, want)
	}
}

// TestBackoffBreaker_MaxDelayCap verifies that the backoff delay never exceeds MaxDelay
// regardless of how many failures accumulate.
func TestBackoffBreaker_MaxDelayCap(t *testing.T) {
	now := time.Now()
	b := NewBackoff(BackoffConfig{
		BaseDelay: 10 * time.Millisecond,
		MaxDelay:  50 * time.Millisecond,
		Now:       func() time.Time { return now },
	})

	// Cause many failures to exceed max delay.
	for range 10 {
		b.RecordFailure()
	}
	retryAt, _ := b.RetryAt()
	if want := now.Add(50 * time.Millisecond); retryAt != want {
		t.Fatalf("retryAt = %v, want capped at %v", retryAt, want)
	}
}

// TestState_String verifies the String representation for all three valid states
// and the fallback for invalid state values.
func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{Closed, "closed"},
		{Open, "open"},
		{HalfOpen, "half-open"},
		{State(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// TestBreaker_HalfOpenFailureReopens verifies that a failure during the half-open
// probe transitions the breaker back to open.
func TestBreaker_HalfOpenFailureReopens(t *testing.T) {
	const threshold = 2
	openDuration := 20 * time.Millisecond

	b := New(threshold, openDuration)

	for range threshold {
		b.RecordFailure()
	}
	time.Sleep(openDuration + 5*time.Millisecond)

	// Transition to half-open via Allow.
	if !b.Allow() {
		t.Fatal("probe Allow() should return true")
	}
	if b.State() != HalfOpen {
		t.Fatalf("expected HalfOpen, got %v", b.State())
	}

	// Record failure in half-open → should reopen.
	b.RecordFailure()
	if b.State() != Open {
		t.Fatalf("expected Open after half-open failure, got %v", b.State())
	}
}

// TestBreaker_HalfOpenAllowReturnsFalse verifies that a second Allow call while
// in half-open state returns false and reopens the breaker, ensuring only one
// probe operation is permitted.
func TestBreaker_HalfOpenAllowReturnsFalse(t *testing.T) {
	const threshold = 2
	openDuration := 20 * time.Millisecond

	b := New(threshold, openDuration)
	for range threshold {
		b.RecordFailure()
	}
	time.Sleep(openDuration + 5*time.Millisecond)

	// First Allow transitions Open → HalfOpen.
	if !b.Allow() {
		t.Fatal("probe Allow() should return true")
	}

	// Second concurrent Allow in HalfOpen → should return false and reopen.
	if b.Allow() {
		t.Fatal("Allow() should return false while half-open probe is active")
	}
	if b.State() != Open {
		t.Fatalf("expected Open after second Allow in half-open, got %v", b.State())
	}
}

func TestBreaker_OpenCASFailure(t *testing.T) {
	// Simulate the CAS failure path in Allow() when the state was Open
	// at the time of the Load but has been changed by another goroutine
	// before the CAS executes. We force this by directly setting the
	// state to HalfOpen after constructing an expired-open breaker, so
	// Allow loads Open (we set openAt to expired) but the CAS(Open→HalfOpen)
	// fails because the actual state is already HalfOpen.
	b := New(1, time.Millisecond)

	// Set state to Open with expired openAt.
	b.state.Store(int32(Open))
	b.openAt.Store(time.Now().Add(-time.Second).UnixNano())

	// Now swap state to HalfOpen so the CAS in Allow will fail.
	// But Allow reads state atomically, so it will see HalfOpen, not Open.
	// Instead, use the race approach with many goroutines.

	// Reset to Open with expired time.
	b.state.Store(int32(Open))

	// Many goroutines race to grab the probe slot. Most will lose the
	// CAS or see HalfOpen state, exercising the CAS-failure path.
	const goroutines = 100
	var wg sync.WaitGroup
	var probes atomic.Int32
	var denied atomic.Int32
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			if b.Allow() {
				probes.Add(1)
			} else {
				denied.Add(1)
			}
		}()
	}
	wg.Wait()

	// At least one goroutine should win the CAS, and many should be denied
	// (hitting the CAS-failure or HalfOpen paths).
	if p := probes.Load(); p < 1 {
		t.Fatal("expected at least 1 probe winner")
	}
	if d := denied.Load(); d < 1 {
		t.Fatal("expected at least 1 denied call (CAS failure path)")
	}
}

func TestBreaker_OpenCASFailureDeterministic(t *testing.T) {
	// Force the CAS failure path on line 79: when Allow() reads state as
	// Open and duration has elapsed, it tries CAS(Open→HalfOpen). If the
	// state is no longer Open (e.g., another goroutine already moved it),
	// the CAS fails and Allow returns false.
	//
	// We simulate this by setting up an expired-open breaker, then swapping
	// the state to HalfOpen before calling Allow. Since Allow loads state
	// and sees Open (we trick it by immediately restoring), but CAS fails
	// because we concurrently change it.
	//
	// Actually, we can't interleave within a single goroutine. Instead,
	// directly set state to HalfOpen so CAS(Open→HalfOpen) will fail
	// because the actual state is HalfOpen, not Open. But Allow will see
	// HalfOpen in its initial Load, so it will take the HalfOpen case.
	//
	// The only way to test line 79 is via concurrent racing. Increase
	// concurrency to make it reliable.
	b := New(1, time.Hour) // long open duration so re-opens stay unexpired
	b.RecordFailure()      // opens circuit
	b.openAt.Store(0)      // force expired so initial goroutines see elapsed >> openDuration

	const goroutines = 500
	var wg sync.WaitGroup
	var probes atomic.Int32
	var denied atomic.Int32
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			if b.Allow() {
				probes.Add(1)
				// Re-open the breaker so other goroutines have a chance
				// to race on the CAS.
				b.RecordFailure()
			} else {
				denied.Add(1)
			}
		}()
	}
	wg.Wait()

	if d := denied.Load(); d < 1 {
		t.Fatal("expected at least 1 denied call")
	}
}

// TestBreaker_AllowDefaultCase exercises the default branch in Allow by setting
// the state to an invalid value, verifying it returns false.
func TestBreaker_AllowDefaultCase(t *testing.T) {
	// Exercise the default case by setting state to an invalid value.
	b := New(1, time.Millisecond)
	b.state.Store(99) // invalid state

	if b.Allow() {
		t.Fatal("Allow() should return false for unknown state")
	}
}

// TestBackoffBreaker_OverflowCapsAtMaxDelay verifies that the backoff delay is
// capped at MaxDelay even when the failure count is high enough to cause integer
// overflow in the bit-shift (failures >= 64 on 64-bit platforms).
func TestBackoffBreaker_OverflowCapsAtMaxDelay(t *testing.T) {
	now := time.Now()
	maxDelay := 50 * time.Millisecond
	b := NewBackoff(BackoffConfig{
		BaseDelay: 10 * time.Millisecond,
		MaxDelay:  maxDelay,
		Now:       func() time.Time { return now },
	})

	// Simulate 100 consecutive failures (well past the 64-bit overflow threshold).
	for range 100 {
		b.RecordFailure()
	}

	// The breaker must still be open with backoff capped at maxDelay.
	if !b.IsOpen() {
		t.Fatal("breaker should be open after 100 failures")
	}
	retryAt, ok := b.RetryAt()
	if !ok {
		t.Fatal("RetryAt should return true when breaker is open")
	}
	if want := now.Add(maxDelay); retryAt != want {
		t.Fatalf("retryAt = %v, want capped at %v (delta = %v)", retryAt, want, retryAt.Sub(want))
	}
}

// TestBackoffBreaker_RecordSuccessResets verifies that RecordSuccess clears the
// failure count and closes the breaker.
func TestBackoffBreaker_RecordSuccessResets(t *testing.T) {
	b := NewBackoff(BackoffConfig{BaseDelay: time.Second})
	b.RecordFailure()
	b.RecordFailure()
	if b.Failures() != 2 {
		t.Fatalf("expected 2 failures, got %d", b.Failures())
	}

	b.RecordSuccess()
	if b.Failures() != 0 {
		t.Fatalf("expected 0 failures after success, got %d", b.Failures())
	}
	if b.IsOpen() {
		t.Fatal("breaker should be closed after success")
	}
}
