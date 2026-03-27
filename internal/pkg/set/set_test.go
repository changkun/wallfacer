package set

import (
	"slices"
	"testing"
)

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

// TestFrom verifies that From creates a set from a slice, deduplicating entries.
func TestFrom(t *testing.T) {
	s := From([]string{"a", "b", "a"})
	if s.Len() != 2 {
		t.Fatalf("expected 2, got %d", s.Len())
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

// TestRemove verifies that Remove deletes an element and is a no-op for absent elements.
func TestRemove(t *testing.T) {
	s := New("a", "b")
	s.Remove("a")
	if s.Has("a") {
		t.Fatal("expected a removed")
	}
	if !s.Has("b") {
		t.Fatal("expected b still present")
	}
	// Remove non-existent is no-op.
	s.Remove("z")
}

// TestHas_Miss verifies that Has returns false for an element not in the set.
func TestHas_Miss(t *testing.T) {
	s := New[int]()
	if s.Has(42) {
		t.Fatal("expected miss on empty set")
	}
}

// TestItems verifies that Items returns all elements (sorted here for stable comparison).
func TestItems(t *testing.T) {
	s := New(3, 1, 2)
	items := s.Items()
	slices.Sort(items)
	if len(items) != 3 || items[0] != 1 || items[1] != 2 || items[2] != 3 {
		t.Fatalf("unexpected items: %v", items)
	}
}

// TestAll verifies that the All iterator yields every element in the set.
func TestAll(t *testing.T) {
	s := New(3, 1, 2)
	var got []int
	for v := range s.All() {
		got = append(got, v)
	}
	slices.Sort(got)
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("unexpected All() items: %v", got)
	}
}

// TestAll_EarlyBreak verifies that breaking out of the All iterator stops iteration.
func TestAll_EarlyBreak(t *testing.T) {
	s := New(1, 2, 3, 4, 5)
	count := 0
	for range s.All() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Fatalf("expected early break after 2, got %d", count)
	}
}

// TestAll_Empty verifies that All over an empty set yields zero iterations.
func TestAll_Empty(t *testing.T) {
	s := New[string]()
	count := 0
	for range s.All() {
		count++
	}
	if count != 0 {
		t.Fatalf("expected 0 items from empty set, got %d", count)
	}
}
