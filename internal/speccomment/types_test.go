package speccomment

import "testing"

func TestNewIDUniqueAndSortable(t *testing.T) {
	const n = 1000
	ids := make([]string, n)
	seen := make(map[string]bool, n)
	for i := range ids {
		ids[i] = NewID()
		if ids[i] == "" {
			t.Fatal("NewID returned empty")
		}
		if seen[ids[i]] {
			t.Fatalf("NewID collision at %d: %q", i, ids[i])
		}
		seen[ids[i]] = true
	}
	// ULIDs minted in sequence are lexicographically non-decreasing (the
	// export-friendly sortability the git-export path depends on).
	for i := 1; i < n; i++ {
		if ids[i] < ids[i-1] {
			t.Fatalf("ids not sortable: ids[%d]=%q < ids[%d]=%q", i, ids[i], i-1, ids[i-1])
		}
	}
}

func TestThreadRoot(t *testing.T) {
	tr := Thread{Comments: []Comment{
		{ID: "c1", ParentID: "c0", Body: "reply"},
		{ID: "c0", Body: "root"},
	}}
	root, ok := tr.Root()
	if !ok || root.ID != "c0" {
		t.Fatalf("Root() = %+v, %v; want the c0 comment", root, ok)
	}

	if _, ok := (Thread{}).Root(); ok {
		t.Fatal("empty thread should have no root")
	}
}
