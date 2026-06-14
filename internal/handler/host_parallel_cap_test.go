package handler

import (
	"os"
	"testing"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/runner"
)

// TestMaxConcurrentTasks_DefaultsToOne verifies that with a live runner and no
// configured WALLFACER_MAX_PARALLEL, the handler caps concurrent task promotion
// to 1. This prevents concurrent claude/codex CLI invocations from racing on
// shared ~/.claude and ~/.codex state (session dir, settings SQLite, statsig
// telemetry).
func TestMaxConcurrentTasks_DefaultsToOne(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	// Swap the real runner for a mock. The lazyval cache closes over h.runner
	// at construction time via a method call, so replacing the field before
	// the first Get() is sufficient.
	h.runner = &runner.MockRunner{}
	h.cachedMaxParallel.Invalidate()

	if got := h.maxConcurrentTasks(); got != 1 {
		t.Errorf("default without explicit MAX_PARALLEL should cap to 1; got %d", got)
	}
}

// TestMaxConcurrentTasks_ExplicitOverride verifies that an explicit
// WALLFACER_MAX_PARALLEL value in the env file wins over the cap, so users who
// have verified their CLI tolerates parallel runs can opt back into it.
func TestMaxConcurrentTasks_ExplicitOverride(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	if err := os.WriteFile(envPath, []byte("WALLFACER_MAX_PARALLEL=3\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	h.runner = &runner.MockRunner{}
	h.cachedMaxParallel.Invalidate()

	if got := h.maxConcurrentTasks(); got != 3 {
		t.Errorf("explicit MAX_PARALLEL should override the cap; got %d, want 3", got)
	}
}

// TestMaxConcurrentTasks_NilRunner_DefaultUnchanged is a regression guard: the
// cap is gated on a live runner, so a nil runner must fall back to the global
// default rather than the cap.
func TestMaxConcurrentTasks_NilRunner_DefaultUnchanged(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	h.runner = nil
	h.cachedMaxParallel.Invalidate()

	if got, want := h.maxConcurrentTasks(), constants.DefaultMaxConcurrentTasks; got != want {
		t.Errorf("nil runner should use %d default; got %d", want, got)
	}
}
