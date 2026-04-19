package planner

import (
	"strings"
	"testing"
)

func TestCommandRegistry_Commands(t *testing.T) {
	r := NewCommandRegistry()
	cmds := r.Commands()

	if len(cmds) != 11 {
		t.Fatalf("len(Commands) = %d, want 11", len(cmds))
	}

	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
		if c.Description == "" {
			t.Errorf("command %q has empty description", c.Name)
		}
		if c.Usage == "" {
			t.Errorf("command %q has empty usage", c.Name)
		}
	}

	for _, want := range []string{
		"summarize", "create", "validate", "impact", "status",
		"break-down", "review-breakdown", "dispatch", "review-impl", "diff", "wrapup",
	} {
		if !names[want] {
			t.Errorf("missing command %q", want)
		}
	}
}

func TestCommandRegistry_Expand_SlashCommand(t *testing.T) {
	r := NewCommandRegistry()

	expanded, ok := r.Expand("/summarize 100", "specs/local/foo.md")
	if !ok {
		t.Fatal("expected slash command to be recognized")
	}
	if !strings.Contains(expanded, "specs/local/foo.md") {
		t.Errorf("expanded prompt missing focused spec path: %q", expanded)
	}
	if !strings.Contains(expanded, "100") {
		t.Errorf("expanded prompt missing word limit: %q", expanded)
	}
}

func TestCommandRegistry_Expand_DefaultWordLimit(t *testing.T) {
	r := NewCommandRegistry()

	expanded, ok := r.Expand("/summarize", "specs/local/foo.md")
	if !ok {
		t.Fatal("expected slash command to be recognized")
	}
	if !strings.Contains(expanded, "200") {
		t.Errorf("expected default word limit 200 in: %q", expanded)
	}
}

func TestCommandRegistry_Expand_NotSlashCommand(t *testing.T) {
	r := NewCommandRegistry()

	result, ok := r.Expand("hello world", "specs/foo.md")
	if ok {
		t.Error("expected ok=false for non-slash input")
	}
	if result != "hello world" {
		t.Errorf("expected original input returned, got %q", result)
	}
}

func TestCommandRegistry_Expand_UnknownCommand(t *testing.T) {
	r := NewCommandRegistry()

	result, ok := r.Expand("/unknown arg", "specs/foo.md")
	if ok {
		t.Error("expected ok=false for unknown command")
	}
	if result != "/unknown arg" {
		t.Errorf("expected original input returned, got %q", result)
	}
}

func TestCommandRegistry_Expand_BreakDown(t *testing.T) {
	r := NewCommandRegistry()

	expanded, ok := r.Expand("/break-down", "specs/local/bar.md")
	if !ok {
		t.Fatal("expected break-down to be recognized")
	}
	if !strings.Contains(expanded, "specs/local/bar.md") {
		t.Errorf("expanded prompt missing focused spec: %q", expanded)
	}
}

func TestCommandRegistry_Expand_Create(t *testing.T) {
	r := NewCommandRegistry()

	expanded, ok := r.Expand("/create My New Spec", "")
	if !ok {
		t.Fatal("expected create to be recognized")
	}
	if !strings.Contains(expanded, "My New Spec") {
		t.Errorf("expanded prompt missing title: %q", expanded)
	}
}

// TestSlashCreate_ExpandsToDirective covers the create-command-expansion
// contract: `/create <title>` produces a prompt whose first line is a
// server-readable /spec-new directive pointing at the slugged filename.
func TestSlashCreate_ExpandsToDirective(t *testing.T) {
	r := NewCommandRegistry()

	expanded, ok := r.Expand("/create Auth Refactor", "")
	if !ok {
		t.Fatal("expected create to be recognized")
	}
	lines := strings.SplitN(expanded, "\n", 2)
	if len(lines) == 0 {
		t.Fatalf("expanded prompt is empty: %q", expanded)
	}
	first := strings.TrimSpace(lines[0])
	want := `/spec-new specs/local/auth-refactor.md title="Auth Refactor"`
	if first != want {
		t.Errorf("first line = %q, want %q", first, want)
	}
	if !strings.Contains(expanded, "Write a first-draft body") {
		t.Errorf("expected instruction body, got: %q", expanded)
	}
}

func TestSlugify_BasicCases(t *testing.T) {
	cases := map[string]string{
		"Auth Refactor":            "auth-refactor",
		"Hello, World!":            "hello-world",
		"  Leading and trailing  ": "leading-and-trailing",
		"Multiple   spaces":        "multiple-spaces",
		"UPPER case":               "upper-case",
		"snake_case_too":           "snake-case-too",
		"numbers 123 and 456":      "numbers-123-and-456",
		"!!! only punctuation ???": "only-punctuation",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Slugify(""); got != "" {
		t.Errorf("Slugify(\"\") = %q, want empty", got)
	}
	if got := Slugify("!!!???"); got != "" {
		t.Errorf("Slugify(\"!!!???\") = %q, want empty", got)
	}
}

func TestSlugify_LengthCapped(t *testing.T) {
	// 14 words × avg 5 chars + dashes > 48, so the slug must trim
	// cleanly at a word boundary without exceeding the cap.
	long := "one two three four five six seven eight nine ten eleven twelve thirteen fourteen"
	got := Slugify(long)
	if len(got) > slugMaxLen {
		t.Errorf("len(%q) = %d, want <= %d", got, len(got), slugMaxLen)
	}
	// The cut lands on a word boundary: no trailing `-`.
	if strings.HasSuffix(got, "-") {
		t.Errorf("slug ends with trailing dash: %q", got)
	}
	// And the prefix is preserved (it still starts with the first word).
	if !strings.HasPrefix(got, "one-two-three") {
		t.Errorf("unexpected prefix: %q", got)
	}

	// A single massively long word hard-cuts at the cap.
	single := strings.Repeat("x", 100)
	out := Slugify(single)
	if len(out) != slugMaxLen {
		t.Errorf("hard-cut len = %d, want %d", len(out), slugMaxLen)
	}
}

func TestCommandRegistry_Expand_Status(t *testing.T) {
	r := NewCommandRegistry()

	expanded, ok := r.Expand("/status validated", "specs/local/foo.md")
	if !ok {
		t.Fatal("expected status to be recognized")
	}
	if !strings.Contains(expanded, "validated") {
		t.Errorf("expanded prompt missing state: %q", expanded)
	}
}
