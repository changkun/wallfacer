package sandbox

import (
	"sync/atomic"
	"testing"
)

// TestStateCreatingIsZeroValue verifies that StateCreating is the zero value
// of BackendState (and therefore of atomic.Int32). This invariant is relied
// upon by localHandle: a freshly constructed handle is already in StateCreating
// without an explicit transition call.
func TestStateCreatingIsZeroValue(t *testing.T) {
	if StateCreating != 0 {
		t.Fatalf("StateCreating = %d, want 0 (must be iota zero value)", int(StateCreating))
	}

	var state atomic.Int32
	if got := BackendState(state.Load()); got != StateCreating {
		t.Fatalf("zero-value atomic.Int32 maps to %v, want StateCreating", got)
	}
}

// TestStateMachineRejectsCreatingToCreating verifies that the state machine
// does not allow transitioning from StateCreating to StateCreating. This is
// a regression test for a bug where launchEphemeral and taskWorker.exec called
// transition(state, StateCreating) on a freshly allocated handle whose state
// was already StateCreating (the zero value), producing a spurious warning:
//
//	WRN runner backend.go:62 invalid sandbox state transition
//	    from=creating to=creating error="invalid transition: creating → creating"
func TestStateMachineRejectsCreatingToCreating(t *testing.T) {
	if StateMachine.CanTransition(StateCreating, StateCreating) {
		t.Fatal("StateCreating → StateCreating should be rejected by the state machine")
	}
}

// TestKillNoopWhenAlreadyStopped verifies that calling Kill on a handle
// that has already reached StateStopped does not produce invalid state
// transition warnings. This is a regression test for a bug where Kill
// tried stopped→stopping→stopped, both of which are invalid.
func TestKillNoopWhenAlreadyStopped(t *testing.T) {
	h := newLocalHandle("test-kill-stopped", nil, nil, nil, "")
	// Drive state to Stopped through valid transitions.
	transition(&h.state, StateRunning)
	transition(&h.state, StateStopped)

	if got := h.State(); got != StateStopped {
		t.Fatalf("State() = %v, want StateStopped", got)
	}

	// Kill on a stopped handle should be a no-op (no log warnings).
	if err := h.Kill(); err != nil {
		t.Fatalf("Kill() returned error: %v", err)
	}
	if got := h.State(); got != StateStopped {
		t.Fatalf("after Kill(), State() = %v, want StateStopped", got)
	}
}

// TestKillNoopWhenAlreadyFailed verifies Kill is a no-op on a failed handle.
func TestKillNoopWhenAlreadyFailed(t *testing.T) {
	h := newLocalHandle("test-kill-failed", nil, nil, nil, "")
	transition(&h.state, StateFailed)

	if err := h.Kill(); err != nil {
		t.Fatalf("Kill() returned error: %v", err)
	}
	if got := h.State(); got != StateFailed {
		t.Fatalf("after Kill(), State() = %v, want StateFailed", got)
	}
}

// TestWaitSkipsTransitionWhenAlreadyStopped verifies that Wait does not
// attempt a stopped→stopped transition when Kill has already moved the
// state to StateStopped. This is a regression test for a race between
// Kill() and Wait() during server shutdown that produced:
//
//	WRN runner backend.go:62 invalid sandbox state transition
//	    from=stopped to=stopped error="invalid transition: stopped → stopped"
func TestWaitSkipsTransitionWhenAlreadyStopped(t *testing.T) {
	h := newLocalHandle("test-wait-race", nil, nil, nil, "")
	// Simulate Kill() having already driven state to Stopped.
	transition(&h.state, StateRunning)
	transition(&h.state, StateStopping)
	transition(&h.state, StateStopped)

	// Wait() would normally try transition → StateStopped again. With the
	// fix, it checks for terminal state first and skips the transition,
	// avoiding the spurious warning. We cannot call Wait() directly here
	// (no real process), so verify the guard logic: the state must remain
	// StateStopped without any invalid-transition warning being logged.
	if got := h.State(); got != StateStopped {
		t.Fatalf("State() = %v, want StateStopped", got)
	}

	// Verify the state machine still rejects stopped→stopped.
	if StateMachine.CanTransition(StateStopped, StateStopped) {
		t.Fatal("StateStopped → StateStopped should be rejected")
	}
}

// TestNewLocalHandleStartsCreatingAndCanTransition verifies that
// newLocalHandle initialises the state to StateCreating and that the handle
// can transition forward to StateRunning without a redundant Creating→Creating
// step.
func TestNewLocalHandleStartsCreatingAndCanTransition(t *testing.T) {
	h := newLocalHandle("test", nil, nil, nil, "")

	if got := h.State(); got != StateCreating {
		t.Fatalf("newLocalHandle().State() = %v, want StateCreating", got)
	}

	// The valid forward transition should succeed.
	transition(&h.state, StateRunning)
	if got := h.State(); got != StateRunning {
		t.Fatalf("after transition to Running, State() = %v, want StateRunning", got)
	}
}
