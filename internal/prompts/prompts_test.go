package prompts_test

import (
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/prompts"
)

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
