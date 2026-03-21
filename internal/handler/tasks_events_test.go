package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
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
	req.SetPathValue("id", task.ID.String())
	w := httptest.NewRecorder()
	h.GetTurnUsage(w, req)

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
	req.SetPathValue("id", task.ID.String())
	w := httptest.NewRecorder()
	h.GetTurnUsage(w, req)

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

func TestGetTurnUsage_InvalidID(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/notauuid/turn-usage", nil)
	req.SetPathValue("id", "notauuid")
	w := httptest.NewRecorder()
	h.GetTurnUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid id, got %d", w.Code)
	}
}

// --- GetEvents tests (no pagination) ---

func TestGetEvents_NoPagination_EmptyTask(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var events []store.TaskEvent
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// May have 0 or more events from creation.
	if events == nil {
		t.Error("expected non-nil events slice")
	}
}

func TestGetEvents_NoPagination_ReturnsArray(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Insert a system event.
	if err := h.store.InsertEvent(ctx, task.ID, store.EventTypeSystem, map[string]string{"msg": "hello"}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var events []store.TaskEvent
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected at least one event")
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
	for i := 0; i < 5; i++ {
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
	for i := 0; i < 5; i++ {
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

func TestGetEvents_Paged_TypeFilter(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Insert both system and error events.
	if err := h.store.InsertEvent(ctx, task.ID, store.EventTypeSystem, map[string]string{"msg": "system"}); err != nil {
		t.Fatalf("InsertEvent system: %v", err)
	}
	if err := h.store.InsertEvent(ctx, task.ID, store.EventTypeError, map[string]string{"msg": "error"}); err != nil {
		t.Fatalf("InsertEvent error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events?types=system", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp eventsPageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, ev := range resp.Events {
		if ev.EventType != store.EventTypeSystem {
			t.Errorf("expected only system events, got %q", ev.EventType)
		}
	}
}

func TestGetEvents_Paged_InvalidAfter(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events?after=notanumber", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid after, got %d", w.Code)
	}
}

func TestGetEvents_Paged_InvalidLimit(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events?limit=0", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for limit=0, got %d", w.Code)
	}
}

func TestGetEvents_Paged_InvalidType(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events?types=invalid_type", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid type, got %d", w.Code)
	}
}

func TestGetEvents_Paged_LimitCappedAt1000(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// limit=9999 should be capped at 1000 (not rejected).
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events?limit=9999", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for limit=9999 (capped), got %d: %s", w.Code, w.Body.String())
	}
}
