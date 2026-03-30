package statemachine

import (
	"errors"
	"testing"
)

func TestValidate_Allowed(t *testing.T) {
	m := New(map[string][]string{
		"a": {"b", "c"},
		"b": {"c"},
		"c": {},
	})
	if err := m.Validate("a", "b"); err != nil {
		t.Errorf("a→b should be allowed: %v", err)
	}
	if err := m.Validate("a", "c"); err != nil {
		t.Errorf("a→c should be allowed: %v", err)
	}
}

func TestValidate_Disallowed(t *testing.T) {
	m := New(map[string][]string{
		"a": {"b"},
		"b": {},
	})
	err := m.Validate("b", "a")
	if err == nil {
		t.Fatal("b→a should not be allowed")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("error should wrap ErrInvalidTransition: %v", err)
	}
}

func TestValidate_SameState(t *testing.T) {
	m := New(map[string][]string{
		"a": {"b"},
	})
	if err := m.Validate("a", "a"); err == nil {
		t.Error("a→a should not be allowed")
	}
}

func TestCanTransition(t *testing.T) {
	m := New(map[string][]string{
		"x": {"y"},
	})
	if !m.CanTransition("x", "y") {
		t.Error("x→y should be allowed")
	}
	if m.CanTransition("y", "x") {
		t.Error("y→x should not be allowed")
	}
}

func TestAllowed(t *testing.T) {
	m := New(map[string][]string{
		"a": {"b", "c"},
		"b": {},
	})
	got := m.Allowed("a")
	if len(got) != 2 {
		t.Errorf("Allowed(a) = %v, want 2 entries", got)
	}
	got = m.Allowed("b")
	if len(got) != 0 {
		t.Errorf("Allowed(b) = %v, want empty", got)
	}
}

func TestAllowed_Unknown(t *testing.T) {
	m := New(map[string][]string{})
	if got := m.Allowed("unknown"); got != nil {
		t.Errorf("Allowed(unknown) = %v, want nil", got)
	}
}

func TestIntStates(t *testing.T) {
	m := New(map[int][]int{
		0: {1, 2},
		1: {2},
	})
	if err := m.Validate(0, 1); err != nil {
		t.Errorf("0→1 should be allowed: %v", err)
	}
	if err := m.Validate(1, 0); err == nil {
		t.Error("1→0 should not be allowed")
	}
}
