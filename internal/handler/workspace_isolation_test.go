package handler

// Tests that verify workspace isolation: task list endpoints and the SSE
// stream must return tasks scoped to the *current* workspace group, not
// from a stale h.store cache that may point to a different workspace.
//
// Bug: before the fix, ListTasks/StreamTasks accessed h.store directly
// without locking. A concurrent workspace switch could leave h.store
// pointing to workspace A's store while h.workspace.Store() already
// returned workspace B's store. Cross-workspace tasks would appear in
// the wrong workspace's view.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
)

// newStaleStoreHandler creates a handler where h.workspace (the workspace
// manager) holds storeA (with tasks), but h.store (the async-update cache)
// is set to storeB (empty) — simulating the race window where the cache
// lags behind the workspace manager after a workspace switch.
//
// Before the fix all affected handlers read h.store directly and would
// return storeB's (empty) data. After the fix they use h.requireStore()
// which routes through h.workspace.Store() → storeA.
func newStaleStoreHandler(t *testing.T, storeA, storeB *store.Store) *Handler {
	t.Helper()
	h := newTestHandler(t)

	// Step 1: replace the workspace manager so that h.requireStore() →
	// h.workspace.Store() returns storeA (the "current" workspace's store).
	h.workspace = workspace.NewStatic(storeA, nil, "")

	// Step 2: set h.store (the cache) to storeB (empty/stale), simulating the
	// race window where applySnapshot hasn't run yet after a workspace switch.
	h.snapshotMu.Lock()
	h.store = storeB
	h.snapshotMu.Unlock()
	return h
}

// TestListTasks_WorkspaceIsolation_StaleCache reproduces the cross-workspace
// task visibility bug for the GET /api/tasks endpoint.
//
// Setup: workspace manager holds storeA (1 task); h.store cache holds storeB
// (empty), simulating the race window after a workspace switch.
//
// Expected: ListTasks must return storeA's task — NOT the stale empty storeB.
func TestListTasks_WorkspaceIsolation_StaleCache(t *testing.T) {
	ctx := context.Background()

	// storeA: the "current" workspace — has one task.
	dirA, _ := os.MkdirTemp("", "ws-isolation-a-*")
	t.Cleanup(func() { _ = os.RemoveAll(dirA) })
	storeA, err := store.NewFileStore(dirA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storeA.WaitCompaction)
	t.Cleanup(storeA.Close)

	_, err = storeA.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "workspace A task", Timeout: 15,
	})
	if err != nil {
		t.Fatal(err)
	}

	// storeB: the "stale cache" — empty (simulates the old workspace's store
	// still sitting in h.store after the workspace switch hasn't propagated yet).
	dirB, _ := os.MkdirTemp("", "ws-isolation-b-*")
	t.Cleanup(func() { _ = os.RemoveAll(dirB) })
	storeB, err := store.NewFileStore(dirB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storeB.WaitCompaction)
	t.Cleanup(storeB.Close)

	h := newStaleStoreHandler(t, storeA, storeB)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var tasks []store.Task
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Must return storeA's task, NOT storeB's empty list.
	if len(tasks) != 1 {
		t.Errorf("expected 1 task from the current workspace store, got %d — cross-workspace isolation broken", len(tasks))
	}
}

// TestStreamTasks_WorkspaceIsolation_StaleCache reproduces the cross-workspace
// task visibility bug for the GET /api/tasks/stream SSE endpoint.
//
// Same setup as the ListTasks test. The SSE snapshot must be taken from
// storeA (workspace manager's current store), not from storeB (stale cache).
func TestStreamTasks_WorkspaceIsolation_StaleCache(t *testing.T) {
	ctx := context.Background()

	dirA, _ := os.MkdirTemp("", "ws-isolation-stream-a-*")
	t.Cleanup(func() { _ = os.RemoveAll(dirA) })
	storeA, err := store.NewFileStore(dirA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storeA.WaitCompaction)
	t.Cleanup(storeA.Close)

	taskA, err := storeA.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "workspace A task for stream", Timeout: 15,
	})
	if err != nil {
		t.Fatal(err)
	}

	dirB, _ := os.MkdirTemp("", "ws-isolation-stream-b-*")
	t.Cleanup(func() { _ = os.RemoveAll(dirB) })
	storeB, err := store.NewFileStore(dirB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storeB.WaitCompaction)
	t.Cleanup(storeB.Close)

	h := newStaleStoreHandler(t, storeA, storeB)

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()

	// Allow the snapshot event to be written.
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, "event: snapshot") {
		t.Fatalf("expected snapshot event, got:\n%s", body)
	}

	// Parse the snapshot payload and verify it contains storeA's task.
	taskFound := false
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var tasks []store.Task
		if err := json.Unmarshal([]byte(data), &tasks); err != nil {
			continue // not the snapshot array line
		}
		for _, task := range tasks {
			if task.ID == taskA.ID {
				taskFound = true
			}
		}
	}
	if !taskFound {
		t.Errorf("expected task %s from current workspace in SSE snapshot, but it was absent — cross-workspace isolation broken\nfull SSE body:\n%s", taskA.ID, body)
	}
}

// TestStreamTasks_WorkspaceIsolation_SubscriptionMatchesSnapshot verifies that
// the SSE stream subscribes to the same store it takes its initial snapshot
// from, so that delta events after the snapshot belong to the same workspace.
//
// Setup:
//   - storeA (workspace manager's store) has one task.
//   - storeB (stale h.store cache) is empty.
//   - After the snapshot is delivered, we mutate a task in storeB.
//   - A mutation in storeA (the correct store) should produce a delta event.
//   - A mutation in storeB (the stale store) must NOT produce a delta event.
func TestStreamTasks_WorkspaceIsolation_SubscriptionMatchesSnapshot(t *testing.T) {
	ctx := context.Background()

	dirA, _ := os.MkdirTemp("", "ws-isolation-sub-a-*")
	t.Cleanup(func() { _ = os.RemoveAll(dirA) })
	storeA, err := store.NewFileStore(dirA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storeA.WaitCompaction)
	t.Cleanup(storeA.Close)

	taskA, err := storeA.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "task in current workspace", Timeout: 15,
	})
	if err != nil {
		t.Fatal(err)
	}

	dirB, _ := os.MkdirTemp("", "ws-isolation-sub-b-*")
	t.Cleanup(func() { _ = os.RemoveAll(dirB) })
	storeB, err := store.NewFileStore(dirB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storeB.WaitCompaction)
	t.Cleanup(storeB.Close)

	// storeB gets a task too — mutations to it must NOT appear in the stream.
	taskB, err := storeB.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "task in OLD (stale) workspace", Timeout: 15,
	})
	if err != nil {
		t.Fatal(err)
	}

	h := newStaleStoreHandler(t, storeA, storeB)

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()

	// Wait for snapshot delivery.
	time.Sleep(30 * time.Millisecond)

	// Mutate taskA (correct workspace) — should produce a delta event.
	if err := storeA.UpdateTaskStatus(ctx, taskA.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	// Mutate taskB (stale/wrong workspace) — must NOT produce a delta event.
	if err := storeB.UpdateTaskStatus(ctx, taskB.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()

	// taskA's update must appear as a delta (correct workspace subscription).
	if !strings.Contains(body, taskA.ID.String()) {
		t.Errorf("expected taskA (%s) delta in stream — storeA subscription missing", taskA.ID)
	}

	// taskB's update must NOT appear (stream must not be subscribed to storeB).
	if strings.Contains(body, taskB.ID.String()) {
		t.Errorf("taskB (%s) from stale workspace appeared in stream — cross-workspace subscription leak", taskB.ID)
	}
}
