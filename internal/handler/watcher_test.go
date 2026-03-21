package handler

import (
	"context"
	"errors"
	"sync"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

type watcherMockStore struct {
	t         *testing.T
	snapshots [][]store.Task
	listCalls int
}

func (m *watcherMockStore) ListTasks(_ context.Context) ([]store.Task, error) {
	m.t.Helper()
	if len(m.snapshots) == 0 {
		return nil, nil
	}
	idx := m.listCalls
	if idx >= len(m.snapshots) {
		idx = len(m.snapshots) - 1
	}
	m.listCalls++
	return append([]store.Task(nil), m.snapshots[idx]...), nil
}

func makeTask(status store.TaskStatus) store.Task {
	return store.Task{ID: uuid.New(), Status: status}
}

func TestRunTwoPhase(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context)
	}{
		{
			name: "phase1_runs_before_lock_acquired",
			run: func(t *testing.T, ctx context.Context) {
				var mu sync.Mutex
				mock := &watcherMockStore{
					t:         t,
					snapshots: [][]store.Task{{makeTask(store.TaskStatusBacklog)}},
				}
				lockFreeInPhase1 := false

				runTwoPhase(ctx, &mu, TwoPhaseWatcherConfig{
					Name: "test",
					Phase1: func(ctx context.Context) (*store.Task, error) {
						if mu.TryLock() {
							mu.Unlock()
							lockFreeInPhase1 = true
						}
						tasks, err := mock.ListTasks(ctx)
						if err != nil || len(tasks) == 0 {
							return nil, err
						}
						return &tasks[0], nil
					},
					Phase2: func(ctx context.Context, _ *store.Task) (bool, error) {
						_, err := mock.ListTasks(ctx)
						if err != nil {
							return false, err
						}
						return true, nil
					},
				})

				if !lockFreeInPhase1 {
					t.Fatal("expected mutex to be free during Phase1")
				}
			},
		},
		{
			name: "phase2_refetches_tasks",
			run: func(t *testing.T, ctx context.Context) {
				var mu sync.Mutex
				candidate := makeTask(store.TaskStatusWaiting)
				mock := &watcherMockStore{
					t: t,
					snapshots: [][]store.Task{
						{candidate},
						{{ID: candidate.ID, Status: store.TaskStatusInProgress}},
					},
				}
				var phase2Status store.TaskStatus

				runTwoPhase(ctx, &mu, TwoPhaseWatcherConfig{
					Name: "test",
					Phase1: func(ctx context.Context) (*store.Task, error) {
						tasks, err := mock.ListTasks(ctx)
						if err != nil || len(tasks) == 0 {
							return nil, err
						}
						return &tasks[0], nil
					},
					Phase2: func(ctx context.Context, _ *store.Task) (bool, error) {
						tasks, err := mock.ListTasks(ctx)
						if err != nil || len(tasks) == 0 {
							return false, err
						}
						phase2Status = tasks[0].Status
						return true, nil
					},
				})

				if mock.listCalls != 2 {
					t.Fatalf("expected Phase2 to re-fetch tasks, got %d ListTasks calls", mock.listCalls)
				}
				if phase2Status != store.TaskStatusInProgress {
					t.Fatalf("expected Phase2 to see refreshed status %q, got %q", store.TaskStatusInProgress, phase2Status)
				}
			},
		},
		{
			name: "promotion_skipped_when_phase2_capacity_fails",
			run: func(t *testing.T, ctx context.Context) {
				var mu sync.Mutex
				candidate := makeTask(store.TaskStatusBacklog)
				mock := &watcherMockStore{
					t: t,
					snapshots: [][]store.Task{
						{candidate},
						{
							{ID: uuid.New(), Status: store.TaskStatusInProgress},
							{ID: uuid.New(), Status: store.TaskStatusInProgress},
						},
					},
				}
				transitionExecuted := false

				runTwoPhase(ctx, &mu, TwoPhaseWatcherConfig{
					Name: "test",
					Phase1: func(ctx context.Context) (*store.Task, error) {
						tasks, err := mock.ListTasks(ctx)
						if err != nil || len(tasks) == 0 {
							return nil, err
						}
						return &tasks[0], nil
					},
					Phase2: func(ctx context.Context, _ *store.Task) (bool, error) {
						tasks, err := mock.ListTasks(ctx)
						if err != nil {
							return false, err
						}
						if countRegularInProgress(tasks) >= 2 {
							return false, nil
						}
						transitionExecuted = true
						return true, nil
					},
				})

				if transitionExecuted {
					t.Fatal("expected no transition when Phase2 capacity check fails")
				}
			},
		},
		{
			name: "after_phase1_hook_fires_between_phases",
			run: func(t *testing.T, ctx context.Context) {
				var mu sync.Mutex
				mock := &watcherMockStore{
					t:         t,
					snapshots: [][]store.Task{{makeTask(store.TaskStatusBacklog)}, {makeTask(store.TaskStatusBacklog)}},
				}
				var callOrder []string

				runTwoPhase(ctx, &mu, TwoPhaseWatcherConfig{
					Name: "test",
					Phase1: func(ctx context.Context) (*store.Task, error) {
						callOrder = append(callOrder, "phase1")
						tasks, err := mock.ListTasks(ctx)
						if err != nil || len(tasks) == 0 {
							return nil, err
						}
						return &tasks[0], nil
					},
					AfterPhase1: func() {
						callOrder = append(callOrder, "after_phase1")
					},
					Phase2: func(ctx context.Context, _ *store.Task) (bool, error) {
						callOrder = append(callOrder, "phase2")
						_, err := mock.ListTasks(ctx)
						if err != nil {
							return false, err
						}
						return true, nil
					},
				})

				want := []string{"phase1", "after_phase1", "phase2"}
				if len(callOrder) != len(want) {
					t.Fatalf("expected call order %v, got %v", want, callOrder)
				}
				for i, v := range want {
					if callOrder[i] != v {
						t.Errorf("call order[%d]: expected %q, got %q", i, v, callOrder[i])
					}
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, ctx)
		})
	}
}

