package runner

import (
	"context"
	"testing"
)

func TestRunRefinementContainerFallsBackToCodexOnTokenLimit(t *testing.T) {
	tokenLimit := `{"result":"rate limit exceeded: token limit reached","session_id":"s1","stop_reason":"end_turn","is_error":true,"total_cost_usd":0.001}`
	refinementOutput := `{"result":"Detailed implementation plan","session_id":"s2","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001,"usage":{"input_tokens":111,"output_tokens":22}}`
	cmd := fakeStatefulCmd(t, []string{tokenLimit, refinementOutput})
	s, r := setupRunnerWithCmd(t, nil, cmd)

	task, err := s.CreateTask(context.Background(), "Refine this task", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	output, _, _, err := r.runRefinementContainer(context.Background(), task.ID, "Refine prompt", "", "claude")
	if err != nil {
		t.Fatalf("expected codex fallback success, got error: %v", err)
	}
	if output == nil {
		t.Fatal("expected refinement output")
	}
	if output.ActualSandbox != "codex" {
		t.Fatalf("expected actual sandbox codex, got %q", output.ActualSandbox)
	}

	usages, err := s.GetTurnUsages(task.ID)
	if err != nil {
		t.Fatalf("GetTurnUsages: %v", err)
	}
	if len(usages) == 0 {
		t.Fatal("expected refinement usage record after fallback")
	}
	if usages[len(usages)-1].Sandbox != "codex" {
		t.Fatalf("expected refinement usage sandbox codex, got %q", usages[len(usages)-1].Sandbox)
	}
	if usages[len(usages)-1].SubAgent != "refinement" {
		t.Fatalf("expected refinement sub-agent, got %q", usages[len(usages)-1].SubAgent)
	}
}
