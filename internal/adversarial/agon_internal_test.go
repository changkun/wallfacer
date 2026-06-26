package adversarial

import (
	"context"
	"testing"
	"time"

	"latere.ai/x/agon/pkg/adversarial"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/runner"
)

// TestHarnessCritic_RunsInCwd proves the critic is invoked in the task
// worktree (so it can read the codebase) and on the harness it was built with.
func TestHarnessCritic_RunsInCwd(t *testing.T) {
	var gotCwd string
	var gotSb harness.ID
	mock := &runner.MockRunner{
		RunCriticRoundFn: func(_ context.Context, _ string, sb harness.ID, cwd string, _ time.Duration) (string, error) {
			gotCwd, gotSb = cwd, sb
			return "## attack\nfound a bug", nil
		},
	}
	c := NewHarnessCritic(mock, harness.Codex)
	res, err := c.Round(context.Background(), adversarial.CriticInput{
		Cwd:       "/wt/task/repo",
		DiffPatch: "+x := 1",
	})
	if err != nil {
		t.Fatalf("Round: %v", err)
	}
	if gotCwd != "/wt/task/repo" {
		t.Errorf("critic cwd = %q, want /wt/task/repo", gotCwd)
	}
	if gotSb != harness.Codex {
		t.Errorf("critic harness = %q, want codex", gotSb)
	}
	if res == nil || res.Markdown == "" {
		t.Error("expected critic markdown in the result")
	}
}

// TestCriticHarnessForFork_Rotates proves forks get diverse harnesses rather
// than the same model sampled twice.
func TestCriticHarnessForFork_Rotates(t *testing.T) {
	v := &AgonVerifier{criticHarnesses: []harness.ID{harness.Claude, harness.Codex}}
	want := map[int]harness.ID{1: harness.Claude, 2: harness.Codex, 3: harness.Claude, 4: harness.Codex}
	for fork, w := range want {
		if got := v.criticHarnessForFork(fork); got != w {
			t.Errorf("fork %d -> %q, want %q", fork, got, w)
		}
	}
	// Empty rotation falls back to Claude rather than panicking on modulo-by-zero.
	if got := (&AgonVerifier{}).criticHarnessForFork(1); got != harness.Claude {
		t.Errorf("empty rotation fork 1 -> %q, want claude", got)
	}
}

// TestNewAgonVerifier_DefaultsToClaude proves the variadic constructor degrades
// to a single Claude critic when no harnesses are supplied.
func TestNewAgonVerifier_DefaultsToClaude(t *testing.T) {
	v, ok := NewAgonVerifier(&runner.MockRunner{}).(*AgonVerifier)
	if !ok {
		t.Fatal("NewAgonVerifier did not return *AgonVerifier")
	}
	if got := v.criticHarnessForFork(2); got != harness.Claude {
		t.Errorf("default rotation -> %q, want claude", got)
	}
}
