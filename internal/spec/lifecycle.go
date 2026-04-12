package spec

import (
	"changkun.de/x/wallfacer/internal/pkg/statemachine"
)

// StatusMachine is the spec lifecycle state machine.
var StatusMachine = statemachine.New(map[Status][]Status{
	StatusVague:     {StatusDrafted},
	StatusDrafted:   {StatusValidated, StatusStale, StatusArchived},
	StatusValidated: {StatusComplete, StatusStale},
	StatusComplete:  {StatusStale, StatusArchived},
	StatusStale:     {StatusDrafted, StatusValidated, StatusArchived},
	StatusArchived:  {StatusDrafted},
})

// ValidStatuses returns all valid spec status values.
func ValidStatuses() []Status {
	return []Status{StatusVague, StatusDrafted, StatusValidated, StatusComplete, StatusStale, StatusArchived}
}

// ValidEfforts returns all valid spec effort values.
func ValidEfforts() []Effort {
	return []Effort{EffortSmall, EffortMedium, EffortLarge, EffortXLarge}
}
