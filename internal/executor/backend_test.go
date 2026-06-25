package executor

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

// TestBackendStateString verifies the human-readable name for each backend
// lifecycle state, including the "unknown" fallback for out-of-range values.
func TestBackendStateString(t *testing.T) {
	tests := []struct {
		state BackendState
		want  string
	}{
		{StateCreating, "creating"},
		{StateRunning, "running"},
		{StateStreaming, "streaming"},
		{StateStopping, "stopping"},
		{StateStopped, "stopped"},
		{StateFailed, "failed"},
		{BackendState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("BackendState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// Note: tests that exercised the localHandle Kill/Wait state-transition
// guards lived here before host-default removed the container backend.
// Equivalent coverage for hostHandle should land alongside the harness
// migration; the state machine itself is exercised by TestStateMachine*.
