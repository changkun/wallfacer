package handler

import (
	"os"
	"testing"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/runner"
)

// TestMaxConcurrentTasks_HostModeDefaultsToOne verifies that when the runner
// reports host mode and the user hasn't configured WALLFACER_MAX_PARALLEL,
// the handler caps concurrent task promotion to 1. This prevents concurrent
// claude/codex CLI invocations from racing on shared ~/.claude and ~/.codex
// state (session dir, settings SQLite, statsig telemetry).
func TestMaxConcurrentTasks_HostModeDefaultsToOne(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	// Swap the real runner for a mock that reports host mode. The lazyval
	// cache closes over h.runner at construction time via a method call,
	// so replacing the field before the first Get() is sufficient.
	h.runner = &runner.MockRunner{Host: true}
	h.cachedMaxParallel.Invalidate()

	if got := h.maxConcurrentTasks(); got != 1 {
		t.Errorf("host mode without explicit MAX_PARALLEL should cap to 1; got %d", got)
	}
}

// TestMaxConcurrentTasks_HostModeExplicitOverride verifies that an explicit
// WALLFACER_MAX_PARALLEL value in the env file wins over the host-mode cap,
// so users who have verified their CLI tolerates parallel runs can opt back
// into it.
func TestMaxConcurrentTasks_HostModeExplicitOverride(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	if err := os.WriteFile(envPath, []byte("WALLFACER_MAX_PARALLEL=3\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	h.runner = &runner.MockRunner{Host: true}
	h.cachedMaxParallel.Invalidate()

	if got := h.maxConcurrentTasks(); got != 3 {
		t.Errorf("explicit MAX_PARALLEL should override host-mode cap; got %d, want 3", got)
	}
}

// TestMaxConcurrentTasks_ContainerMode_DefaultUnchanged is a regression guard:
// container mode (host=false) must keep the global default, not the host cap.
func TestMaxConcurrentTasks_ContainerMode_DefaultUnchanged(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	h.runner = &runner.MockRunner{Host: false}
	h.cachedMaxParallel.Invalidate()

	if got, want := h.maxConcurrentTasks(), constants.DefaultMaxConcurrentTasks; got != want {
		t.Errorf("container mode should use %d default; got %d", want, got)
	}
}
