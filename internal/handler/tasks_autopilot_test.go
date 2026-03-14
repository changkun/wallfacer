package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
)

type autopilotPhase1Store struct {
	waitingTasks []store.Task
	waitingErr   error
	backlogTasks []store.Task
	backlogErr   error
	calls        []store.TaskStatus
}

func (m *autopilotPhase1Store) ListTasksByStatus(_ context.Context, status store.TaskStatus) ([]store.Task, error) {
	m.calls = append(m.calls, status)
	switch status {
	case store.TaskStatusWaiting:
		return append([]store.Task(nil), m.waitingTasks...), m.waitingErr
	case store.TaskStatusBacklog:
		return append([]store.Task(nil), m.backlogTasks...), m.backlogErr
	default:
		return nil, nil
	}
}

func TestTryAutoPromote_Phase1StoreErrorsLogAndOpenBreaker(t *testing.T) {
	waitingErr := errors.New("waiting list failed")
	backlogErr := errors.New("backlog list failed")

	tests := []struct {
		name         string
		store        autopilotPhase1Store
		wantErr      error
		wantCalls    []store.TaskStatus
		wantLogError string
	}{
		{
			name: "waiting_list_error",
			store: autopilotPhase1Store{
				waitingErr: waitingErr,
			},
			wantErr:      waitingErr,
			wantCalls:    []store.TaskStatus{store.TaskStatusWaiting},
			wantLogError: "waiting list failed",
		},
		{
			name: "backlog_list_error",
			store: autopilotPhase1Store{
				backlogErr: backlogErr,
			},
			wantErr:      backlogErr,
			wantCalls:    []store.TaskStatus{store.TaskStatusWaiting, store.TaskStatusBacklog},
			wantLogError: "backlog list failed",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			mockStore := tc.store
			wb := &watcherBreaker{}

			phase1 := func(ctx context.Context) (*store.Task, error) {
				waitingTasks, err := mockStore.ListTasksByStatus(ctx, store.TaskStatusWaiting)
				if err != nil {
					return nil, err
				}
				_ = waitingTasks

				backlogTasks, err := mockStore.ListTasksByStatus(ctx, store.TaskStatusBacklog)
				if err != nil {
					return nil, err
				}
				if len(backlogTasks) == 0 {
					return nil, nil
				}
				return &backlogTasks[0], nil
			}

			candidate, err := phase1(ctx)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Phase1 error = %v, want %v", err, tc.wantErr)
			}
			if candidate != nil {
				t.Fatalf("Phase1 candidate = %+v, want nil", candidate)
			}
			if len(mockStore.calls) != len(tc.wantCalls) {
				t.Fatalf("ListTasksByStatus calls = %v, want %v", mockStore.calls, tc.wantCalls)
			}
			for i, status := range tc.wantCalls {
				if mockStore.calls[i] != status {
					t.Fatalf("ListTasksByStatus call[%d] = %q, want %q", i, mockStore.calls[i], status)
				}
			}

			mockStore.calls = nil

			var buf bytes.Buffer
			prev := logger.Handler
			logger.Handler = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).With("component", "handler")
			defer func() {
				logger.Handler = prev
			}()

			phase2Called := false
			runTwoPhase(ctx, nil, TwoPhaseWatcherConfig{
				Name: "auto-promote",
				OnPhase1Error: func(err error) {
					wb.recordFailure(nil, err.Error())
				},
				Phase1: phase1,
				Phase2: func(_ context.Context, _ *store.Task) (bool, error) {
					phase2Called = true
					return true, nil
				},
			})

			if phase2Called {
				t.Fatal("Phase2 must not run when Phase1 returns an error")
			}
			if !wb.isOpen() {
				t.Fatal("expected circuit breaker to be open after Phase1 store error")
			}

			logOutput := buf.String()
			if !strings.Contains(logOutput, `"msg":"two-phase watcher: phase1 error"`) {
				t.Fatalf("expected phase1 error log, got %q", logOutput)
			}
			if !strings.Contains(logOutput, `"watcher":"auto-promote"`) {
				t.Fatalf("expected watcher name in log, got %q", logOutput)
			}
			if !strings.Contains(logOutput, `"error":"`+tc.wantLogError+`"`) {
				t.Fatalf("expected store error in log, got %q", logOutput)
			}
		})
	}
}
