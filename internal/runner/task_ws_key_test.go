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

	_, r := setupTestRunner(t, nil)
	r.workspaceManager = mgr

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

	_, r := setupTestRunner(t, nil)
	r.workspaceManager = workspace.NewStatic(storeA, nil, "")

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

// TestTaskStoreFallbackOnMissingGroup verifies that taskStore falls back to
// currentStore when the mapped group is no longer active in the manager.
func TestTaskStoreFallbackOnMissingGroup(t *testing.T) {
	storeA, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storeA.Close() })

	mgr := workspace.NewStatic(storeA, nil, "")

	_, r := setupTestRunner(t, nil)
	r.workspaceManager = mgr
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
