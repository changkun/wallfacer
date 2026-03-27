package pagination

import "testing"

// item is a test fixture with an ID used as cursor and a string payload.
type item struct {
	ID    int64
	Value string
}

func cursor(i item) int64 { return i.ID }

var testItems = []item{
	{1, "a"}, {2, "b"}, {3, "c"}, {4, "d"}, {5, "e"},
	{6, "f"}, {7, "g"}, {8, "h"}, {9, "i"}, {10, "j"},
}

// TestPaginate_Empty verifies that an empty input yields zero items and HasMore=false.
func TestPaginate_Empty(t *testing.T) {
	p := Paginate([]item{}, cursor, 0, 10, 100, 1000, nil)
	if len(p.Items) != 0 {
		t.Fatalf("expected empty items, got %d", len(p.Items))
	}
	if p.HasMore {
		t.Fatal("expected HasMore=false for empty input")
	}
	if p.TotalFiltered != 0 {
		t.Fatalf("expected TotalFiltered=0, got %d", p.TotalFiltered)
	}
}

// TestPaginate_NoFilter verifies basic pagination of the first page with no filter,
// checking item count, boundaries, HasMore, TotalFiltered, and NextCursor.
func TestPaginate_NoFilter(t *testing.T) {
	p := Paginate(testItems, cursor, 0, 5, 100, 1000, nil)
	if len(p.Items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(p.Items))
	}
	if p.Items[0].ID != 1 || p.Items[4].ID != 5 {
		t.Fatalf("unexpected items: %+v", p.Items)
	}
	if !p.HasMore {
		t.Fatal("expected HasMore=true")
	}
	if p.TotalFiltered != 10 {
		t.Fatalf("expected TotalFiltered=10, got %d", p.TotalFiltered)
	}
	if p.NextCursor != 5 {
		t.Fatalf("expected NextCursor=5, got %d", p.NextCursor)
	}
}

// TestPaginate_WithCursor verifies that afterCursor skips items with ID <= cursor.
func TestPaginate_WithCursor(t *testing.T) {
	p := Paginate(testItems, cursor, 5, 3, 100, 1000, nil)
	if len(p.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(p.Items))
	}
	if p.Items[0].ID != 6 {
		t.Fatalf("expected first item ID=6, got %d", p.Items[0].ID)
	}
	if !p.HasMore {
		t.Fatal("expected HasMore=true")
	}
}

// TestPaginate_WithFilter verifies that the filter predicate excludes non-matching
// items from both the result slice and the TotalFiltered count.
func TestPaginate_WithFilter(t *testing.T) {
	even := func(i item) bool { return i.ID%2 == 0 }
	p := Paginate(testItems, cursor, 0, 3, 100, 1000, even)
	if len(p.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(p.Items))
	}
	for _, it := range p.Items {
		if it.ID%2 != 0 {
			t.Fatalf("expected even ID, got %d", it.ID)
		}
	}
	if p.TotalFiltered != 5 {
		t.Fatalf("expected TotalFiltered=5, got %d", p.TotalFiltered)
	}
	if !p.HasMore {
		t.Fatal("expected HasMore=true")
	}
}

// TestPaginate_DefaultLimit verifies that limit=0 falls back to the defaultLimit.
func TestPaginate_DefaultLimit(t *testing.T) {
	p := Paginate(testItems, cursor, 0, 0, 3, 1000, nil)
	if len(p.Items) != 3 {
		t.Fatalf("expected defaultLimit=3 items, got %d", len(p.Items))
	}
}

// TestPaginate_MaxLimit verifies that a requested limit exceeding maxLimit is clamped.
func TestPaginate_MaxLimit(t *testing.T) {
	p := Paginate(testItems, cursor, 0, 999, 100, 5, nil)
	if len(p.Items) != 5 {
		t.Fatalf("expected maxLimit=5 items, got %d", len(p.Items))
	}
}

// TestPaginate_LastPage verifies that the final page has HasMore=false when all
// remaining items fit within the limit.
func TestPaginate_LastPage(t *testing.T) {
	p := Paginate(testItems, cursor, 8, 10, 100, 1000, nil)
	if len(p.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(p.Items))
	}
	if p.HasMore {
		t.Fatal("expected HasMore=false on last page")
	}
}
