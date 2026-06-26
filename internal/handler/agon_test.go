package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/agon/pkg/adversarial"
	"latere.ai/x/wallfacer/internal/store"
)

// ─────────────────────────────────────────────────────────────────────────────
// AgonEnabled / SetAgon toggle
// ─────────────────────────────────────────────────────────────────────────────

func TestAgonEnabled_DefaultsFalse(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	if h.AgonEnabled() {
		t.Error("AgonEnabled() should default to false")
	}
}

func TestSetAgon_EnablesAndDisables(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.SetAgon(true)
	if !h.AgonEnabled() {
		t.Error("AgonEnabled() should be true after SetAgon(true)")
	}
	h.SetAgon(false)
	if h.AgonEnabled() {
		t.Error("AgonEnabled() should be false after SetAgon(false)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tryAutoAgon short-circuit paths
// ─────────────────────────────────────────────────────────────────────────────

// mockVerifier records Verify calls.
type mockVerifier struct {
	called int
	lastIn adversarial.VerifyInput
	result *adversarial.VerifyResult
	err    error
}

func (v *mockVerifier) Verify(_ context.Context, in adversarial.VerifyInput) (*adversarial.VerifyResult, error) {
	v.called++
	v.lastIn = in
	return v.result, v.err
}

func TestTryAutoAgon_SkipsWhenDisabled(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 0}}
	h.verifier = v
	// AgonEnabled defaults to false — tryAutoAgon must not call verifier.
	h.tryAutoAgon(context.Background())
	if v.called != 0 {
		t.Errorf("verifier called %d times when agon disabled, want 0", v.called)
	}
}

func TestTryAutoAgon_SkipsTaskWithoutSession(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 0}}
	h.verifier = v
	h.SetAgon(true)

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "no-session", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	// No session → ListWaitingTasksWithSession returns nothing → verifier not called.
	h.tryAutoAgon(ctx)
	if v.called != 0 {
		t.Errorf("verifier called %d times for task without session, want 0", v.called)
	}
}

func TestTryAutoAgon_SkipsTaskWithAgonAlreadyRun(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 0}}
	h.verifier = v
	h.SetAgon(true)

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "already-run", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.UpdateTaskResult(ctx, task.ID, "done", "session-xyz", "end_turn", 1); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	if err := s.UpdateTaskAgon(ctx, task.ID, 0, "", ""); err != nil {
		t.Fatalf("UpdateTaskAgon: %v", err)
	}

	h.tryAutoAgon(ctx)
	if v.called != 0 {
		t.Errorf("verifier called %d times for already-run task, want 0", v.called)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// In-flight dedup + concurrency cap (beginAgon / endAgon)
// ─────────────────────────────────────────────────────────────────────────────

func TestBeginAgon_DedupAndCap(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	id1, id2, id3 := uuid.New(), uuid.New(), uuid.New()

	if !h.beginAgon(id1) {
		t.Fatal("first reservation should succeed")
	}
	if h.beginAgon(id1) {
		t.Fatal("duplicate reservation for the same task must fail")
	}
	if !h.beginAgon(id2) {
		t.Fatal("second distinct task within cap should succeed")
	}
	if h.beginAgon(id3) {
		t.Fatal("third task exceeds maxConcurrentAgon, reservation must fail")
	}
	h.endAgon(id1)
	if !h.beginAgon(id3) {
		t.Fatal("after a slot is released, reservation should succeed")
	}
}

// waitingTaskWithSession creates a waiting task that has a session ID and a
// (non-git) worktree path, the minimum for runAgon to reach the verifier.
func waitingTaskWithSession(t *testing.T, s *store.Store) store.Task {
	t.Helper()
	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "verify", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.UpdateTaskResult(ctx, task.ID, "done", "sess-1", "end_turn", 1); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{t.TempDir(): t.TempDir()}, "branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}
	fresh, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return *fresh
}

func TestRunAgon_PersistsWhenWaiting(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	unresolved := 3
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: unresolved, Headline: "boom"}}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskWithSession(t, s)

	if err := h.runAgon(ctx, s, task); err != nil {
		t.Fatalf("runAgon: %v", err)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.AgonUnresolved == nil || *got.AgonUnresolved != unresolved {
		t.Errorf("AgonUnresolved = %v, want %d", got.AgonUnresolved, unresolved)
	}
	if got.AgonHeadline != "boom" {
		t.Errorf("AgonHeadline = %q, want %q", got.AgonHeadline, "boom")
	}
}

// TestRunAgon_SkipsPersistWhenNotWaiting proves a run that finishes after the
// task already left waiting (resumed, submitted, failed) does not stamp a stale
// result onto it.
func TestRunAgon_SkipsPersistWhenNotWaiting(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 5, Headline: "stale"}}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskWithSession(t, s)

	// Task leaves waiting before the (mock, instantaneous) run completes.
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	if err := h.runAgon(ctx, s, task); err != nil {
		t.Fatalf("runAgon: %v", err)
	}
	if v.called != 1 {
		t.Fatalf("verifier called %d times, want 1", v.called)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.AgonUnresolved != nil {
		t.Errorf("AgonUnresolved = %v, want nil (no stale write)", *got.AgonUnresolved)
	}
}

// blockingVerifier counts Verify calls and parks inside Verify until released,
// so a test can observe a run in flight and assert no duplicate is launched.
type blockingVerifier struct {
	mu      sync.Mutex
	called  int
	entered chan struct{}
	release chan struct{}
}

func (v *blockingVerifier) Verify(_ context.Context, _ adversarial.VerifyInput) (*adversarial.VerifyResult, error) {
	v.mu.Lock()
	v.called++
	v.mu.Unlock()
	v.entered <- struct{}{}
	<-v.release
	return &adversarial.VerifyResult{Unresolved: 0}, nil
}

// TestTryAutoAgon_DedupesConcurrentTicks proves a waiting task whose agon run is
// still in flight is not re-launched on the next watcher tick. Without the
// beginAgon guard, AgonUnresolved stays nil for the whole multi-minute run, so
// every tick fires another duplicate run for the same task.
func TestTryAutoAgon_DedupesConcurrentTicks(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &blockingVerifier{entered: make(chan struct{}, 4), release: make(chan struct{})}
	h.verifier = v
	h.SetAgon(true)

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "dedup", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.UpdateTaskResult(ctx, task.ID, "done", "sess-1", "end_turn", 1); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	// A non-git worktree path is enough: generateWorktreeDiff skips it and
	// returns "", and runAgon still reaches the verifier.
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{t.TempDir(): t.TempDir()}, "branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	// First tick: launch a run and wait until it is parked inside Verify so the
	// in-flight slot is held.
	h.tryAutoAgon(ctx)
	select {
	case <-v.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first agon run never reached the verifier")
	}

	// Second tick while the first run is still in flight: must dedup.
	h.tryAutoAgon(ctx)
	select {
	case <-v.entered:
		t.Fatal("second agon run started for an in-flight task; dedup failed")
	case <-time.After(200 * time.Millisecond):
	}

	close(v.release) // let the first run finish

	v.mu.Lock()
	got := v.called
	v.mu.Unlock()
	if got != 1 {
		t.Errorf("verifier called %d times, want 1", got)
	}
}
