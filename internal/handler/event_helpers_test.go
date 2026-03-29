package handler

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// logCaptureHandler is a minimal slog.Handler that records every log record
// in memory so tests can assert on structured log output without writing to
// stdout. All methods are thread-safe via mu.
type logCaptureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *logCaptureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *logCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *logCaptureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *logCaptureHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *logCaptureHandler) hasError(msg string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level == slog.LevelError && r.Message == msg {
			return true
		}
	}
	return false
}

// TestInsertEventOrLog_ErrorNoPanic verifies that insertEventOrLog does not
// panic when InsertEvent returns an error (task deleted between status update
// and event write), increments the autopilot error counter, and emits a
// structured ERROR log record.
func TestInsertEventOrLog_ErrorNoPanic(t *testing.T) {
	h, reg := newTestHandlerWithRegistry(t)
	ctx := context.Background()

	// Create a task so we have a valid ID, then delete it to force
	// InsertEvent to return "task not found".
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "probe", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.DeleteTask(ctx, task.ID, "test teardown"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	// Redirect logger.Handler to our in-memory capture so we can assert on
	// the logged record without touching stdout.
	capture := &logCaptureHandler{}
	orig := logger.Handler
	logger.Handler = slog.New(capture).With("component", "handler")
	t.Cleanup(func() { logger.Handler = orig })

	// Must not panic.
	h.insertEventOrLog(ctx, task.ID, store.EventTypeSystem, map[string]string{"msg": "probe"})

	// (b) The error counter must have been incremented.
	got := autopilotCounterValue(t, reg, "event_write", "error")
	if got != 1 {
		t.Errorf("expected event_write/error counter = 1, got %v", got)
	}

	// (c) A structured ERROR log line must have been emitted.
	if !capture.hasError("InsertEvent failed") {
		t.Error("expected an ERROR log record with message \"InsertEvent failed\" but none found")
	}
}

// TestInsertEventOrLog_SuccessNoCounter verifies that insertEventOrLog does
// NOT increment the error counter when InsertEvent succeeds.
func TestInsertEventOrLog_SuccessNoCounter(t *testing.T) {
	h, reg := newTestHandlerWithRegistry(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "probe", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	h.insertEventOrLog(ctx, task.ID, store.EventTypeSystem, map[string]string{"msg": "ok"})

	got := autopilotCounterValue(t, reg, "event_write", "error")
	if got != 0 {
		t.Errorf("expected event_write/error counter = 0, got %v", got)
	}
}

// TestInsertEventOrLog_NilRegistryNoPanic verifies that insertEventOrLog
// handles a nil registry gracefully (incAutopilotAction is a no-op when
// h.reg == nil).
func TestInsertEventOrLog_NilRegistryNoPanic(t *testing.T) {
	// newTestHandler creates a Handler with a nil registry.
	h := newTestHandler(t)
	ctx := context.Background()

	missingID := uuid.New()
	// Must not panic even with nil registry and a missing task ID.
	h.insertEventOrLog(ctx, missingID, store.EventTypeSystem, map[string]string{"msg": "probe"})
}
