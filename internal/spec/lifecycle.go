package spec

import (
	"latere.ai/x/wallfacer/internal/pkg/statemachine"
)

// StatusMachine is the spec lifecycle state machine.
//
// The completion gate: validated → complete is not a legal edge. A spec
// reaches complete only through testing, where the drift pipeline renders a
// verdict. validated → stale stays legal because stale propagation
// (FanOutStale) marks validated dependents stale when an upstream changes.
var StatusMachine = statemachine.New(map[Status][]Status{
	StatusVague:     {StatusDrafted, StatusArchived},
	StatusDrafted:   {StatusValidated, StatusStale, StatusArchived},
	StatusValidated: {StatusTesting, StatusStale},
	StatusTesting:   {StatusComplete, StatusStale, StatusArchived},
	StatusComplete:  {StatusStale, StatusArchived},
	StatusStale:     {StatusDrafted, StatusValidated, StatusArchived},
	StatusArchived:  {StatusDrafted},
})

// ValidStatuses returns all valid spec status values.
func ValidStatuses() []Status {
	return []Status{
		StatusVague, StatusDrafted, StatusValidated, StatusTesting,
		StatusComplete, StatusStale, StatusArchived,
	}
}

// ValidEfforts returns all valid spec effort values.
func ValidEfforts() []Effort {
	return []Effort{EffortSmall, EffortMedium, EffortLarge, EffortXLarge}
}
