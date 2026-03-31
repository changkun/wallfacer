package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"changkun.de/x/wallfacer/internal/planner"
)

func TestGetPlanningStatus_NilPlanner(t *testing.T) {
	h := newTestHandler(t)
	// h.planner is nil by default — should return running: false.

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning", nil)
	h.GetPlanningStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp["running"] != false {
		t.Errorf("running = %v, want false", resp["running"])
	}
}

func TestGetPlanningStatus_WithPlanner(t *testing.T) {
	h := newTestHandler(t)
	h.planner = planner.New(planner.Config{Command: "podman"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning", nil)
	h.GetPlanningStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	// Not started, so running should be false.
	if resp["running"] != false {
		t.Errorf("running = %v, want false", resp["running"])
	}
}

func TestStartPlanning_NilPlanner(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning", nil)
	h.StartPlanning(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestStopPlanning_NilPlanner(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/planning", nil)
	h.StopPlanning(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp["stopped"] != true {
		t.Errorf("stopped = %v, want true", resp["stopped"])
	}
}

func TestStopPlanning_WithPlanner(t *testing.T) {
	h := newTestHandler(t)
	h.planner = planner.New(planner.Config{Command: "podman"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/planning", nil)
	h.StopPlanning(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp["stopped"] != true {
		t.Errorf("stopped = %v, want true", resp["stopped"])
	}
}

func TestSetPlanner(t *testing.T) {
	h := newTestHandler(t)
	if h.planner != nil {
		t.Fatal("expected nil planner by default")
	}

	p := planner.New(planner.Config{Command: "podman"})
	h.SetPlanner(p)

	if h.planner != p {
		t.Error("SetPlanner did not set the planner field")
	}
}
