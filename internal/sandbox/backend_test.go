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
