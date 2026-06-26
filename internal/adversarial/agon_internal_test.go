package adversarial

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"latere.ai/x/agon/pkg/adversarial"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/runner"
)

// TestHarnessCritic_RunsInThrowawayCwd proves the critic is invoked in the
// throwaway worktree it was built with (overriding CriticInput.Cwd, the real
// tree) on the harness it was built with.
func TestHarnessCritic_RunsInThrowawayCwd(t *testing.T) {
	var gotCwd string
	var gotSb harness.ID
	mock := &runner.MockRunner{
		RunCriticRoundFn: func(_ context.Context, _ string, sb harness.ID, cwd string, _ time.Duration) (string, error) {
			gotCwd, gotSb = cwd, sb
			return "## attack\nfound a bug", nil
		},
	}
	c := NewHarnessCritic(mock, harness.Codex, "/wt/task/.agon-critic-abcd1234")
	res, err := c.Round(context.Background(), adversarial.CriticInput{
		Cwd:       "/wt/task/repo", // the real worktree — must NOT be used
		DiffPatch: "+x := 1",
	})
	if err != nil {
		t.Fatalf("Round: %v", err)
	}
	if gotCwd != "/wt/task/.agon-critic-abcd1234" {
		t.Errorf("critic cwd = %q, want the throwaway worktree (not the real tree)", gotCwd)
	}
	if gotSb != harness.Codex {
		t.Errorf("critic harness = %q, want codex", gotSb)
	}
	if res == nil || res.Markdown == "" {
		t.Error("expected critic markdown in the result")
	}
}

// TestHarnessCritic_FallsBackToInputCwd proves an empty override cwd falls back
// to CriticInput.Cwd (the patch-only / no-isolation path).
func TestHarnessCritic_FallsBackToInputCwd(t *testing.T) {
	var gotCwd string
	mock := &runner.MockRunner{
		RunCriticRoundFn: func(_ context.Context, _ string, _ harness.ID, cwd string, _ time.Duration) (string, error) {
			gotCwd = cwd
			return "ok", nil
		},
	}
	c := NewHarnessCritic(mock, harness.Claude, "")
	if _, err := c.Round(context.Background(), adversarial.CriticInput{Cwd: "/fallback"}); err != nil {
		t.Fatalf("Round: %v", err)
	}
	if gotCwd != "/fallback" {
		t.Errorf("critic cwd = %q, want /fallback", gotCwd)
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

// TestNewCriticWorktree_CreatesAndCleansUp proves the throwaway worktree is
// created at HEAD outside the source tree and fully removed by cleanup, so a
// full-permission critic never touches (or leaves a branch on) the real tree.
func TestNewCriticWorktree_CreatesAndCleansUp(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init")
	runGit("commit", "--allow-empty", "-m", "init")

	path, cleanup, err := newCriticWorktree(repo)
	if err != nil {
		t.Fatalf("newCriticWorktree: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("throwaway worktree not created: %v", err)
	}
	if strings.HasPrefix(path, repo+string(os.PathSeparator)) {
		t.Errorf("throwaway %q is inside the source worktree %q", path, repo)
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("throwaway worktree not removed by cleanup: stat err=%v", err)
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
