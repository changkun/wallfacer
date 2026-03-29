package spec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestSpec(path string) *Spec {
	return &Spec{
		Title:   "Test Spec",
		Status:  StatusValidated,
		Track:   TrackLocal,
		Effort:  EffortSmall,
		Created: Date{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		Updated: Date{time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
		Author:  "test",
		Path:    path,
		Body:    "# Test\n\nSome content.",
	}
}

func hasRule(results []Result, rule string, severity Severity) bool {
	for _, r := range results {
		if r.Rule == rule && r.Severity == severity {
			return true
		}
	}
	return false
}

func countRule(results []Result, rule string) int {
	n := 0
	for _, r := range results {
		if r.Rule == rule {
			n++
		}
	}
	return n
}

func TestValidateSpec_Valid(t *testing.T) {
	s := newTestSpec("local/test.md")
	results := ValidateSpec(s, "", true)
	if len(results) != 0 {
		t.Errorf("expected no results, got %d: %v", len(results), results)
	}
}

func TestValidateSpec_MissingTitle(t *testing.T) {
	s := newTestSpec("local/test.md")
	s.Title = ""
	results := ValidateSpec(s, "", true)
	if !hasRule(results, "required-fields", SeverityError) {
		t.Error("expected required-fields error for missing title")
	}
}

func TestValidateSpec_MissingMultipleFields(t *testing.T) {
	s := newTestSpec("local/test.md")
	s.Title = ""
	s.Author = ""
	s.Effort = ""
	results := ValidateSpec(s, "", true)
	n := countRule(results, "required-fields")
	if n != 3 {
		t.Errorf("expected 3 required-fields errors, got %d", n)
	}
}

func TestValidateSpec_InvalidStatus(t *testing.T) {
	s := newTestSpec("local/test.md")
	s.Status = "bogus"
	results := ValidateSpec(s, "", true)
	if !hasRule(results, "valid-status", SeverityError) {
		t.Error("expected valid-status error")
	}
}

func TestValidateSpec_InvalidTrack(t *testing.T) {
	s := newTestSpec("local/test.md")
	s.Track = "bogus"
	results := ValidateSpec(s, "", true)
	if !hasRule(results, "valid-track", SeverityError) {
		t.Error("expected valid-track error")
	}
}

func TestValidateSpec_InvalidEffort(t *testing.T) {
	s := newTestSpec("local/test.md")
	s.Effort = "bogus"
	results := ValidateSpec(s, "", true)
	if !hasRule(results, "valid-effort", SeverityError) {
		t.Error("expected valid-effort error")
	}
}

func TestValidateSpec_TrackMismatch(t *testing.T) {
	s := newTestSpec("foundations/test.md")
	s.Track = TrackLocal
	results := ValidateSpec(s, "", true)
	if !hasRule(results, "track-matches-path", SeverityError) {
		t.Error("expected track-matches-path error")
	}
}

func TestValidateSpec_TrackMatchesPath(t *testing.T) {
	s := newTestSpec("local/test.md")
	s.Track = TrackLocal
	results := ValidateSpec(s, "", true)
	if hasRule(results, "track-matches-path", SeverityError) {
		t.Error("unexpected track-matches-path error")
	}
}

func TestValidateSpec_DateOrdering(t *testing.T) {
	s := newTestSpec("local/test.md")
	s.Updated = Date{time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)}
	results := ValidateSpec(s, "", true)
	if !hasRule(results, "date-ordering", SeverityError) {
		t.Error("expected date-ordering error")
	}
}

func TestValidateSpec_SelfDependency(t *testing.T) {
	s := newTestSpec("local/test.md")
	s.DependsOn = []string{"local/test.md"}
	results := ValidateSpec(s, "", true)
	if !hasRule(results, "no-self-dependency", SeverityError) {
		t.Error("expected no-self-dependency error")
	}
}

func TestValidateSpec_NonLeafWithDispatch(t *testing.T) {
	s := newTestSpec("local/test.md")
	id := "550e8400-e29b-41d4-a716-446655440000"
	s.DispatchedTaskID = &id
	results := ValidateSpec(s, "", false) // non-leaf
	if !hasRule(results, "dispatch-consistency", SeverityError) {
		t.Error("expected dispatch-consistency error for non-leaf with dispatch ID")
	}
}

func TestValidateSpec_LeafWithDispatch(t *testing.T) {
	s := newTestSpec("local/test.md")
	id := "550e8400-e29b-41d4-a716-446655440000"
	s.DispatchedTaskID = &id
	results := ValidateSpec(s, "", true) // leaf
	if hasRule(results, "dispatch-consistency", SeverityError) {
		t.Error("leaf spec with dispatch ID should not trigger error")
	}
}

func TestValidateSpec_DependsOnMissing(t *testing.T) {
	repoRoot := t.TempDir()
	s := newTestSpec("local/test.md")
	s.DependsOn = []string{"specs/nonexistent.md"}
	results := ValidateSpec(s, repoRoot, true)
	if !hasRule(results, "depends-on-exist", SeverityError) {
		t.Error("expected depends-on-exist error")
	}
}

func TestValidateSpec_DependsOnExists(t *testing.T) {
	repoRoot := t.TempDir()
	depPath := filepath.Join(repoRoot, "specs", "dep.md")
	if err := os.MkdirAll(filepath.Dir(depPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(depPath, []byte("---\ntitle: Dep\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s := newTestSpec("local/test.md")
	s.DependsOn = []string{"specs/dep.md"}
	results := ValidateSpec(s, repoRoot, true)
	if hasRule(results, "depends-on-exist", SeverityError) {
		t.Error("existing dependency should not trigger error")
	}
}

func TestValidateSpec_AffectsMissing(t *testing.T) {
	repoRoot := t.TempDir()
	s := newTestSpec("local/test.md")
	s.Affects = []string{"internal/nonexistent/"}
	results := ValidateSpec(s, repoRoot, true)
	if !hasRule(results, "affects-exist", SeverityWarning) {
		t.Error("expected affects-exist warning")
	}
	// Should be warning, not error.
	if hasRule(results, "affects-exist", SeverityError) {
		t.Error("affects-exist should be warning, not error")
	}
}

func TestValidateSpec_EmptyBodyWarning(t *testing.T) {
	s := newTestSpec("local/test.md")
	s.Status = StatusDrafted
	s.Body = ""
	results := ValidateSpec(s, "", true)
	if !hasRule(results, "body-not-empty", SeverityWarning) {
		t.Error("expected body-not-empty warning for drafted spec")
	}
}

func TestValidateSpec_VagueEmptyBody(t *testing.T) {
	s := newTestSpec("local/test.md")
	s.Status = StatusVague
	s.Body = ""
	results := ValidateSpec(s, "", true)
	if hasRule(results, "body-not-empty", SeverityWarning) {
		t.Error("vague spec with empty body should not trigger warning")
	}
}

func TestValidateSpec_AllRulesRun(t *testing.T) {
	s := &Spec{
		Title:   "",
		Status:  "invalid",
		Track:   "invalid",
		Effort:  "invalid",
		Author:  "",
		Path:    "local/test.md",
		Body:    "",
	}
	results := ValidateSpec(s, "", true)

	// Should have at least: required-fields (title, effort skipped since invalid,
	// created, updated, author), valid-status, valid-track, valid-effort.
	rules := map[string]bool{}
	for _, r := range results {
		rules[r.Rule] = true
	}
	expected := []string{"required-fields", "valid-status", "valid-track", "valid-effort"}
	for _, rule := range expected {
		if !rules[rule] {
			t.Errorf("expected rule %q to fire", rule)
		}
	}
	if len(results) < 4 {
		t.Errorf("expected at least 4 results, got %d", len(results))
	}
}
