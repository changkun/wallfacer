package handler

import (
	"context"
	"testing"

	"changkun.de/x/wallfacer/internal/auth"
	"changkun.de/x/wallfacer/internal/store"
)

// TestInsertEventOrLog_StampsActorFromClaims confirms the
// auth → store attribution bridge at the handler layer. A ctx with
// auth.Claims produces an event stamped with the caller's sub and
// "user" actor type.
func TestInsertEventOrLog_StampsActorFromClaims(t *testing.T) {
	h := newTestHandler(t)
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "p", Timeout: 60})
	if err != nil {
		t.Fatal(err)
	}

	ctx := auth.WithClaims(context.Background(), &auth.Claims{Sub: "user-xyz", PrincipalType: "user"})
	h.insertEventOrLog(ctx, task.ID, store.EventTypeSystem, map[string]string{"msg": "claim-derived"})

	events, err := h.store.GetEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.ActorSub != "user-xyz" {
		t.Errorf("ActorSub = %q, want user-xyz", last.ActorSub)
	}
	if last.ActorType != "user" {
		t.Errorf("ActorType = %q, want user", last.ActorType)
	}
}

// TestInsertEventOrLog_ServicePrincipalMapsToService covers the
// branch where the claim set's principal_type is "service" (service
// accounts calling the API). The helper picks ActorService, not
// ActorUser.
func TestInsertEventOrLog_ServicePrincipalMapsToService(t *testing.T) {
	h := newTestHandler(t)
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "p", Timeout: 60})
	if err != nil {
		t.Fatal(err)
	}

	ctx := auth.WithClaims(context.Background(), &auth.Claims{Sub: "svc-1", PrincipalType: "service"})
	h.insertEventOrLog(ctx, task.ID, store.EventTypeSystem, map[string]string{"msg": "svc"})

	events, _ := h.store.GetEvents(context.Background(), task.ID)
	last := events[len(events)-1]
	if last.ActorType != "service" {
		t.Errorf("ActorType = %q, want service", last.ActorType)
	}
}

// TestInsertEventOrLog_NoClaimsIsAnonymous covers local mode: no
// claims in context, event stamps with empty attribution (matches
// today's on-disk shape).
func TestInsertEventOrLog_NoClaimsIsAnonymous(t *testing.T) {
	h := newTestHandler(t)
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "p", Timeout: 60})
	if err != nil {
		t.Fatal(err)
	}
	h.insertEventOrLog(context.Background(), task.ID, store.EventTypeSystem, map[string]string{"msg": "anon"})

	events, _ := h.store.GetEvents(context.Background(), task.ID)
	last := events[len(events)-1]
	if last.ActorSub != "" || last.ActorType != "" {
		t.Errorf("anon write leaked actor: (%q,%q)", last.ActorSub, last.ActorType)
	}
}
