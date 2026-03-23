package set

import (
	"slices"
	"testing"
)

func TestNew_Empty(t *testing.T) {
	s := New[int]()
	if s.Len() != 0 {
		t.Fatalf("expected empty set, got len %d", s.Len())
	}
}

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

func TestFrom(t *testing.T) {
	s := From([]string{"a", "b", "a"})
	if s.Len() != 2 {
		t.Fatalf("expected 2, got %d", s.Len())
	}
}

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

func TestHas_Miss(t *testing.T) {
	s := New[int]()
	if s.Has(42) {
		t.Fatal("expected miss on empty set")
	}
}

func TestItems(t *testing.T) {
	s := New(3, 1, 2)
	items := s.Items()
	slices.Sort(items)
	if len(items) != 3 || items[0] != 1 || items[1] != 2 || items[2] != 3 {
		t.Fatalf("unexpected items: %v", items)
	}
}

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
