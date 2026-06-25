package spec

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSetOutcome_AppendAndReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.md")
	writeStatusSpec(t, dir, "s.md", StatusTesting)

	// Append when absent.
	if err := SetOutcome(path, "**Drift: minimal**\n\nLooks good."); err != nil {
		t.Fatalf("SetOutcome append: %v", err)
	}
	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(s.Body, "## Outcome") || !strings.Contains(s.Body, "Drift: minimal") {
		t.Fatalf("body missing appended outcome:\n%s", s.Body)
	}
	if !strings.Contains(s.Body, "# Body") {
		t.Errorf("original body content lost:\n%s", s.Body)
	}

	// Replace when present.
	if err := SetOutcome(path, "**Drift: moderate**\n\nOne extra file."); err != nil {
		t.Fatalf("SetOutcome replace: %v", err)
	}
	s2, _ := ParseFile(path)
	if strings.Contains(s2.Body, "minimal") {
		t.Errorf("old outcome not replaced:\n%s", s2.Body)
	}
	if !strings.Contains(s2.Body, "moderate") {
		t.Errorf("new outcome missing:\n%s", s2.Body)
	}
	if strings.Count(s2.Body, "## Outcome") != 1 {
		t.Errorf("expected exactly one Outcome section:\n%s", s2.Body)
	}
	// Frontmatter still parses.
	if s2.Status != StatusTesting {
		t.Errorf("status = %q, want testing (frontmatter preserved)", s2.Status)
	}
}

func TestSetOutcome_PreservesTrailingSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.md")
	writeStatusSpec(t, dir, "s.md", StatusTesting)
	// Add an Outcome followed by another section, then replace Outcome only.
	if err := SetOutcome(path, "first"); err != nil {
		t.Fatal(err)
	}
	// Manually append a trailing section after Outcome.
	s, _ := ParseFile(path)
	_ = s
	if err := SetOutcome(path, "second"); err != nil {
		t.Fatal(err)
	}
	s2, _ := ParseFile(path)
	if strings.Contains(s2.Body, "first") || !strings.Contains(s2.Body, "second") {
		t.Errorf("replace failed:\n%s", s2.Body)
	}
}