func TestRunTwoPhase_Phase2MissCallbackFired(t *testing.T) {
	ctx := context.Background()
	candidate := makeTask(store.TaskStatusBacklog)
	missCallbackFired := false

	runTwoPhase(ctx, nil, TwoPhaseWatcherConfig{
		Name: "test-miss",
		Phase1: func(_ context.Context) (*store.Task, error) {
			return &candidate, nil
		},
		Phase2: func(_ context.Context, _ *store.Task) (bool, error) {
			return false, nil
		},
		OnPhase2Miss: func(_ *store.Task) {
			missCallbackFired = true
		},
	})

	if !missCallbackFired {
		t.Fatal("expected OnPhase2Miss callback to be called when Phase2 returns (false, nil)")
	}
}

func TestRunTwoPhase_Phase2MissCallbackNotFiredOnSuccess(t *testing.T) {
	ctx := context.Background()
	candidate := makeTask(store.TaskStatusBacklog)
	missCallbackFired := false

	runTwoPhase(ctx, nil, TwoPhaseWatcherConfig{
		Name: "test-success",
		Phase1: func(_ context.Context) (*store.Task, error) {
			return &candidate, nil
		},
		Phase2: func(_ context.Context, _ *store.Task) (bool, error) {
			return true, nil
		},
		OnPhase2Miss: func(_ *store.Task) {
			missCallbackFired = true
		},
	})

	if missCallbackFired {
		t.Fatal("expected OnPhase2Miss callback NOT to be called when Phase2 returns (true, nil)")
	}
}

func TestRunTwoPhase_Phase2MissCallbackNotFiredOnError(t *testing.T) {
	ctx := context.Background()
	candidate := makeTask(store.TaskStatusBacklog)
	missCallbackFired := false

	runTwoPhase(ctx, nil, TwoPhaseWatcherConfig{
		Name: "test-error",
		Phase1: func(_ context.Context) (*store.Task, error) {
			return &candidate, nil
		},
		Phase2: func(_ context.Context, _ *store.Task) (bool, error) {
			return false, errors.New("phase2 error")
		},
		OnPhase2Miss: func(_ *store.Task) {
			missCallbackFired = true
		},
	})

	if missCallbackFired {
		t.Fatal("expected OnPhase2Miss callback NOT to be called when Phase2 returns an error")
	}
}

