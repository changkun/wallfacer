package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
)

// TestTryAutoPromote_SkipsRoutineCards verifies that a routine card in
// backlog is never promoted to in_progress. Routine cards are schedule
// templates; the scheduler engine — not the autopilot — fires them.
func TestTryAutoPromote_SkipsRoutineCards(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	ctx := context.Background()

	routine, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:                 "daily triage",
		Timeout:                30,
		Kind:                   store.TaskKindRoutine,
		RoutineIntervalSeconds: 3600,
		RoutineEnabled:         true,
	})
	if err != nil {
		t.Fatalf("CreateTask (routine): %v", err)
	}

	h.tryAutoPromote(ctx)

	got, err := h.store.GetTask(ctx, routine.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TaskStatusBacklog {
		t.Fatalf("routine card status = %q, want backlog", got.Status)
	}
}

// TestPatchRoutineTask_RejectsStatusChange ensures the generic
// PATCH /api/tasks/{id} refuses to mutate a routine card's lifecycle
// state. Users must go through /api/routines instead (Schedule editing
// lives there, and cards are never meant to run themselves).
func TestPatchRoutineTask_RejectsStatusChange(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	routine, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:                 "pause me",
		Timeout:                30,
		Kind:                   store.TaskKindRoutine,
		RoutineIntervalSeconds: 3600,
		RoutineEnabled:         true,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	body := map[string]any{"status": string(store.TaskStatusInProgress)}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+routine.ID.String(), strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.UpdateTask(rec, req, routine.ID)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	got, _ := h.store.GetTask(ctx, routine.ID)
	if got.Status != store.TaskStatusBacklog {
		t.Fatalf("routine status drifted to %q", got.Status)
	}
}
