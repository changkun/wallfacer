package handler

import (
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/store/storetest"
	"latere.ai/x/wallfacer/internal/workspace"
)

// TestResubscribingWakeSourceForwardsSignals verifies that wake signals from
// the store are forwarded to the output channel.
func TestResubscribingWakeSourceForwardsSignals(t *testing.T) {
	s, err := storetest.NewFileStore(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mgr := workspace.NewStatic(s, nil)
	h := &Handler{workspace: mgr, store: s}

	src := h.newResubscribingWakeSource()
	defer src.UnsubscribeWake(0)

	_, ch := src.SubscribeWake()

	// Trigger a wake signal by creating a task (which publishes to the hub).
	if _, err := s.CreateTaskWithOptions(t.Context(), store.TaskCreateOptions{Prompt: "test", Timeout: 5}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
		// Got the forwarded signal.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for forwarded wake signal")
	}
}

// TestResubscribingWakeSourceCancelCleanup verifies that calling
// UnsubscribeWake stops the goroutine without panic or deadlock.
func TestResubscribingWakeSourceCancelCleanup(t *testing.T) {
	s, err := storetest.NewFileStore(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mgr := workspace.NewStatic(s, nil)
	h := &Handler{workspace: mgr, store: s}

	src := h.newResubscribingWakeSource()

	// UnsubscribeWake should stop the goroutine cleanly.
	src.UnsubscribeWake(0)

	// Calling it again should not panic.
	src.UnsubscribeWake(0)
}

// TestResubscribingWakeSourceNilWorkspaceManager verifies that the source
// works when the workspace manager is nil (no re-subscription, just forwards
// from the initial store).
func TestResubscribingWakeSourceNilWorkspaceManager(t *testing.T) {
	s, err := storetest.NewFileStore(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	h := &Handler{workspace: nil, store: s}

	src := h.newResubscribingWakeSource()
	defer src.UnsubscribeWake(0)

	_, ch := src.SubscribeWake()

	// Trigger a wake signal.
	if _, err := s.CreateTaskWithOptions(t.Context(), store.TaskCreateOptions{Prompt: "test", Timeout: 5}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for wake signal with nil manager")
	}
}
