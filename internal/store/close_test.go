package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// blockingBackend wraps a real backend but holds CompactEvents open until the
// test releases it, so a test can observe whether Close waits for in-flight
// compaction.
type blockingBackend struct {
	StorageBackend
	startOnce sync.Once
	started   chan struct{}
	release   chan struct{}
}

func (b *blockingBackend) CompactEvents(taskID uuid.UUID, events []TaskEvent) error {
	b.startOnce.Do(func() { close(b.started) })
	<-b.release
	return b.StorageBackend.CompactEvents(taskID, events)
}

// TestClose_DrainsInFlightCompaction is the regression test for the flaky
// "directory not empty" TempDir cleanup: Close must not return until the
// background compaction goroutine scheduled by a terminal transition has
// finished. Before the fix Close only set a flag and returned immediately,
// leaving the compactor writing into a directory the caller was about to
// remove.
func TestClose_DrainsInFlightCompaction(t *testing.T) {
	dir := t.TempDir()
	fsb, err := NewFilesystemBackend(dir)
	if err != nil {
		t.Fatalf("NewFilesystemBackend: %v", err)
	}
	bb := &blockingBackend{
		StorageBackend: fsb,
		started:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	s, err := NewStore(bb)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "p", Timeout: 5})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Events are needed so compaction has something to merge (it no-ops on an
	// empty event set and never reaches the backend).
	for range 2 {
		if err := s.InsertEvent(ctx, task.ID, EventTypeStateChange, map[string]string{"n": "x"}); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	// Terminal transition schedules the background compaction. cancelled is
	// only reachable from in_progress.
	if err := s.UpdateTaskStatus(ctx, task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, TaskStatusCancelled); err != nil {
		t.Fatalf("UpdateTaskStatus cancelled: %v", err)
	}

	// Wait until compaction is running and parked inside the backend.
	select {
	case <-bb.started:
	case <-time.After(2 * time.Second):
		t.Fatal("compaction never started")
	}

	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()

	// Close must block while compaction is still parked.
	select {
	case <-closed:
		t.Fatal("Close returned before in-flight compaction finished")
	case <-time.After(200 * time.Millisecond):
	}

	// Release compaction; Close must now return promptly.
	close(bb.release)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after compaction finished")
	}
}

// TestClose_SkipsCompactionAfterClose verifies the other half of the contract:
// a terminal transition after Close does not start (and leak) new background
// work, so WaitCompaction cannot race a late Add.
func TestClose_SkipsCompactionAfterClose(t *testing.T) {
	dir := t.TempDir()
	fsb, err := NewFilesystemBackend(dir)
	if err != nil {
		t.Fatalf("NewFilesystemBackend: %v", err)
	}
	bb := &blockingBackend{
		StorageBackend: fsb,
		started:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	s, err := NewStore(bb)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "p", Timeout: 5})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.InsertEvent(ctx, task.ID, EventTypeStateChange, map[string]string{"n": "x"}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	// Reach a state from which cancelled is valid before closing.
	if err := s.UpdateTaskStatus(ctx, task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}

	s.Close() // no compaction in flight, returns immediately

	// A terminal transition now must not spawn a compaction goroutine.
	if err := s.UpdateTaskStatus(ctx, task.ID, TaskStatusCancelled); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	select {
	case <-bb.started:
		t.Fatal("compaction started after Close")
	case <-time.After(200 * time.Millisecond):
	}
}
