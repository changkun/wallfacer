package spec

import (
	"changkun.de/x/wallfacer/internal/pkg/statemachine"
)

// ErrInvalidTransition is returned when a status transition is not allowed.
var ErrInvalidTransition = statemachine.ErrInvalidTransition

// specMachine is the spec lifecycle state machine.
var specMachine = statemachine.New(map[Status][]Status{
	StatusVague:     {StatusDrafted},
	StatusDrafted:   {StatusValidated, StatusStale},
	StatusValidated: {StatusComplete, StatusStale},
	StatusComplete:  {StatusStale},
	StatusStale:     {StatusDrafted, StatusValidated},
})

// ValidateTransition checks whether transitioning from one status to another
// is allowed by the spec lifecycle state machine.
func ValidateTransition(from, to Status) error {
	return specMachine.Validate(from, to)
}

// ValidStatuses returns all valid spec status values.
func ValidStatuses() []Status {
	return []Status{StatusVague, StatusDrafted, StatusValidated, StatusComplete, StatusStale}
}

// ValidTracks returns all valid spec track values.
func ValidTracks() []Track {
	return []Track{TrackFoundations, TrackLocal, TrackCloud, TrackShared}
}

// ValidEfforts returns all valid spec effort values.
func ValidEfforts() []Effort {
	return []Effort{EffortSmall, EffortMedium, EffortLarge, EffortXLarge}
}
