package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/workspace"
)

// TestHandler_StoreFieldRaceWithApplySnapshot drives a handler endpoint that
// reads the active store concurrently with applySnapshot swapping the snapshot
// store, mirroring a workspace-group switch racing in-flight requests. Run with
// -race: before handlers were routed through the lock-guarded accessors, they
// read the h.store field directly and raced applySnapshot's write.
//
// workspace is left nil so currentStore falls back to the snapshotMu-guarded
// h.store field (the field under test); in production NewHandler installs a
// static manager, but the field write in applySnapshot still happens on every
// switch.
func TestHandler_StoreFieldRaceWithApplySnapshot(t *testing.T) {
	newStore := func() *store.Store {
		dir, err := os.MkdirTemp("", "wallfacer-race-*")
		if err != nil {
			t.Fatal(err)
		}
		s, err := store.NewFileStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		t.Cleanup(s.WaitCompaction)
		return s
	}

	s1, s2 := newStore(), newStore()
	r := runner.NewRunner(s1, runner.RunnerConfig{})
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)

	h := &Handler{
		store:     s1,
		runner:    r,
		configDir: t.TempDir(),
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: swap the snapshot store, as the workspace subscription goroutine
	// does on every group switch.
	go func() {
		defer wg.Done()
		for i := range 500 {
			snap := workspace.Snapshot{Store: s1}
			if i%2 == 1 {
				snap.Store = s2
			}
			h.applySnapshot(snap)
		}
	}()

	// Reader: a converted endpoint that reads the active store.
	go func() {
		defer wg.Done()
		for range 500 {
			req := httptest.NewRequest(http.MethodGet, "/api/debug/runtime", nil)
			h.GetRuntimeStatus(httptest.NewRecorder(), req)
		}
	}()

	wg.Wait()
}
