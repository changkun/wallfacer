package handler

import (
	"context"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// insertEventOrLog calls InsertEvent and logs any error via the structured
// logger and the autopilot error counter. It is a convenience wrapper for
// call sites that cannot meaningfully recover from an event-write failure (e.g.
// the task was deleted between a status update and the subsequent event write,
// or the underlying trace file hit a disk error). Using this helper prevents
// silent state/event-log divergence while keeping autopilot code paths clean.
func (h *Handler) insertEventOrLog(ctx context.Context, taskID uuid.UUID, eventType store.EventType, data any) {
	if err := h.store.InsertEvent(ctx, taskID, eventType, data); err != nil {
		logger.Handler.Error("InsertEvent failed",
			"task", taskID, "event_type", eventType, "error", err)
		h.incAutopilotAction("event_write", "error")
	}
}
