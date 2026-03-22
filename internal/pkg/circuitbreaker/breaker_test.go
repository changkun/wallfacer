package circuitbreaker

import (
	"sync"
	"testing"
	"time"
)

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
