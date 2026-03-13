package store

import (
	"encoding/json"
	"errors"
	"testing"
)

// allStatuses lists every defined TaskStatus for exhaustive negative-case coverage.
var allStatuses = []TaskStatus{
	TaskStatusBacklog,
	TaskStatusInProgress,
	TaskStatusCommitting,
	TaskStatusWaiting,
	TaskStatusDone,
	TaskStatusFailed,
	TaskStatusCancelled,
}

func TestValidateTransition(t *testing.T) {
	// valid transitions derived from allowedTransitions map.
	valid := []struct {
		from, to TaskStatus
	}{
		{TaskStatusBacklog, TaskStatusInProgress},
		{TaskStatusInProgress, TaskStatusWaiting},
		{TaskStatusInProgress, TaskStatusFailed},
		{TaskStatusInProgress, TaskStatusCancelled},
		{TaskStatusCommitting, TaskStatusDone},
		{TaskStatusCommitting, TaskStatusFailed},
		{TaskStatusWaiting, TaskStatusInProgress},
		{TaskStatusWaiting, TaskStatusCommitting},
		{TaskStatusWaiting, TaskStatusCancelled},
		{TaskStatusFailed, TaskStatusBacklog},
		{TaskStatusFailed, TaskStatusCancelled},
		{TaskStatusDone, TaskStatusCancelled},
		{TaskStatusCancelled, TaskStatusBacklog},
	}

	for _, tc := range valid {
		if err := ValidateTransition(tc.from, tc.to); err != nil {
			t.Errorf("ValidateTransition(%s, %s): expected nil, got %v", tc.from, tc.to, err)
		}
	}

	// invalid: every status → itself, plus a sampling of known-bad edges.
	for _, s := range allStatuses {
		if err := ValidateTransition(s, s); err == nil {
			t.Errorf("ValidateTransition(%s, %s): expected error for self-transition, got nil", s, s)
		} else if !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("ValidateTransition(%s, %s): error should wrap ErrInvalidTransition, got %v", s, s, err)
		}
	}

	// spot-check specific illegal edges
	illegal := []struct {
		from, to TaskStatus
	}{
		{TaskStatusBacklog, TaskStatusDone},
		{TaskStatusBacklog, TaskStatusCancelled},
		{TaskStatusInProgress, TaskStatusCommitting},
		{TaskStatusInProgress, TaskStatusDone},
		{TaskStatusWaiting, TaskStatusDone},
		{TaskStatusCommitting, TaskStatusBacklog},
		{TaskStatusDone, TaskStatusBacklog},
		{TaskStatusCancelled, TaskStatusDone},
	}
	for _, tc := range illegal {
		if err := ValidateTransition(tc.from, tc.to); err == nil {
			t.Errorf("ValidateTransition(%s, %s): expected error, got nil", tc.from, tc.to)
		} else if !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("ValidateTransition(%s, %s): error should wrap ErrInvalidTransition, got %v", tc.from, tc.to, err)
		}
	}
}

func TestTaskStatus_CanTransitionTo(t *testing.T) {
	// A few representative positive cases.
	positive := []struct {
		from, to TaskStatus
	}{
		{TaskStatusBacklog, TaskStatusInProgress},
		{TaskStatusInProgress, TaskStatusWaiting},
		{TaskStatusWaiting, TaskStatusCommitting},
		{TaskStatusFailed, TaskStatusBacklog},
		{TaskStatusCancelled, TaskStatusBacklog},
	}
	for _, tc := range positive {
		if !tc.from.CanTransitionTo(tc.to) {
			t.Errorf("%s.CanTransitionTo(%s) = false, want true", tc.from, tc.to)
		}
	}

	// Self-transitions must always be false.
	for _, s := range allStatuses {
		if s.CanTransitionTo(s) {
			t.Errorf("%s.CanTransitionTo(%s) = true, want false (self-transition)", s, s)
		}
	}
}

func TestTaskStatus_AllowedTransitions(t *testing.T) {
	tests := []struct {
		status   TaskStatus
		expected []TaskStatus
	}{
		{TaskStatusBacklog, []TaskStatus{TaskStatusInProgress}},
		{TaskStatusInProgress, []TaskStatus{TaskStatusBacklog, TaskStatusWaiting, TaskStatusFailed, TaskStatusCancelled}},
		{TaskStatusCommitting, []TaskStatus{TaskStatusDone, TaskStatusFailed}},
		{TaskStatusWaiting, []TaskStatus{TaskStatusInProgress, TaskStatusCommitting, TaskStatusCancelled}},
		{TaskStatusFailed, []TaskStatus{TaskStatusBacklog, TaskStatusCancelled}},
		{TaskStatusDone, []TaskStatus{TaskStatusCancelled}},
		{TaskStatusCancelled, []TaskStatus{TaskStatusBacklog}},
	}

	for _, tc := range tests {
		got := tc.status.AllowedTransitions()
		if len(got) != len(tc.expected) {
			t.Errorf("%s.AllowedTransitions(): len = %d, want %d (got %v, want %v)",
				tc.status, len(got), len(tc.expected), got, tc.expected)
			continue
		}
		for i, g := range got {
			if g != tc.expected[i] {
				t.Errorf("%s.AllowedTransitions()[%d] = %s, want %s", tc.status, i, g, tc.expected[i])
			}
		}
	}

	// An unknown status should return nil (no outgoing transitions).
	unknown := TaskStatus("unknown")
	if got := unknown.AllowedTransitions(); got != nil {
		t.Errorf("AllowedTransitions() for unknown status = %v, want nil", got)
	}
}

func TestTask_HasTag(t *testing.T) {
	task := &Task{Tags: []string{"backend", "idea-agent"}}

	if !task.HasTag("backend") {
		t.Error("expected HasTag to find existing tag")
	}
	if task.HasTag("IDEA-AGENT") {
		t.Error("expected case-sensitive tag matching")
	}
	if !task.HasTag("idea-agent") {
		t.Error("expected HasTag to find second tag")
	}
	if task.HasTag("frontend") {
		t.Error("expected HasTag to return false for missing tag")
	}
}

// TestTaskBudgetFieldsRoundTrip verifies that MaxCostUSD and MaxInputTokens
// survive JSON marshal→unmarshal with correct values, and that zero values are
// omitted (omitempty) for backwards compatibility with existing task files.
func TestTaskBudgetFieldsRoundTrip(t *testing.T) {
	original := Task{
		MaxCostUSD:     1.5,
		MaxInputTokens: 50000,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.MaxCostUSD != 1.5 {
		t.Errorf("MaxCostUSD = %f, want 1.5", decoded.MaxCostUSD)
	}
	if decoded.MaxInputTokens != 50000 {
		t.Errorf("MaxInputTokens = %d, want 50000", decoded.MaxInputTokens)
	}

	// Zero values should be omitted (omitempty).
	zero := Task{}
	zeroData, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("json.Marshal zero: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(zeroData, &m); err != nil {
		t.Fatalf("json.Unmarshal zero: %v", err)
	}
	if _, ok := m["max_cost_usd"]; ok {
		t.Error("max_cost_usd should be omitted from JSON when zero (omitempty)")
	}
	if _, ok := m["max_input_tokens"]; ok {
		t.Error("max_input_tokens should be omitted from JSON when zero (omitempty)")
	}
}
