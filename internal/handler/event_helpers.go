package handler

import (
	"context"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/store"
)

// insertEventOrLog calls InsertEvent and logs any error via the structured
// logger. It is a convenience wrapper for call sites that cannot meaningfully
// recover from an event-write failure (e.g. the task was deleted between a
// status update and the subsequent event write, or the underlying trace file
// hit a disk error).
func (h *Handler) insertEventOrLog(ctx context.Context, taskID uuid.UUID, eventType store.EventType, data any) {
	s, _ := h.currentStore()
	h.insertEventOrLogTo(ctx, s, taskID, eventType, data)
}

// insertEventOrLogTo is insertEventOrLog against an explicitly captured store.
// Background goroutines that outlive their originating request must capture the
// store once (via currentStore) and use this so every write targets the same
// store, rather than re-reading the lock-guarded h.store field which the
// workspace-switch subscription goroutine may reassign concurrently.
func (h *Handler) insertEventOrLogTo(ctx context.Context, s *store.Store, taskID uuid.UUID, eventType store.EventType, data any) {
	if s == nil {
		logger.Handler.Error("InsertEvent skipped: no store",
			"task", taskID, "event_type", eventType)
		return
	}
	if err := s.InsertEvent(ctx, taskID, eventType, data); err != nil {
		logger.Handler.Error("InsertEvent failed",
			"task", taskID, "event_type", eventType, "error", err)
	}
}
