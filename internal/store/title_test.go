package store

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDeriveTitle(t *testing.T) {
	long := "Refactor the authentication middleware so that every handler reads the session once"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"blank lines only", "\n  \n\t\n", ""},
		{"first line", "Fix the login bug\nmore detail here", "Fix the login bug"},
		{"skips leading blank lines", "\n\n   Add dark mode  \nbody", "Add dark mode"},
		{"strips heading marker", "# Spec A\n\nFoundation work.", "Spec A"},
		{"strips deeper heading marker", "### Plan the migration", "Plan the migration"},
		{"empty heading falls through", "##\nReal first line", "Real first line"},
		{"hashtag is not a heading", "#hotfix the parser", "#hotfix the parser"},
		{"seven hashes is not a heading", "####### seven", "####### seven"},
		{"collapses whitespace", "Fix   the\tparser", "Fix the parser"},
		{"exact limit kept", strings.Repeat("a", TitleMaxRunes), strings.Repeat("a", TitleMaxRunes)},
		{"long line cut at a word", long, "Refactor the authentication middleware so that every…"},
		{"single long word cut mid-word", strings.Repeat("x", 80), strings.Repeat("x", TitleMaxRunes-1) + "…"},
		{"trailing punctuation dropped before ellipsis", "Update the docs, the guide, and the README, then rebuild, the index again today", "Update the docs, the guide, and the README, then rebuild…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveTitle(tc.in)
			if got != tc.want {
				t.Errorf("DeriveTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if n := utf8.RuneCountInString(got); n > TitleMaxRunes {
				t.Errorf("DeriveTitle(%q) has %d runes, want at most %d", tc.in, n, TitleMaxRunes)
			}
		})
	}
}

func TestCreateTaskWithOptions_Title(t *testing.T) {
	s := newTestStore(t)

	derived, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "## Ship the release\n\nTag and publish.", Timeout: 5})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	if derived.Title != "Ship the release" {
		t.Errorf("derived title = %q, want %q", derived.Title, "Ship the release")
	}

	explicit, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "Tag and publish.", Title: "  Release v1  ", Timeout: 5})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	if explicit.Title != "Release v1" {
		t.Errorf("explicit title = %q, want %q", explicit.Title, "Release v1")
	}

	results, err := s.SearchTasks(bg(), "ship the release")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 || results[0].ID != derived.ID {
		t.Errorf("search by derived title found %d tasks, want the derived one", len(results))
	}
}
