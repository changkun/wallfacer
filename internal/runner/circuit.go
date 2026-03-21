package runner

import (
	"log/slog"
	"sync/atomic"
	"time"

	"changkun.de/x/wallfacer/internal/logger"
)

// circuitState represents the three states of the circuit breaker.
type circuitState int32

const (
	circuitClosed   circuitState = 0 // normal: allow launches
	circuitOpen     circuitState = 1 // tripped: block launches
	circuitHalfOpen circuitState = 2 // probing: allow one launch
)

func (s circuitState) String() string {
	switch s {
	case circuitClosed:
		return "closed"
	case circuitOpen:
		return "open"
	case circuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker is a three-state circuit breaker for container launch
// operations. It prevents cascade task failures when the container runtime
// (podman/docker) is transiently unavailable.
//
// All state transitions use atomic operations for lock-free performance.
type CircuitBreaker struct {
	state        atomic.Int32
	failures     atomic.Int32
	threshold    int           // consecutive failures required to open circuit
	openDuration time.Duration // how long to stay open before probing (half-open)
	openAt       atomic.Int64  // unix nanoseconds when circuit was last opened
	logger       *slog.Logger
}

// NewCircuitBreaker creates a CircuitBreaker that opens after threshold
// consecutive failures and stays open for openDuration before probing.
func NewCircuitBreaker(threshold int, openDuration time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold:    threshold,
		openDuration: openDuration,
		logger:       logger.Runner,
	}
}

// Allow reports whether a container launch should be permitted.
//
//   - Closed state: always returns true.
//   - Open state: returns false unless openDuration has elapsed, in which
//     case the circuit transitions to half-open and returns true (the probe).
//   - Half-open state: returns false; also transitions back to open so that
//     subsequent calls block until RecordSuccess closes the circuit.
func (cb *CircuitBreaker) Allow() bool {
	state := circuitState(cb.state.Load())
	switch state {
	case circuitClosed:
		return true

	case circuitOpen:
		elapsed := time.Now().UnixNano() - cb.openAt.Load()
		if elapsed < cb.openDuration.Nanoseconds() {
			return false
		}
		// Enough time has passed: try to grab the single probe slot by
		// transitioning to half-open. Only one goroutine wins the CAS.
		if cb.state.CompareAndSwap(int32(circuitOpen), int32(circuitHalfOpen)) {
			cb.logger.Warn("circuit breaker half-open: sending probe")
			return true
		}
		// Another goroutine already transitioned; block this call.
		return false

	case circuitHalfOpen:
		// Probe was already dispatched. Transition back to open so that
		// any further callers block until the probe resolves.
		if cb.state.CompareAndSwap(int32(circuitHalfOpen), int32(circuitOpen)) {
			cb.openAt.Store(time.Now().UnixNano())
			cb.logger.Warn("circuit breaker reopened: probe not yet resolved")
		}
		return false
	}

	return false
}

// RecordSuccess resets the failure counter and closes the circuit.
// Call this when a container launch succeeds.
func (cb *CircuitBreaker) RecordSuccess() {
	prev := circuitState(cb.state.Swap(int32(circuitClosed)))
	cb.failures.Store(0)
	if prev != circuitClosed {
		cb.logger.Warn("circuit breaker closed", "previous_state", prev.String())
	}
}

// RecordFailure increments the consecutive failure counter and trips the
// circuit to open when the threshold is reached. If the circuit is already
// in half-open state (probe failed), it transitions directly back to open.
func (cb *CircuitBreaker) RecordFailure() {
	f := int(cb.failures.Add(1))
	state := circuitState(cb.state.Load())

	switch state {
	case circuitClosed:
		if f >= cb.threshold {
			if cb.state.CompareAndSwap(int32(circuitClosed), int32(circuitOpen)) {
				cb.openAt.Store(time.Now().UnixNano())
				cb.logger.Warn("circuit breaker opened",
					"failures", f, "threshold", cb.threshold)
			}
		}

	case circuitHalfOpen:
		// Probe failed: go back to open immediately.
		if cb.state.CompareAndSwap(int32(circuitHalfOpen), int32(circuitOpen)) {
			cb.openAt.Store(time.Now().UnixNano())
			cb.logger.Warn("circuit breaker reopened after failed probe")
		}
	}
	// Already open: nothing to change.
}

// State returns the human-readable name of the current circuit state.
func (cb *CircuitBreaker) State() string {
	return circuitState(cb.state.Load()).String()
}

// Failures returns the current consecutive failure count.
func (cb *CircuitBreaker) Failures() int {
	return int(cb.failures.Load())
}

// isContainerRuntimeError reports whether err signals a container runtime
// failure (the daemon or binary is unavailable) rather than a normal task
// exit from the agent process inside the container.
//
// Specifically it matches:
//   - Exit code 125: the container engine (Docker/Podman) itself failed.
//   - Non-exit errors with "connection refused" (daemon not running).
//   - Non-exit errors with "no such file or directory" (binary not found).
//
// Normal agent exit codes (1–124) are intentionally NOT matched.
func isContainerRuntimeError(err error) bool {
	if err == nil {
		return false
	}

	// Check for a process exit code via the ExitCode() interface.
	// cmd.Run() returns *exec.ExitError directly (not wrapped), so a direct
	// type assertion is sufficient. We define a local interface to avoid
	// importing os/exec in this file.
	type exitCoder interface{ ExitCode() int }
	if e, ok := err.(exitCoder); ok {
		// Exit code 125 = container engine error (not agent error).
		return e.ExitCode() == 125
	}

	// Non-exit error: check the message for daemon/binary unavailability.
	msg := err.Error()
	lower := make([]byte, len(msg))
	for i := 0; i < len(msg); i++ {
		c := msg[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		lower[i] = c
	}
	lmsg := string(lower)
	return containsSubstring(lmsg, "connection refused") ||
		containsSubstring(lmsg, "no such file or directory")
}

// containsSubstring is a simple substring check to keep imports minimal.
func containsSubstring(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
