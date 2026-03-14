package runner

import (
	"strings"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
	"changkun.de/wallfacer/prompts"
)

func TestBuildRefinementPromptIncludesTaskAgeAndValidityDecision(t *testing.T) {
	now := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	created := now.Add(-45 * 24 * time.Hour)

	task := &store.Task{
		Prompt:    "Upgrade API integration for latest model responses.",
		Status:    store.TaskStatusBacklog,
		CreatedAt: created,
	}

	r := &Runner{promptsMgr: prompts.Default}
	prompt := r.buildRefinementPrompt(task, "preserve backward compatibility", now)

	for _, want := range []string{
		"Task created: 2026-01-22",
		"Current date: 2026-03-08",
		"Task age: 45 days",
		"Backlog status at refinement start: backlog",
		"## Backlog Outcome",
		"Outcome: [KEEP | REWRITE | CLOSE]",
		"<user_instructions>\npreserve backward compatibility\n</user_instructions>",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing expected text %q\n--- prompt ---\n%s", want, prompt)
		}
	}
}

func TestBuildRefinementPromptNoUserInstructionsBlockWhenEmpty(t *testing.T) {
	now := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	task := &store.Task{
		Prompt:    "Investigate flaky stream tests.",
		Status:    store.TaskStatusBacklog,
		CreatedAt: now,
	}

	r := &Runner{promptsMgr: prompts.Default}
	prompt := r.buildRefinementPrompt(task, "   ", now)
	if strings.Contains(prompt, "<user_instructions>") {
		t.Fatalf("did not expect user instructions block for empty instructions\n--- prompt ---\n%s", prompt)
	}
}

// TestCleanRefinementResult verifies that cleanRefinementResult strips preamble
// before the first top-level heading.
func TestCleanRefinementResult_StartsWithHeading(t *testing.T) {
	input := "# My Spec\nContent here."
	got := cleanRefinementResult(input)
	if got != input {
		t.Errorf("cleanRefinementResult(%q) = %q, want unchanged", input, got)
	}
}

func TestCleanRefinementResult_HeadingAfterPreamble(t *testing.T) {
	input := "Here is some preamble.\n\n# My Spec\nContent here."
	got := cleanRefinementResult(input)
	want := "# My Spec\nContent here."
	if got != want {
		t.Errorf("cleanRefinementResult(%q) = %q, want %q", input, got, want)
	}
}

func TestCleanRefinementResult_NoHeading(t *testing.T) {
	input := "Just some text with no headings."
	got := cleanRefinementResult(input)
	if got != input {
		t.Errorf("cleanRefinementResult(%q) = %q, want unchanged", input, got)
	}
}

func TestCleanRefinementResult_Empty(t *testing.T) {
	got := cleanRefinementResult("")
	if got != "" {
		t.Errorf("cleanRefinementResult(%q) = %q, want %q", "", got, "")
	}
}
