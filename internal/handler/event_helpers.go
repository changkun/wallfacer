package handler

import (
	"context"

	"changkun.de/x/wallfacer/internal/auth"
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
//
// Also stamps the ctx with actor attribution (store.WithActorPrincipal)
// derived from auth.PrincipalFromContext so every handler-layer event
// write carries the caller's sub + actor type without requiring each
// handler to thread it manually.
func (h *Handler) insertEventOrLog(ctx context.Context, taskID uuid.UUID, eventType store.EventType, data any) {
	ctx = stampEventActor(ctx)
	if err := h.store.InsertEvent(ctx, taskID, eventType, data); err != nil {
		logger.Handler.Error("InsertEvent failed",
			"task", taskID, "event_type", eventType, "error", err)
		h.incAutopilotAction("event_write", "error")
	}
}

// stampEventActor decorates ctx with actor info for downstream event
// writes. Priority:
//  1. A user/service principal already validated by the auth
//     middleware (OptionalAuth / CookiePrincipal).
//  2. Local anonymous ("") fallback.
//
// The API-key branch is not detected here because the static-key
// middleware does not currently propagate a principal; handler-layer
// writes from that path stamp as anonymous, matching today's
// attribution (the event records simply tell us "someone with the
// key" did it). Future specs can extend the static-key middleware
// to stamp ActorAPIKey if richer attribution is needed.
func stampEventActor(ctx context.Context) context.Context {
	if c, ok := auth.PrincipalFromContext(ctx); ok && c != nil {
		return store.WithActorPrincipal(ctx, c.Sub, actorTypeFor(c))
	}
	return ctx
}

// actorTypeFor picks the right store.ActorType for a claim set.
// Service principals produce "service"; everything else (user or
// agent) maps to "user", since agents acting on behalf of a user are
// attributionally indistinguishable from the user at the audit log
// level (the agent-token-exchange spec handles deeper attribution).
func actorTypeFor(c *auth.Claims) store.ActorType {
	if c.PrincipalType == "service" {
		return store.ActorService
	}
	return store.ActorUser
}
