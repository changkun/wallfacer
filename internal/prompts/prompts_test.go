package prompts_test

import (
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/prompts"
	"latere.ai/x/wallfacer/internal/spec"
)

// TestTitle_RendersEmbedded verifies that the title template renders the task
// prompt with no template syntax left over.
func TestTitle_RendersEmbedded(t *testing.T) {
	got := prompts.Title("my task prompt")
	if strings.TrimSpace(got) == "" {
		t.Error("Title returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("Title returned unreplaced template syntax: %q", got)
	}
}

// TestOversight_ReturnsNonEmptyRendered verifies that the oversight template
// renders with a sample activity log.
func TestOversight_ReturnsNonEmptyRendered(t *testing.T) {
	got := prompts.Oversight("Phase 1: analyzed codebase. Phase 2: implemented feature.")
	if strings.TrimSpace(got) == "" {
		t.Error("Oversight returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("Oversight returned unreplaced template syntax: %q", got)
	}
}

// TestCommitMessage_ReturnsNonEmptyRendered verifies that the commit message
// template renders with prompt, diff stat, and recent log populated.
func TestCommitMessage_ReturnsNonEmptyRendered(t *testing.T) {
	data := prompts.CommitData{
		Prompt:    "add user authentication",
		DiffStat:  "3 files changed, 100 insertions(+), 2 deletions(-)",
		RecentLog: "abc1234 previous commit message",
	}
	got := prompts.CommitMessage(data)
	if strings.TrimSpace(got) == "" {
		t.Error("CommitMessage returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("CommitMessage returned unreplaced template syntax: %q", got)
	}
}

func TestDriftAssessment_ReturnsNonEmptyRendered(t *testing.T) {
	got := prompts.DriftAssessment(prompts.DriftData{
		SpecBody:     "# Spec\n\nImplement X.",
		Affects:      "internal/x/",
		ChangedFiles: "internal/x/foo.go",
		Diff:         "+ added a line",
	})
	if strings.TrimSpace(got) == "" {
		t.Error("DriftAssessment returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unreplaced template syntax: %q", got)
	}
	for _, want := range []string{"Implement X.", "internal/x/foo.go", "drift_level", "JSON"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered prompt missing %q", want)
		}
	}
}

// TestConflictResolution_ReturnsNonEmptyRendered verifies that the conflict
// resolution template renders with the worktree path and default branch.
func TestConflictResolution_ReturnsNonEmptyRendered(t *testing.T) {
	data := prompts.ConflictData{
		WorktreePath:  "/home/me/worktrees/task/myrepo",
		DefaultBranch: "main",
	}
	got := prompts.ConflictResolution(data)
	if strings.TrimSpace(got) == "" {
		t.Error("ConflictResolution returned empty string")
	}
	if !strings.Contains(got, data.WorktreePath) {
		t.Errorf("ConflictResolution does not name the worktree path %q: %q", data.WorktreePath, got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("ConflictResolution returned unreplaced template syntax: %q", got)
	}
}

// TestTestVerification_ReturnsNonEmptyRendered verifies that the test
// verification template renders with all optional fields (criteria, impl
// result, diff) populated.
func TestTestVerification_ReturnsNonEmptyRendered(t *testing.T) {
	data := prompts.TestData{
		OriginalPrompt: "build a widget",
		Criteria:       "widget must render in < 100ms",
		ImplResult:     "I implemented the widget",
		Diff:           "--- a/widget.go\n+++ b/widget.go\n@@ -1 +1 @@",
	}
	got := prompts.TestVerification(data)
	if strings.TrimSpace(got) == "" {
		t.Error("TestVerification returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("TestVerification returned unreplaced template syntax: %q", got)
	}
}

// TestTestVerification_NoCriteria verifies that the test verification template
// renders when only the required OriginalPrompt is set and all optional fields
// (Criteria, ImplResult, Diff) are empty.
func TestTestVerification_NoCriteria(t *testing.T) {
	got := prompts.TestVerification(prompts.TestData{OriginalPrompt: "build something"})
	if strings.TrimSpace(got) == "" {
		t.Error("TestVerification without criteria returned empty string")
	}
}

// TestRenderSpec verifies that the spec template renders without error
// and contains key phrases.
func TestRenderSpec(t *testing.T) {
	got := prompts.Spec()
	if strings.TrimSpace(got) == "" {
		t.Error("Spec returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("Spec returned unreplaced template syntax: %q", got)
	}
	for _, phrase := range []string{"spec", "planning", "specs/"} {
		if !strings.Contains(strings.ToLower(got), phrase) {
			t.Errorf("Spec output missing expected phrase %q", phrase)
		}
	}
}

// TestRenderSpec_StatusesMatchModel pins the spec prompt to the lifecycle
// the code enforces: every status the parser knows is listed, so the agent
// never learns a five-state machine while the server runs a seven-state one.
func TestRenderSpec_StatusesMatchModel(t *testing.T) {
	got := prompts.Spec()
	for _, st := range []spec.Status{
		spec.StatusVague, spec.StatusDrafted, spec.StatusValidated, spec.StatusTesting,
		spec.StatusComplete, spec.StatusStale, spec.StatusArchived,
	} {
		if !strings.Contains(got, "`"+string(st)+"`") {
			t.Errorf("spec prompt does not list status %q", st)
		}
	}
}

// TestRenderSpec_NoHardcodedTracks: track directories come and go, so the
// prompt points the agent at specs/README.md instead of naming a fixed set.
func TestRenderSpec_NoHardcodedTracks(t *testing.T) {
	got := prompts.Spec()
	if strings.Contains(got, "specs/foundations/") {
		t.Errorf("spec prompt still hardcodes a track directory:\n%s", got)
	}
	if !strings.Contains(got, "specs/README.md") {
		t.Errorf("spec prompt should point at specs/README.md for the live tracks")
	}
}

// TestTestVerification_VerdictMarkersStatedOnce: the verdict contract names
// each marker exactly once. Bare example lines duplicating the markers read
// as output the agent should reproduce, and the parser accepts the marker
// anywhere, so the single instruction is the whole contract.
func TestTestVerification_VerdictMarkersStatedOnce(t *testing.T) {
	got := prompts.TestVerification(prompts.TestData{OriginalPrompt: "build widget"})
	for _, marker := range []string{"**PASS**", "**FAIL**"} {
		if n := strings.Count(got, marker); n != 1 {
			t.Errorf("%s appears %d times, want 1", marker, n)
		}
	}
}

// TestRenderSpecSystemEmpty_ContainsDirective ensures the empty-tree
// variant actually mentions the /spec-new directive so the agent knows
// how to bootstrap a scaffold.
func TestRenderSpecSystemEmpty_ContainsDirective(t *testing.T) {
	got := prompts.SpecSystemEmpty()
	if strings.TrimSpace(got) == "" {
		t.Fatal("SpecSystemEmpty returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unreplaced template syntax: %q", got)
	}
	for _, phrase := range []string{"/spec-new", "empty", "clean slate"} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(phrase)) {
			t.Errorf("missing expected phrase %q in empty-tree prompt", phrase)
		}
	}
}

// TestRenderSpecSystemNonempty_PrefersExisting ensures the non-empty
// variant steers the agent toward editing existing specs rather than
// scaffolding new ones on every turn.
func TestRenderSpecSystemNonempty_PrefersExisting(t *testing.T) {
	got := prompts.SpecSystemNonempty()
	if strings.TrimSpace(got) == "" {
		t.Fatal("SpecSystemNonempty returned empty string")
	}
	for _, phrase := range []string{"existing", "Edit"} {
		if !strings.Contains(got, phrase) {
			t.Errorf("missing expected phrase %q in non-empty prompt", phrase)
		}
	}
}
