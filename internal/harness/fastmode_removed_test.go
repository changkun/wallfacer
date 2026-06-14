package harness

import (
	"strings"
	"testing"
)

// The fast-mode profile (claude `/fast`, codex model_reasoning_effort=low) and
// its WALLFACER_SANDBOX_FAST knob were removed. These guards fail if either
// argv hint ever reappears, regardless of which Request fields are set.

func TestClaude_BuildArgv_NeverInjectsFast(t *testing.T) {
	reqs := []Request{
		{Prompt: "p"},
		{Prompt: "p", Model: "sonnet", SessionID: "s", SystemPrompt: "sys"},
	}
	for _, req := range reqs {
		argv, _, _ := claudeHarness{}.BuildArgv(req)
		if joined := strings.Join(argv, " "); strings.Contains(joined, "/fast") {
			t.Errorf("claude argv must not contain /fast: %v", argv)
		}
	}
}

func TestCodex_BuildArgv_NeverSetsReasoningEffort(t *testing.T) {
	reqs := []Request{
		{Prompt: "p"},
		{Prompt: "p", Model: "gpt-5", SystemPrompt: "sys"},
	}
	for _, req := range reqs {
		argv, _, _ := codexHarness{}.BuildArgv(req)
		if joined := strings.Join(argv, " "); strings.Contains(joined, "reasoning_effort") {
			t.Errorf("codex argv must not set model_reasoning_effort: %v", argv)
		}
	}
}
