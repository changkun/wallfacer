package handler

import (
	"context"
	"sync"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
)

// TwoPhaseWatcherConfig parameterises one auto-watcher loop.
type TwoPhaseWatcherConfig struct {
	// Name is used in log messages.
	Name string
	// Phase1 runs WITHOUT the promoteMu lock. It may do slow I/O (git, store reads).
	// Returns the candidate task to promote, or nil if nothing is eligible.
	Phase1 func(ctx context.Context) (*store.Task, error)
	// Phase2 runs UNDER the promoteMu lock. It re-verifies capacity with fresh
	// state and executes the transition. Returns false if promotion should be skipped.
	Phase2 func(ctx context.Context, candidate *store.Task) (bool, error)
	// AfterPhase1 is an optional test hook fired between Phase1 and acquiring the lock.
	AfterPhase1 func()
	// OnPhase2Miss is called when Phase2 returns (false, nil) indicating a benign
	// race. It receives the candidate that was skipped. Callers may use this to
	// increment a metric or schedule an immediate re-scan. May be nil.
	OnPhase2Miss func(candidate *store.Task)
	// OnPhase1Error is an optional callback invoked when Phase1 returns a
	// non-nil error, after the error has been logged by runTwoPhase. Callers
	// may use this to record a circuit-breaker failure for the watcher. May be nil.
	OnPhase1Error func(error)
}

// runTwoPhase executes the two-phase protocol described above.
// It acquires mu only during Phase2 and emits a debug log when Phase2
// skips the candidate (signals a benign race window). When mu is nil,
// Phase2 runs without locking; this preserves existing behaviour for
// watchers that do not compete for the promoteMu capacity slot.
func runTwoPhase(ctx context.Context, mu *sync.Mutex, cfg TwoPhaseWatcherConfig) {
	candidate, err := cfg.Phase1(ctx)
	if err != nil {
		logger.Handler.Error("two-phase watcher: phase1 error", "watcher", cfg.Name, "error", err)
		if cfg.OnPhase1Error != nil {
			cfg.OnPhase1Error(err)
		}
		return
	}
	if candidate == nil {
		return
	}

	if cfg.AfterPhase1 != nil {
		cfg.AfterPhase1()
	}

	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}

	ok, err := cfg.Phase2(ctx, candidate)
	if err != nil {
		logger.Handler.Error("two-phase watcher: phase2 error", "watcher", cfg.Name, "error", err)
		return
	}
	if !ok {
		logger.Handler.Debug("two-phase watcher: phase2 skipped candidate (benign race)",
			"watcher", cfg.Name, "candidate", candidate.ID)
		if cfg.OnPhase2Miss != nil {
			cfg.OnPhase2Miss(candidate)
		}
	}
}