// TestRunTwoPhase_Phase1Error_CallsOnPhase1Error verifies that when Phase1
// returns a non-nil error the OnPhase1Error callback is invoked and Phase2 is
// never entered.
func TestRunTwoPhase_Phase1Error_CallsOnPhase1Error(t *testing.T) {
	ctx := context.Background()
	sentinelErr := errors.New("injected store error")

	var gotErr error
	phase2Called := false

	runTwoPhase(ctx, nil, TwoPhaseWatcherConfig{
		Name: "test-phase1-error",
		Phase1: func(_ context.Context) (*store.Task, error) {
			return nil, sentinelErr
		},
		OnPhase1Error: func(err error) {
			gotErr = err
		},
		Phase2: func(_ context.Context, _ *store.Task) (bool, error) {
			phase2Called = true
			return true, nil
		},
	})

	if gotErr != sentinelErr {
		t.Fatalf("expected OnPhase1Error to receive sentinel error, got %v", gotErr)
	}
	if phase2Called {
		t.Fatal("expected Phase2 NOT to be called when Phase1 returns an error")
	}
}

// TestRunTwoPhase_Phase1Error_CircuitBreakerRecordsFailure verifies that an
// OnPhase1Error callback that calls recordFailure on a watcherBreaker opens
// the breaker, and that Phase2 is never entered.
func TestRunTwoPhase_Phase1Error_CircuitBreakerRecordsFailure(t *testing.T) {
	ctx := context.Background()
	wb := &watcherBreaker{}
	sentinelErr := errors.New("store unavailable")

	phase2Called := false

	runTwoPhase(ctx, nil, TwoPhaseWatcherConfig{
		Name: "auto-promote",
		Phase1: func(_ context.Context) (*store.Task, error) {
			return nil, sentinelErr
		},
		OnPhase1Error: func(err error) {
			wb.recordFailure(nil, err.Error())
		},
		Phase2: func(_ context.Context, _ *store.Task) (bool, error) {
			phase2Called = true
			return true, nil
		},
	})

	if !wb.isOpen() {
		t.Fatal("expected circuit breaker to be open after Phase1 error")
	}
	if phase2Called {
		t.Fatal("expected Phase2 NOT to be called when Phase1 errors")
	}
}

// TestRunTwoPhase_Phase1Error_OnPhase1ErrorNotCalledOnSuccess verifies that
// OnPhase1Error is NOT invoked when Phase1 succeeds.
func TestRunTwoPhase_Phase1Error_OnPhase1ErrorNotCalledOnSuccess(t *testing.T) {
	ctx := context.Background()
	candidate := makeTask(store.TaskStatusBacklog)
	onPhase1ErrorCalled := false

	runTwoPhase(ctx, nil, TwoPhaseWatcherConfig{
		Name: "test-no-error",
		Phase1: func(_ context.Context) (*store.Task, error) {
			return &candidate, nil
		},
		OnPhase1Error: func(_ error) {
			onPhase1ErrorCalled = true
		},
		Phase2: func(_ context.Context, _ *store.Task) (bool, error) {
			return true, nil
		},
	})

	if onPhase1ErrorCalled {
		t.Fatal("expected OnPhase1Error NOT to be called when Phase1 succeeds")
	}
}

// TestRunTwoPhase_Phase1NilError_OnPhase1ErrorNotCalledOnNilCandidate verifies
// that OnPhase1Error is NOT invoked when Phase1 returns (nil, nil) — the normal
// "nothing to do" signal.
func TestRunTwoPhase_Phase1Error_OnPhase1ErrorNotCalledOnNilCandidate(t *testing.T) {
	ctx := context.Background()
	onPhase1ErrorCalled := false

	runTwoPhase(ctx, nil, TwoPhaseWatcherConfig{
		Name: "test-nil-candidate",
		Phase1: func(_ context.Context) (*store.Task, error) {
			return nil, nil
		},
		OnPhase1Error: func(_ error) {
			onPhase1ErrorCalled = true
		},
		Phase2: func(_ context.Context, _ *store.Task) (bool, error) {
			return true, nil
		},
	})

	if onPhase1ErrorCalled {
		t.Fatal("expected OnPhase1Error NOT to be called for (nil, nil) Phase1 result")
	}
}
