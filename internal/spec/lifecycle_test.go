package spec

import (
	"errors"
	"testing"

	"latere.ai/x/wallfacer/internal/pkg/statemachine"
)

func TestStatusMachine_AllValid(t *testing.T) {
	valid := []struct {
		from, to Status
	}{
		{StatusVague, StatusDrafted},
		{StatusDrafted, StatusValidated},
		{StatusDrafted, StatusStale},
		{StatusValidated, StatusTesting},
		{StatusValidated, StatusStale},
		{StatusTesting, StatusComplete},
		{StatusTesting, StatusStale},
		{StatusTesting, StatusArchived},
		{StatusComplete, StatusStale},
		{StatusStale, StatusDrafted},
		{StatusStale, StatusValidated},
		{StatusVague, StatusArchived},
		{StatusDrafted, StatusArchived},
		{StatusComplete, StatusArchived},
		{StatusStale, StatusArchived},
		{StatusArchived, StatusDrafted},
	}
	for _, tc := range valid {
		if err := StatusMachine.Validate(tc.from, tc.to); err != nil {
			t.Errorf("Validate(%s, %s): expected nil, got %v", tc.from, tc.to, err)
		}
	}
}

func TestStatusMachine_AllInvalid(t *testing.T) {
	invalid := []struct {
		from, to Status
	}{
		{StatusVague, StatusValidated},
		{StatusVague, StatusComplete},
		{StatusVague, StatusStale},
		{StatusDrafted, StatusVague},
		{StatusDrafted, StatusComplete},
		{StatusDrafted, StatusTesting},
		{StatusValidated, StatusVague},
		{StatusValidated, StatusDrafted},
		{StatusValidated, StatusComplete}, // completion gate: must go through testing
		{StatusTesting, StatusVague},
		{StatusTesting, StatusDrafted},
		{StatusTesting, StatusValidated},
		{StatusComplete, StatusVague},
		{StatusComplete, StatusDrafted},
		{StatusComplete, StatusValidated},
		{StatusComplete, StatusTesting},
		{StatusStale, StatusVague},
		{StatusStale, StatusComplete},
		{StatusStale, StatusTesting},
		{StatusValidated, StatusArchived},
		{StatusArchived, StatusComplete},
		{StatusArchived, StatusValidated},
		{StatusArchived, StatusTesting},
		{StatusArchived, StatusStale},
	}
	for _, tc := range invalid {
		if err := StatusMachine.Validate(tc.from, tc.to); err == nil {
			t.Errorf("Validate(%s, %s): expected error, got nil", tc.from, tc.to)
		}
	}
}

func TestStatusMachine_SameStatus(t *testing.T) {
	for _, s := range ValidStatuses() {
		if err := StatusMachine.Validate(s, s); err == nil {
			t.Errorf("Validate(%s, %s): expected error for same-to-same", s, s)
		}
	}
}

func TestStatusMachine_ErrorWrapping(t *testing.T) {
	err := StatusMachine.Validate(StatusVague, StatusComplete)
	if !errors.Is(err, statemachine.ErrInvalidTransition) {
		t.Errorf("error should wrap ErrInvalidTransition, got %v", err)
	}
}

func TestValidStatuses(t *testing.T) {
	statuses := ValidStatuses()
	if len(statuses) != 7 {
		t.Fatalf("len(ValidStatuses()) = %d, want 7", len(statuses))
	}
	want := map[Status]bool{
		StatusVague: true, StatusDrafted: true, StatusValidated: true,
		StatusTesting: true, StatusComplete: true, StatusStale: true,
		StatusArchived: true,
	}
	for _, s := range statuses {
		if !want[s] {
			t.Errorf("unexpected status %q", s)
		}
	}
}

func TestValidEfforts(t *testing.T) {
	efforts := ValidEfforts()
	if len(efforts) != 4 {
		t.Fatalf("len(ValidEfforts()) = %d, want 4", len(efforts))
	}
	want := map[Effort]bool{
		EffortSmall: true, EffortMedium: true, EffortLarge: true, EffortXLarge: true,
	}
	for _, e := range efforts {
		if !want[e] {
			t.Errorf("unexpected effort %q", e)
		}
	}
}
