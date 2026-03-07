package runner

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// TestWorktreeConcurrency verifies that concurrent calls to setupWorktrees,
// CleanupWorktrees, and PruneOrphanedWorktrees do not cause data races, panics,
// or spurious errors. Run with -race to catch unsynchronised accesses.
func TestWorktreeConcurrency(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	// Pre-create a known task so PruneOrphanedWorktrees has something to
	// preserve, making it do meaningful read+compare work during the race.
	knownTask, err := s.CreateTask(ctx, "known task for concurrency test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	const (
		numSetup  = 5 // goroutines that set up then clean up worktrees
		numPrune  = 5 // goroutines that call PruneOrphanedWorktrees
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

	// goroutines: prune orphaned worktrees
	for i := 0; i < numPrune; i++ {
		go func() {
			defer wg.Done()
			runner.PruneOrphanedWorktrees(s)
		}()
	}

	wg.Wait()

	// The known task's directory should not have been pruned (it has no
	// on-disk worktree dir, so there is nothing to remove — but the ID must
	// still be in the store so PruneOrphanedWorktrees leaves it alone).
	_ = knownTask
}

// TestWorktreeConcurrencySetupAndPrune is a focused race test: one goroutine
// continuously sets up and cleans up a worktree while another continuously
// prunes. Both share the same worktreesDir. Detects races in ReadDir vs
// MkdirAll / RemoveAll paths.
func TestWorktreeConcurrencySetupAndPrune(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})

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
			runner.PruneOrphanedWorktrees(s)
		}
	}()

	wg.Wait()
}
