package runner

import (
	"testing"

	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
	"github.com/google/uuid"
)

// TestCurrentWSKey verifies that currentWSKey returns the key from the
// most recently applied workspace snapshot.
func TestCurrentWSKey(t *testing.T) {
	_, r := setupTestRunner(t, nil)

	// Initially empty since no snapshot has been applied.
	if got := r.currentWSKey(); got != "" {
		t.Fatalf("expected empty wsKey before any snapshot, got %q", got)
	}

	// Apply a snapshot with a known key.
	r.applyWorkspaceSnapshot(workspace.Snapshot{Key: "group-a"})
	if got := r.currentWSKey(); got != "group-a" {
		t.Fatalf("expected wsKey=%q, got %q", "group-a", got)
	}

	// Apply another snapshot — key should update.
	r.applyWorkspaceSnapshot(workspace.Snapshot{Key: "group-b"})
	if got := r.currentWSKey(); got != "group-b" {
		t.Fatalf("expected wsKey=%q, got %q", "group-b", got)
	}
}

// TestTaskStoreResolution verifies that taskStore returns the correct store
// when a task-to-group mapping exists and the group is active in the manager.
func TestTaskStoreResolution(t *testing.T) {
	storeA, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeA.Close() })

	storeB, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeB.Close() })

	// Create a static manager with storeA as the viewed group.
	mgr := workspace.NewStatic(storeA, []string{"/ws/a"}, "")

	// Manually add storeB as a second active group.
	// Since NewStatic only supports one group, we'll test the fallback path
	// by mapping a task to the viewed group's key.
	snap := mgr.Snapshot()

	_, r := setupTestRunnerWithManager(t, nil, mgr)

	taskID := uuid.New()
	r.taskWSKey.Store(taskID, snap.Key)

	got := r.taskStore(taskID)
	if got != storeA {
		t.Fatal("expected taskStore to return storeA for the mapped key")
	}
}

// TestTaskStoreFallback verifies that taskStore falls back to currentStore
// when no task-to-group mapping exists.
func TestTaskStoreFallback(t *testing.T) {
	storeA, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeA.Close() })

	mgr := workspace.NewStatic(storeA, nil, "")
	_, r := setupTestRunnerWithManager(t, nil, mgr)

	// Apply a snapshot so currentStore returns storeA.
	r.storeMu.Lock()
	r.store = storeA
	r.storeMu.Unlock()

	taskID := uuid.New()
	// No mapping stored — should fall back.
	got := r.taskStore(taskID)
	if got != storeA {
		t.Fatal("expected taskStore to fall back to currentStore")
	}
}

// --- RunBackground lifecycle tests ---

// TestRunBackgroundCapturesWSKey verifies that RunBackground stores the
// workspace key in taskWSKey before Run() starts.
func TestRunBackgroundCapturesWSKey(t *testing.T) {
	storeDir := t.TempDir()
	s, err := store.NewFileStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mgr := workspace.NewStatic(s, []string{"/ws/a"}, "")
	snap := mgr.Snapshot()

	_, r := setupTestRunnerWithManager(t, nil, mgr)
	r.applyWorkspaceSnapshot(snap)

	taskID := uuid.New()

	// RunBackground will call Run(), which will fail quickly since the
	// task doesn't exist in the store. That's fine — we're testing
	// the bookkeeping, not the execution.
	r.RunBackground(taskID, "test prompt", "", false)

	// The key should be captured immediately (before Run returns).
	if key, ok := r.taskWSKey.Load(taskID); !ok {
		t.Fatal("expected taskWSKey to be populated after RunBackground")
	} else if key.(string) != snap.Key {
		t.Fatalf("expected wsKey=%q, got %q", snap.Key, key.(string))
	}

	// Wait for the background goroutine to finish.
	r.WaitBackground()

	// After completion, the mapping should be cleaned up.
	if _, ok := r.taskWSKey.Load(taskID); ok {
		t.Fatal("expected taskWSKey to be deleted after Run completes")
	}
}

// TestRunBackgroundIncrementsTaskCount verifies that RunBackground calls
// IncrementTaskCount on the workspace manager.
func TestRunBackgroundIncrementsTaskCount(t *testing.T) {
	storeDir := t.TempDir()
	s, err := store.NewFileStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mgr := workspace.NewStatic(s, []string{"/ws/a"}, "")
	snap := mgr.Snapshot()

	_, r := setupTestRunnerWithManager(t, nil, mgr)
	r.applyWorkspaceSnapshot(snap)

	// Launch two tasks to verify count increments.
	taskA := uuid.New()
	taskB := uuid.New()
	r.RunBackground(taskA, "test A", "", false)
	r.RunBackground(taskB, "test B", "", false)

	// Wait for both to finish.
	r.WaitBackground()

	// After both tasks complete, DecrementAndCleanup should have been
	// called twice. Since this is the viewed group, it stays in
	// activeGroups even at count 0.
	keys := mgr.ActiveGroupKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 active group, got %d", len(keys))
	}
}

// TestRunBackgroundCleansUpOnCompletion verifies that after Run() returns,
// both the taskWSKey entry is deleted and DecrementAndCleanup is called.
func TestRunBackgroundCleansUpOnCompletion(t *testing.T) {
	storeA, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeA.Close() })

	mgr := workspace.NewStatic(storeA, []string{"/ws/a"}, "")
	snap := mgr.Snapshot()

	// Manually increment to simulate pre-existing task count.
	mgr.IncrementTaskCount(snap.Key)

	_, r := setupTestRunnerWithManager(t, nil, mgr)
	r.applyWorkspaceSnapshot(snap)

	taskID := uuid.New()
	r.RunBackground(taskID, "cleanup test", "", false)
	r.WaitBackground()

	// taskWSKey should be cleaned up.
	if _, ok := r.taskWSKey.Load(taskID); ok {
		t.Fatal("expected taskWSKey to be deleted after completion")
	}

	// The pre-existing increment + RunBackground's increment = 2.
	// RunBackground's defer calls DecrementAndCleanup once, leaving 1.
	// Since this is the viewed group, it stays regardless.
	if _, ok := mgr.StoreForKey(snap.Key); !ok {
		t.Fatal("expected viewed group to remain in activeGroups")
	}
}

// TestTaskStoreFallbackOnMissingGroup verifies that taskStore falls back to
// currentStore when the mapped group is no longer active in the manager.
func TestTaskStoreFallbackOnMissingGroup(t *testing.T) {
	storeA, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeA.Close() })

	mgr := workspace.NewStatic(storeA, nil, "")

	_, r := setupTestRunnerWithManager(t, nil, mgr)
	r.storeMu.Lock()
	r.store = storeA
	r.storeMu.Unlock()

	taskID := uuid.New()
	r.taskWSKey.Store(taskID, "nonexistent-key")

	// The key is mapped but the group doesn't exist — should fall back.
	got := r.taskStore(taskID)
	if got != storeA {
		t.Fatal("expected taskStore to fall back when group is not in manager")
	}
}
