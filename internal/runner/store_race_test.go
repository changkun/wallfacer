package runner

import (
	"context"
	"os"
	"sync"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
	"github.com/google/uuid"
)

// TestStoreAccessorsRaceWithSnapshot exercises the worktree/board store
// readers concurrently with applyWorkspaceSnapshot, which reassigns r.store
// under storeMu. Before the fix these readers touched r.store directly without
// the lock; run with -race this reproduced a data race. After routing them
// through r.currentStore() the race detector stays quiet.
func TestStoreAccessorsRaceWithSnapshot(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupTestRunner(t, []string{repo})

	// A second store to swap in via snapshots.
	otherDir, err := os.MkdirTemp("", "wallfacer-runner-other-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(otherDir) })
	other, err := store.NewFileStore(otherDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)

	snapA := workspace.Snapshot{Store: runner.currentStore(), Workspaces: []string{repo}, Key: "a"}
	snapB := workspace.Snapshot{Store: other, Workspaces: []string{repo}, Key: "b"}

	ctx := context.Background()
	var wg sync.WaitGroup
	const iters = 200

	wg.Go(func() {
		for i := range iters {
			if i%2 == 0 {
				runner.applyWorkspaceSnapshot(snapA)
			} else {
				runner.applyWorkspaceSnapshot(snapB)
			}
		}
	})

	wg.Go(func() {
		for range iters {
			_, _ = runner.ScanOrphanedWorktrees(ctx)
			runner.PruneUnknownWorktrees()
			_, _, _ = runner.generateBoardContextAndMounts(uuid.New(), false)
		}
	})

	wg.Wait()
	// Restore the original store so cleanup operates on the right one.
	runner.applyWorkspaceSnapshot(snapA)
}
