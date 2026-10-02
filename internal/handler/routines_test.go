package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/store"
)

// patchRoutine is a small test helper that fires PATCH
// /api/routines/{id}/schedule with the given body. Mirrors the shape the
// real HTTP client will send, including the {id} path value wiring.
func patchRoutine(t *testing.T, h *Handler, id uuid.UUID, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/routines/"+id.String()+"/schedule", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()
	h.UpdateRoutineSchedule(rec, req)
	return rec
}

func postRoutine(t *testing.T, h *Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/routines", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CreateRoutine(rec, req)
	return rec
}

func TestCreateRoutine_Valid(t *testing.T) {
	h := newTestHandler(t)
	rec := postRoutine(t, h, map[string]any{
		"prompt":           "daily triage",
		"interval_minutes": 15,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp RoutineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Kind != store.TaskKindRoutine {
		t.Fatalf("kind = %q, want routine", resp.Kind)
	}
	if resp.RoutineIntervalSeconds != 15*60 {
		t.Fatalf("interval = %d, want %d", resp.RoutineIntervalSeconds, 15*60)
	}
	if !resp.RoutineEnabled {
		t.Fatalf("expected enabled by default")
	}

	// Card is materialized in the store.
	got, err := h.store.GetTask(context.Background(), resp.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TaskStatusBacklog {
		t.Fatalf("routine card status = %q, want backlog", got.Status)
	}
}

// TestCreateRoutine_WritesNoSpawnTarget asserts a routine created through the
// API stores neither a spawn flow nor a spawn kind: every fire spawns an
// ordinary task, so the record names no target.
func TestCreateRoutine_WritesNoSpawnTarget(t *testing.T) {
	h := newTestHandler(t)
	rec := postRoutine(t, h, map[string]any{
		"prompt":           "implement routine",
		"interval_minutes": 30,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, "routine_spawn_flow") || strings.Contains(body, "routine_spawn_kind") {
		t.Errorf("response carries a spawn target: %s", body)
	}
	var resp RoutineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, err := h.store.GetTask(context.Background(), resp.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.RoutineSpawnFlow != "" || got.RoutineSpawnKind != "" {
		t.Fatalf("stored spawn flow %q and kind %q, want both empty", got.RoutineSpawnFlow, got.RoutineSpawnKind)
	}
}

// TestCreateRoutine_RejectsSpawnTargetFields asserts the create request no
// longer takes spawn_flow or spawn_kind: either is rejected as an unknown
// field and no routine is stored.
func TestCreateRoutine_RejectsSpawnTargetFields(t *testing.T) {
	h := newTestHandler(t)
	for _, field := range []string{"spawn_flow", "spawn_kind"} {
		t.Run(field, func(t *testing.T) {
			rec := postRoutine(t, h, map[string]any{
				"prompt":           "fleet routine",
				"interval_minutes": 5,
				field:              "implement",
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "unknown field") {
				t.Errorf("body = %q, want the unknown-field rejection", rec.Body.String())
			}
		})
	}
	tasks, err := h.store.ListTasks(context.Background(), true)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("stored %d tasks, want none for rejected requests", len(tasks))
	}
}

func TestCreateRoutine_RejectsEmptyPrompt(t *testing.T) {
	h := newTestHandler(t)
	rec := postRoutine(t, h, map[string]any{
		"interval_minutes": 10,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateRoutine_RejectsShortInterval(t *testing.T) {
	h := newTestHandler(t)
	rec := postRoutine(t, h, map[string]any{
		"prompt":           "too fast",
		"interval_minutes": 0,
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestListRoutines_FiltersByKind(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	_, _ = h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "normal", Timeout: 10})
	routine, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:                 "routine",
		Timeout:                10,
		Kind:                   store.TaskKindRoutine,
		RoutineIntervalSeconds: 3600,
		RoutineEnabled:         true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/routines", nil)
	rec := httptest.NewRecorder()
	h.ListRoutines(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp struct {
		Routines []RoutineResponse `json:"routines"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Routines) != 1 {
		t.Fatalf("got %d routines, want 1", len(resp.Routines))
	}
	if resp.Routines[0].ID != routine.ID {
		t.Fatalf("got %s, want %s", resp.Routines[0].ID, routine.ID)
	}
}

func TestUpdateRoutineSchedule_ChangesInterval(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	routine, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:                 "r",
		Timeout:                10,
		Kind:                   store.TaskKindRoutine,
		RoutineIntervalSeconds: 600,
		RoutineEnabled:         true,
	})

	rec := patchRoutine(t, h, routine.ID, map[string]any{"interval_minutes": 30})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got, _ := h.store.GetTask(ctx, routine.ID)
	if got.RoutineIntervalSeconds != 30*60 {
		t.Fatalf("interval = %d, want %d", got.RoutineIntervalSeconds, 30*60)
	}
}

func TestUpdateRoutineSchedule_TogglesEnabledClearsNextRun(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	routine, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:                 "r",
		Timeout:                10,
		Kind:                   store.TaskKindRoutine,
		RoutineIntervalSeconds: 3600,
		RoutineEnabled:         true,
	})
	// Seed a next-run so we can observe it being cleared on disable.
	future := routine.CreatedAt.Add(time.Hour)
	_ = h.store.UpdateRoutineNextRun(ctx, routine.ID, &future)

	rec := patchRoutine(t, h, routine.ID, map[string]any{"enabled": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got, _ := h.store.GetTask(ctx, routine.ID)
	if got.RoutineEnabled {
		t.Fatalf("expected disabled")
	}
	if got.RoutineNextRun != nil {
		t.Fatalf("expected RoutineNextRun cleared, got %v", got.RoutineNextRun)
	}
}

func TestUpdateRoutineSchedule_RejectsShortInterval(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	routine, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "r", Timeout: 10, Kind: store.TaskKindRoutine,
		RoutineIntervalSeconds: 600, RoutineEnabled: true,
	})

	rec := patchRoutine(t, h, routine.ID, map[string]any{"interval_minutes": 0})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateRoutineSchedule_UnknownID(t *testing.T) {
	h := newTestHandler(t)
	rec := patchRoutine(t, h, uuid.New(), map[string]any{"enabled": false})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateRoutineSchedule_RejectsNonRoutine(t *testing.T) {
	h := newTestHandler(t)
	normal, _ := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "x", Timeout: 5})
	rec := patchRoutine(t, h, normal.ID, map[string]any{"enabled": false})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestTriggerRoutine_AcceptsAndWritesEvent(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	routine, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "r", Timeout: 10, Kind: store.TaskKindRoutine,
		RoutineIntervalSeconds: 600, RoutineEnabled: true,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/routines/"+routine.ID.String()+"/trigger", nil)
	req.SetPathValue("id", routine.ID.String())
	rec := httptest.NewRecorder()
	h.TriggerRoutine(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}

	// Event trail should contain a system:routine:triggered entry.
	events, err := h.store.GetEvents(ctx, routine.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	found := false
	for _, e := range events {
		if e.EventType == store.EventTypeSystem {
			var payload map[string]any
			if err := json.Unmarshal(e.Data, &payload); err == nil && payload["kind"] == "routine:triggered" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected routine:triggered system event, got %+v", events)
	}
}

func TestTriggerRoutine_UnknownID(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/routines/"+uuid.New().String()+"/trigger", nil)
	req.SetPathValue("id", uuid.New().String())
	rec := httptest.NewRecorder()
	h.TriggerRoutine(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}
