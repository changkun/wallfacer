package handler

import (
	"context"
	"testing"

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
