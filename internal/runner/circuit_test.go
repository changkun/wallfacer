package runner

import (
	"sync"
	"testing"
	"time"
)

// TestCircuitBreaker_ClosedByDefault verifies that a newly created breaker
// is in the closed state and allows launches immediately.
func TestCircuitBreaker_ClosedByDefault(t *testing.T) {
	cb := NewCircuitBreaker(3, 30*time.Second)
	if !cb.Allow() {
		t.Fatal("expected Allow() == true for a fresh circuit breaker")
	}
	if cb.State() != "closed" {
		t.Fatalf("expected state 'closed', got %q", cb.State())
	}
	if cb.Failures() != 0 {
		t.Fatalf("expected 0 failures, got %d", cb.Failures())
	}
}

// TestCircuitBreaker_OpensAfterThreshold verifies that recording N consecutive
// failures (where N == threshold) trips the circuit to open and blocks Allow().
func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	const threshold = 5
	cb := NewCircuitBreaker(threshold, 30*time.Second)

	// Failures below threshold must keep the circuit closed.
	for i := 0; i < threshold-1; i++ {
		cb.RecordFailure()
		if cb.State() != "closed" {
			t.Fatalf("after %d failures circuit should be closed, got %q", i+1, cb.State())
		}
		if !cb.Allow() {
			t.Fatalf("Allow() should be true before threshold, failure %d", i+1)
		}
	}

	// The threshold-th failure must open the circuit.
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Fatalf("expected state 'open' after %d failures, got %q", threshold, cb.State())
	}
	if cb.Allow() {
		t.Fatal("Allow() should return false when circuit is open")
	}
}

// TestCircuitBreaker_TransitionsToHalfOpen verifies that after openDuration
// elapses the circuit allows exactly one probe (half-open) and then blocks
// again.
func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	const threshold = 3
	openDuration := 20 * time.Millisecond

	cb := NewCircuitBreaker(threshold, openDuration)

	// Trip the circuit.
	for i := 0; i < threshold; i++ {
		cb.RecordFailure()
	}
	if cb.State() != "open" {
		t.Fatalf("expected open after %d failures, got %q", threshold, cb.State())
	}

	// Allow() must be false immediately (duration not elapsed).
	if cb.Allow() {
		t.Fatal("Allow() should be false while open duration has not elapsed")
	}

	// Wait for the open window to expire.
	time.Sleep(openDuration + 5*time.Millisecond)

	// First Allow() after expiry should return true (probe dispatched).
	if !cb.Allow() {
		t.Fatal("Allow() should return true (probe) after openDuration elapses")
	}

	// Second Allow() with no RecordSuccess must block again.
	if cb.Allow() {
		t.Fatal("Allow() should return false after probe was already dispatched")
	}
}

// TestCircuitBreaker_ClosesOnSuccess verifies the full recovery path:
// open → half-open (probe) → RecordSuccess → closed.
func TestCircuitBreaker_ClosesOnSuccess(t *testing.T) {
	const threshold = 2
	openDuration := 20 * time.Millisecond

	cb := NewCircuitBreaker(threshold, openDuration)

	// Open the circuit.
	for i := 0; i < threshold; i++ {
		cb.RecordFailure()
	}
	if cb.State() != "open" {
		t.Fatalf("expected open, got %q", cb.State())
	}

	// Wait for probe window.
	time.Sleep(openDuration + 5*time.Millisecond)

	// First Allow() dispatches the probe (half-open).
	if !cb.Allow() {
		t.Fatal("probe Allow() should return true")
	}
	if cb.State() != "half-open" {
		t.Fatalf("expected half-open after probe Allow(), got %q", cb.State())
	}

	// Successful probe closes the circuit.
	cb.RecordSuccess()
	if cb.State() != "closed" {
		t.Fatalf("expected closed after RecordSuccess, got %q", cb.State())
	}
	if cb.Failures() != 0 {
		t.Fatalf("expected 0 failures after RecordSuccess, got %d", cb.Failures())
	}

	// Normal operation: Allow() must be true again.
	if !cb.Allow() {
		t.Fatal("Allow() should return true after circuit is closed")
	}
}

// TestCircuitBreaker_ConcurrentSafe launches 100 goroutines that concurrently
// call Allow, RecordFailure, and RecordSuccess. With -race it verifies there
// are no data races.
func TestCircuitBreaker_ConcurrentSafe(_ *testing.T) {
	cb := NewCircuitBreaker(5, 10*time.Millisecond)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			switch n % 3 {
			case 0:
				cb.Allow()
			case 1:
				cb.RecordFailure()
			case 2:
				cb.RecordSuccess()
			}
		}(i)
	}

	wg.Wait()
	// No assertion on final state — correctness is verified by the race detector.
	_ = cb.State()
	_ = cb.Failures()
}

// TestIsContainerRuntimeError verifies that the helper correctly classifies
// container runtime errors vs. normal agent exit codes.
func TestIsContainerRuntimeError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		want    bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "exit code 125 (container engine failure)",
			err:  &fakeExitError{code: 125},
			want: true,
		},
		{
			name: "exit code 1 (Claude agent failure)",
			err:  &fakeExitError{code: 1},
			want: false,
		},
		{
			name: "exit code 0 (success — not an error in practice)",
			err:  &fakeExitError{code: 0},
			want: false,
		},
		{
			name: "exit code 2 (normal non-zero task exit)",
			err:  &fakeExitError{code: 2},
			want: false,
		},
		{
			name: "connection refused (daemon down)",
			err:  fakeError("dial tcp: connect: connection refused"),
			want: true,
		},
		{
			name: "no such file or directory (binary missing)",
			err:  fakeError("fork/exec /opt/podman/bin/podman: no such file or directory"),
			want: true,
		},
		{
			name: "token limit error (not a runtime error)",
			err:  fakeError("exceeded token limit"),
			want: false,
		},
		{
			name: "random exec error",
			err:  fakeError("permission denied"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isContainerRuntimeError(tc.err)
			if got != tc.want {
				t.Errorf("isContainerRuntimeError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// fakeExitError implements the exitCoder interface used by isContainerRuntimeError.
type fakeExitError struct{ code int }

func (e *fakeExitError) ExitCode() int { return e.code }
func (e *fakeExitError) Error() string { return "exit status " + string(rune('0'+e.code)) }

// fakeError is a plain error type for testing non-exit errors.
type fakeError string

func (e fakeError) Error() string { return string(e) }
