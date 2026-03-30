// Package statemachine provides a generic finite state machine with
// transition validation.
package statemachine

import (
	"errors"
	"fmt"
	"slices"
)

// ErrInvalidTransition is returned when a transition is not allowed.
var ErrInvalidTransition = errors.New("invalid transition")

// Machine defines allowed transitions between states.
type Machine[S comparable] struct {
	transitions map[S][]S
}

// New creates a state machine from a map of allowed transitions.
// Each key maps to the states reachable from it.
func New[S comparable](transitions map[S][]S) *Machine[S] {
	return &Machine[S]{transitions: transitions}
}

// Validate returns nil if transitioning from → to is allowed, or an error
// wrapping ErrInvalidTransition if not.
func (m *Machine[S]) Validate(from, to S) error {
	if slices.Contains(m.transitions[from], to) {
		return nil
	}
	return fmt.Errorf("%w: %v → %v", ErrInvalidTransition, from, to)
}

// CanTransition reports whether from → to is allowed.
func (m *Machine[S]) CanTransition(from, to S) bool {
	return m.Validate(from, to) == nil
}

// Allowed returns the states reachable from the given state.
// Returns nil for terminal or unknown states.
func (m *Machine[S]) Allowed(from S) []S {
	return m.transitions[from]
}
