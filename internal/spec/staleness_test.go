package spec

import (
	"testing"
	"time"
)

func TestScanStaleCandidates(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		// Complete spec, affects foo.go, last updated 2026-01-01.
		"local/done.md": {
			Status:  StatusComplete,
			Affects: []string{"internal/x/foo.go"},
			Updated: Date{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
		// Complete but no affects — never a candidate.
		"local/noaff.md": {Status: StatusComplete},
		// Not complete — skipped.
		"local/wip.md": {
			Status:  StatusValidated,
			Affects: []string{"internal/x/foo.go"},
			Updated: Date{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
		// Archived — skipped even though affects changed.
		"local/arch.md": {
			Status:  StatusArchived,
			Affects: []string{"internal/x/foo.go"},
			Updated: Date{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	})

	// Stub: a commit touched the paths on 2026-02-01.
	commitDate := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	changedSince := func(since time.Time, paths []string) ([]string, error) {
		if since.Before(commitDate) {
			return paths, nil
		}
		return nil, nil
	}

	got := ScanStaleCandidates(tree, changedSince)
	if len(got) != 1 || got[0].Path != "local/done.md" {
		t.Fatalf("candidates = %+v, want only local/done.md", got)
	}
	if len(got[0].Files) != 1 || got[0].Files[0] != "internal/x/foo.go" {
		t.Errorf("files = %v, want [internal/x/foo.go]", got[0].Files)
	}

	// After bumping updated past the commit date, the spec is no longer flagged.
	tree.All["local/done.md"].Value.Updated = Date{time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}
	if got := ScanStaleCandidates(tree, changedSince); len(got) != 0 {
		t.Errorf("after updated bump, candidates = %+v, want none", got)
	}
}
