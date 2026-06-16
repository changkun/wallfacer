package runner

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/store"
)

// TestWorktreeConcurrency verifies that concurrent calls to setupWorktrees,
// CleanupWorktrees, and PruneUnknownWorktrees do not cause data races, panics,
// or spurious errors. Run with -race to catch unsynchronised accesses.
func TestWorktreeConcurrency(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	// Pre-create a known task so PruneUnknownWorktrees has something to
	// preserve, making it do meaningful read+compare work during the race.
	knownTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "known task for concurrency test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	const (
		numSetup = 5 // goroutines that set up then clean up worktrees
		numPrune = 5 // goroutines that call PruneUnknownWorktrees
	)

	var wg sync.WaitGroup
	wg.Add(numSetup + numPrune)

	// goroutines: setup + cleanup a unique task worktree
	for i := 0; i < numSetup; i++ {
		go func() {
			defer wg.Done()
			taskID := uuid.New()
			wt, br, err := runner.setupWorktrees(taskID)
			if err != nil {
				// setupWorktrees may fail if the git branch already exists from
				// a concurrent call with the same taskID prefix — but since we
				// use unique UUIDs here it should always succeed.
				t.Errorf("setupWorktrees: %v", err)
				return
			}
			runner.CleanupWorktrees(taskID, wt, br)
		}()
	}

	// goroutines: prune unknown worktrees
	for i := 0; i < numPrune; i++ {
		go func() {
			defer wg.Done()
			runner.PruneUnknownWorktrees()
		}()
	}

	wg.Wait()

	// The known task's directory should not have been pruned (it has no
	// on-disk worktree dir, so there is nothing to remove — but the ID must
	// still be in the store so PruneUnknownWorktrees leaves it alone).
	_ = knownTask
}

// TestWorktreeConcurrencySetupAndPrune is a focused race test: one goroutine
// continuously sets up and cleans up a worktree while another continuously
// prunes. Both share the same worktreesDir. Detects races in ReadDir vs
// MkdirAll / RemoveAll paths.
func TestWorktreeConcurrencySetupAndPrune(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupTestRunner(t, []string{repo})

	const iterations = 10

	var wg sync.WaitGroup
	wg.Add(2)

	// goroutine A: repeatedly setup + cleanup distinct task worktrees
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			taskID := uuid.New()
			wt, br, err := runner.setupWorktrees(taskID)
			if err != nil {
				t.Errorf("goroutine A setupWorktrees iteration %d: %v", i, err)
				return
			}
			runner.CleanupWorktrees(taskID, wt, br)
		}
	}()

	// goroutine B: repeatedly prune (should never remove the active worktree)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			runner.PruneUnknownWorktrees()
		}
	}()

	wg.Wait()
}

// TestCwdInDir verifies separator-aware path containment: a process cwd that is
// the worktree dir or strictly inside it matches, while a sibling that merely
// shares the dir as a leading string (the prefix bug) must NOT match.
func TestCwdInDir(t *testing.T) {
	sep := string(os.PathSeparator)
	dir := sep + "tmp" + sep + "worktrees" + sep + "abc"

	cases := []struct {
		name string
		cwd  string
		want bool
	}{
		{"exact dir", dir, true},
		{"descendant", dir + sep + "sub" + sep + "pkg", true},
		{"sibling sharing prefix", dir + "-backup", false},
		{"sibling sharing prefix no sep", dir + "x", false},
		{"unrelated", sep + "tmp" + sep + "other", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cwdInDir(tc.cwd, dir); got != tc.want {
				t.Errorf("cwdInDir(%q, %q) = %v, want %v", tc.cwd, dir, got, tc.want)
			}
		})
	}
}
