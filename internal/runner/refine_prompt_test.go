package runner

import (
	"strings"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
)

func TestBuildRefinementPromptIncludesTaskAgeAndValidityDecision(t *testing.T) {
	now := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	created := now.Add(-45 * 24 * time.Hour)

	task := &store.Task{
		Prompt:    "Upgrade API integration for latest model responses.",
		Status:    store.TaskStatusBacklog,
		CreatedAt: created,
	}

	prompt := buildRefinementPrompt(task, "preserve backward compatibility", now)

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

	prompt := buildRefinementPrompt(task, "   ", now)
	if strings.Contains(prompt, "<user_instructions>") {
		t.Fatalf("did not expect user instructions block for empty instructions\n--- prompt ---\n%s", prompt)
	}
}
