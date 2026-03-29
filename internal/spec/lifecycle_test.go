package spec

import (
	"errors"
	"testing"
)

func TestValidateTransition_AllValid(t *testing.T) {
	valid := []struct {
		from, to Status
	}{
		{StatusVague, StatusDrafted},
		{StatusDrafted, StatusValidated},
		{StatusDrafted, StatusStale},
		{StatusValidated, StatusComplete},
		{StatusValidated, StatusStale},
		{StatusComplete, StatusStale},
		{StatusStale, StatusDrafted},
		{StatusStale, StatusValidated},
	}
	for _, tc := range valid {
		if err := ValidateTransition(tc.from, tc.to); err != nil {
			t.Errorf("ValidateTransition(%s, %s): expected nil, got %v", tc.from, tc.to, err)
		}
	}
}

func TestValidateTransition_AllInvalid(t *testing.T) {
	invalid := []struct {
		from, to Status
	}{
		{StatusVague, StatusValidated},
		{StatusVague, StatusComplete},
		{StatusVague, StatusStale},
		{StatusDrafted, StatusVague},
		{StatusDrafted, StatusComplete},
		{StatusValidated, StatusVague},
		{StatusValidated, StatusDrafted},
		{StatusComplete, StatusVague},
		{StatusComplete, StatusDrafted},
		{StatusComplete, StatusValidated},
		{StatusStale, StatusVague},
		{StatusStale, StatusComplete},
	}
	for _, tc := range invalid {
		if err := ValidateTransition(tc.from, tc.to); err == nil {
			t.Errorf("ValidateTransition(%s, %s): expected error, got nil", tc.from, tc.to)
		}
	}
}

func TestValidateTransition_SameStatus(t *testing.T) {
	for _, s := range ValidStatuses() {
		if err := ValidateTransition(s, s); err == nil {
			t.Errorf("ValidateTransition(%s, %s): expected error for same-to-same", s, s)
		}
	}
}

func TestValidateTransition_ErrorWrapping(t *testing.T) {
	err := ValidateTransition(StatusVague, StatusComplete)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("error should wrap ErrInvalidTransition, got %v", err)
	}
}

func TestValidStatuses(t *testing.T) {
	statuses := ValidStatuses()
	if len(statuses) != 5 {
		t.Fatalf("len(ValidStatuses()) = %d, want 5", len(statuses))
	}
	want := map[Status]bool{
		StatusVague: true, StatusDrafted: true, StatusValidated: true,
		StatusComplete: true, StatusStale: true,
	}
	for _, s := range statuses {
		if !want[s] {
			t.Errorf("unexpected status %q", s)
		}
	}
}

func TestValidTracks(t *testing.T) {
	tracks := ValidTracks()
	if len(tracks) != 4 {
		t.Fatalf("len(ValidTracks()) = %d, want 4", len(tracks))
	}
	want := map[Track]bool{
		TrackFoundations: true, TrackLocal: true, TrackCloud: true, TrackShared: true,
	}
	for _, tr := range tracks {
		if !want[tr] {
			t.Errorf("unexpected track %q", tr)
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
