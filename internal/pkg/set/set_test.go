package set

import "testing"

// TestNew_Empty verifies that New with no arguments produces an empty set.
func TestNew_Empty(t *testing.T) {
	s := New[int]()
	if s.Len() != 0 {
		t.Fatalf("expected empty set, got len %d", s.Len())
	}
}

// TestNew_WithItems verifies that New deduplicates its arguments.
func TestNew_WithItems(t *testing.T) {
	s := New(1, 2, 3, 2, 1)
	if s.Len() != 3 {
		t.Fatalf("expected 3 unique items, got %d", s.Len())
	}
	for _, v := range []int{1, 2, 3} {
		if !s.Has(v) {
			t.Errorf("expected set to contain %d", v)
		}
	}
}

// TestAdd verifies that Add is idempotent (adding the same item twice does not increase Len).
func TestAdd(t *testing.T) {
	s := New[string]()
	s.Add("x")
	s.Add("x")
	if s.Len() != 1 {
		t.Fatalf("expected 1, got %d", s.Len())
	}
	if !s.Has("x") {
		t.Fatal("expected Has(x) == true")
	}
}

// TestHas_Miss verifies that Has returns false for an element not in the set.
func TestHas_Miss(t *testing.T) {
	s := New[int]()
	if s.Has(42) {
		t.Fatal("expected miss on empty set")
	}
}
