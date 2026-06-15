package cli

import (
	"testing"
	"testing/fstest"
)

func testDocsFS() fstest.MapFS {
	return fstest.MapFS{
		"docs/guide/automation.md": {Data: []byte(
			"# Automation\n\nEach toggle drives one server-side watcher that promotes backlog tasks.\n")},
		"docs/guide/workspaces.md": {Data: []byte(
			"# Workspaces\n\nA workspace scopes every task to a directory on disk.\n")},
		"docs/internals/architecture.md": {Data: []byte(
			"# Architecture\n\nThe server embeds a watcher engine for automation lifecycles.\n")},
		"docs/guide/images/board.png": {Data: []byte("not markdown")},
	}
}

func TestSearchDocsTitleMatchRanksFirst(t *testing.T) {
	// "automation" matches the title of automation.md AND the body of
	// architecture.md; the title hit must come first.
	hits := searchDocs(testDocsFS(), "automation", 20)
	if len(hits) < 2 {
		t.Fatalf("want >=2 hits, got %d: %+v", len(hits), hits)
	}
	if hits[0].Slug != "guide/automation" {
		t.Errorf("title match should rank first, got slug %q", hits[0].Slug)
	}
	foundArch := false
	for _, h := range hits {
		if h.Slug == "internals/architecture" {
			foundArch = true
		}
	}
	if !foundArch {
		t.Errorf("body match internals/architecture missing from %+v", hits)
	}
}

func TestSearchDocsBodyOnlyMatch(t *testing.T) {
	// "directory" appears only in the body of workspaces.md, never a title.
	hits := searchDocs(testDocsFS(), "directory", 20)
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].Slug != "guide/workspaces" {
		t.Errorf("got slug %q, want guide/workspaces", hits[0].Slug)
	}
	if hits[0].Title != "Workspaces" {
		t.Errorf("got title %q, want Workspaces", hits[0].Title)
	}
	if hits[0].Snippet == "" {
		t.Error("body match should carry a context snippet")
	}
}

func TestSearchDocsShortQueryReturnsNil(t *testing.T) {
	if hits := searchDocs(testDocsFS(), "a", 20); hits != nil {
		t.Errorf("single-char query should return nil, got %+v", hits)
	}
	if hits := searchDocs(testDocsFS(), "   ", 20); hits != nil {
		t.Errorf("blank query should return nil, got %+v", hits)
	}
}

func TestSearchDocsCaseInsensitive(t *testing.T) {
	if hits := searchDocs(testDocsFS(), "WORKSPACE", 20); len(hits) == 0 {
		t.Error("search should be case-insensitive")
	}
}

func TestSearchDocsRespectsLimit(t *testing.T) {
	// "the" appears in several bodies; cap at 1.
	if hits := searchDocs(testDocsFS(), "the", 1); len(hits) > 1 {
		t.Errorf("limit not respected: got %d hits", len(hits))
	}
}
