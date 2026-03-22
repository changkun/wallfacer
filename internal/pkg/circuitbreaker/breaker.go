// Package circuitbreaker provides circuit breaker implementations for fault
// isolation. Two variants are offered: a lock-free three-state [Breaker] for
// high-throughput scenarios and a mutex-based [BackoffBreaker] with exponential
// backoff for simpler per-feature use cases.
package circuitbreaker

import (
	"sync/atomic"
	"time"
)

// State represents the three states of a circuit breaker.
type State int32

const (
	Closed   State = 0 // normal: allow operations
	Open     State = 1 // tripped: block operations
	HalfOpen State = 2 // probing: allow one operation
)

// String returns the human-readable name of the state.
func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Breaker is a three-state circuit breaker using lock-free atomic operations.
//
// All state transitions use atomic CAS (Compare-And-Swap) for high-throughput
// concurrent access without mutex contention.
type Breaker struct {
	state        atomic.Int32
	failures     atomic.Int32
	threshold    int           // consecutive failures required to open circuit
	openDuration time.Duration // how long to stay open before probing (half-open)
	openAt       atomic.Int64  // unix nanoseconds when circuit was last opened
}

// New creates a Breaker that opens after threshold consecutive failures and
// stays open for openDuration before probing.
func New(threshold int, openDuration time.Duration) *Breaker {
	return &Breaker{
		threshold:    threshold,
		openDuration: openDuration,
	}
}

// Allow reports whether an operation should be permitted.
//
//   - Closed state: always returns true.
//   - Open state: returns false unless openDuration has elapsed, in which
//     case the circuit transitions to half-open and returns true (the probe).
//   - Half-open state: returns false; transitions back to open so that
//     subsequent calls block until RecordSuccess closes the circuit.
func (b *Breaker) Allow() bool {
	state := State(b.state.Load())
	switch state {
	case Closed:
		return true

	case Open:
		elapsed := time.Now().UnixNano() - b.openAt.Load()
		if elapsed < b.openDuration.Nanoseconds() {
			return false
		}
		// Try to grab the single probe slot via CAS.
		if b.state.CompareAndSwap(int32(Open), int32(HalfOpen)) {
			return true
		}
		return false

	case HalfOpen:
		// Probe was already dispatched. Transition back to open so that
		// further callers block until the probe resolves.
		if b.state.CompareAndSwap(int32(HalfOpen), int32(Open)) {
			b.openAt.Store(time.Now().UnixNano())
		}
		return false
	}

	return false
}

// RecordSuccess resets the failure counter and closes the circuit.
func (b *Breaker) RecordSuccess() {
	b.state.Swap(int32(Closed))
	b.failures.Store(0)
}

// RecordFailure increments the consecutive failure counter and trips the
// circuit to open when the threshold is reached. If the circuit is in
// half-open state (probe failed), it transitions directly back to open.
func (b *Breaker) RecordFailure() {
	f := int(b.failures.Add(1))
	state := State(b.state.Load())

	switch state {
	case Closed:
		if f >= b.threshold {
			if b.state.CompareAndSwap(int32(Closed), int32(Open)) {
				b.openAt.Store(time.Now().UnixNano())
			}
		}

	case HalfOpen:
		if b.state.CompareAndSwap(int32(HalfOpen), int32(Open)) {
			b.openAt.Store(time.Now().UnixNano())
		}
	}
}

// State returns the current circuit state.
func (b *Breaker) State() State {
	return State(b.state.Load())
}

// Failures returns the current consecutive failure count.
func (b *Breaker) Failures() int {
	return int(b.failures.Load())
}
