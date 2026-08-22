package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/store"
)

// --- GetTurnUsage tests ---

func TestGetTurnUsage_NoRecords(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/turn-usage", nil)
	w := httptest.NewRecorder()
	h.GetTurnUsage(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var records []store.TurnUsageRecord
	if err := json.NewDecoder(w.Body).Decode(&records); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestGetTurnUsage_WithRecords(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	rec := store.TurnUsageRecord{
		Turn:         1,
		Timestamp:    time.Now().UTC(),
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.01,
		StopReason:   "end_turn",
	}
	if err := h.store.AppendTurnUsage(task.ID, rec); err != nil {
		t.Fatalf("AppendTurnUsage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/turn-usage", nil)
	w := httptest.NewRecorder()
	h.GetTurnUsage(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var records []store.TurnUsageRecord
	if err := json.NewDecoder(w.Body).Decode(&records); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", records[0].InputTokens)
	}
}

// --- GetEvents with pagination ---

func TestGetEvents_Paged_DefaultLimit(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Insert several events.
	for i := range 5 {
		if err := h.store.InsertEvent(ctx, task.ID, store.EventTypeSystem, map[string]string{"i": fmt.Sprintf("%d", i)}); err != nil {
			t.Fatalf("InsertEvent %d: %v", i, err)
		}
	}

	// Using "after" param triggers paged mode.
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events?after=0", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp eventsPageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode paged response: %v", err)
	}
	if resp.Events == nil {
		t.Error("expected non-nil events slice")
	}
	if len(resp.Events) == 0 {
		t.Error("expected at least one event")
	}
}

func TestGetEvents_Paged_Limit2(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Insert 5 events.
	for i := range 5 {
		if err := h.store.InsertEvent(ctx, task.ID, store.EventTypeSystem, map[string]string{"i": fmt.Sprintf("%d", i)}); err != nil {
			t.Fatalf("InsertEvent %d: %v", i, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events?after=0&limit=2", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp eventsPageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Events) > 2 {
		t.Errorf("expected at most 2 events with limit=2, got %d", len(resp.Events))
	}
	if resp.HasMore {
		if resp.NextAfter == 0 {
			t.Error("NextAfter should be non-zero when has_more=true")
		}
	}
}
