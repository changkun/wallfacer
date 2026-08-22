package store

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"latere.ai/x/wallfacer/internal/pkg/statemachine"
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

func TestTaskMachine_Validate(t *testing.T) {
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
		if err := TaskMachine.Validate(tc.from, tc.to); err != nil {
			t.Errorf("TaskMachine.Validate(%s, %s): expected nil, got %v", tc.from, tc.to, err)
		}
	}

	// invalid: every status → itself, plus a sampling of known-bad edges.
	for _, s := range allStatuses {
		if err := TaskMachine.Validate(s, s); err == nil {
			t.Errorf("TaskMachine.Validate(%s, %s): expected error for self-transition, got nil", s, s)
		} else if !errors.Is(err, statemachine.ErrInvalidTransition) {
			t.Errorf("TaskMachine.Validate(%s, %s): error should wrap statemachine.ErrInvalidTransition, got %v", s, s, err)
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
		if err := TaskMachine.Validate(tc.from, tc.to); err == nil {
			t.Errorf("TaskMachine.Validate(%s, %s): expected error, got nil", tc.from, tc.to)
		} else if !errors.Is(err, statemachine.ErrInvalidTransition) {
			t.Errorf("TaskMachine.Validate(%s, %s): error should wrap statemachine.ErrInvalidTransition, got %v", tc.from, tc.to, err)
		}
	}
}

func TestTaskMachine_CanTransition(t *testing.T) {
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
		if !TaskMachine.CanTransition(tc.from, tc.to) {
			t.Errorf("TaskMachine.CanTransition(%s, %s) = false, want true", tc.from, tc.to)
		}
	}

	// Self-transitions must always be false.
	for _, s := range allStatuses {
		if TaskMachine.CanTransition(s, s) {
			t.Errorf("TaskMachine.CanTransition(%s, %s) = true, want false (self-transition)", s, s)
		}
	}
}

func TestTaskMachine_Allowed(t *testing.T) {
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
		got := TaskMachine.Allowed(tc.status)
		if len(got) != len(tc.expected) {
			t.Errorf("TaskMachine.Allowed(%s): len = %d, want %d (got %v, want %v)",
				tc.status, len(got), len(tc.expected), got, tc.expected)
			continue
		}
		for i, g := range got {
			if g != tc.expected[i] {
				t.Errorf("TaskMachine.Allowed(%s)[%d] = %s, want %s", tc.status, i, g, tc.expected[i])
			}
		}
	}

	// An unknown status should return nil (no outgoing transitions).
	unknown := TaskStatus("unknown")
	if got := TaskMachine.Allowed(unknown); got != nil {
		t.Errorf("TaskMachine.Allowed(unknown) = %v, want nil", got)
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

func TestSandboxActivityAgentSession(t *testing.T) {
	found := slices.Contains(SandboxActivities, SandboxActivityAgentSession)
	if !found {
		t.Error("SandboxActivityAgentSession not in SandboxActivities slice")
	}
}

func TestTaskUsage_Add(t *testing.T) {
	dst := TaskUsage{
		InputTokens:          10,
		OutputTokens:         20,
		CacheReadInputTokens: 5,
		CacheCreationTokens:  3,
		CostUSD:              0.10,
	}
	src := TaskUsage{
		InputTokens:          1,
		OutputTokens:         2,
		CacheReadInputTokens: 4,
		CacheCreationTokens:  6,
		CostUSD:              0.05,
	}
	dst.Add(src)
	if dst.InputTokens != 11 || dst.OutputTokens != 22 ||
		dst.CacheReadInputTokens != 9 || dst.CacheCreationTokens != 9 {
		t.Errorf("after Add token totals wrong: %+v", dst)
	}
	if diff := dst.CostUSD - 0.15; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("CostUSD = %v, want 0.15 (±1e-9)", dst.CostUSD)
	}
}

func TestTaskUsage_Add_ZeroOther(t *testing.T) {
	dst := TaskUsage{InputTokens: 100, CostUSD: 1.0}
	dst.Add(TaskUsage{})
	if dst.InputTokens != 100 || dst.CostUSD != 1.0 {
		t.Errorf("Add(zero) mutated dst: %+v", dst)
	}
}
