package store

import (
	"context"
	"strconv"
	"sync"
	"testing"
)

// TestRebuildSearchIndex_RaceWithMutations runs RebuildSearchIndex concurrently
// with title/tag mutations on the same task. Before the fix, buildIndexEntry
// read task.Title/Prompt/Tags outside s.mu while mutateTask rewrote them in
// place under the write lock; run with -race this reported a data race.
func TestRebuildSearchIndex_RaceWithMutations(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	t.Cleanup(s.Close)

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "initial", Timeout: 30})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	var wg sync.WaitGroup
	const iters = 300

	wg.Go(func() {
		for i := range iters {
			_ = s.UpdateTaskTitle(ctx, task.ID, "title-"+strconv.Itoa(i))
			_ = s.UpdateTaskTags(ctx, task.ID, []string{"tag-" + strconv.Itoa(i)})
		}
	})
	wg.Go(func() {
		for range iters {
			if _, err := s.RebuildSearchIndex(ctx); err != nil {
				t.Errorf("RebuildSearchIndex: %v", err)
				return
			}
		}
	})
	wg.Wait()
}
