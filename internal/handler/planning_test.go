package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// --- Planning Messages ---

func newPlannerWithStore(t *testing.T) *planner.Planner {
	t.Helper()
	return planner.New(planner.Config{
		Command:     "podman",
		Fingerprint: "test-fp",
		ConfigDir:   t.TempDir(),
	})
}

func TestGetPlanningMessages_Empty(t *testing.T) {
	h := newTestHandler(t)
	h.planner = newPlannerWithStore(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/messages", nil)
	h.GetPlanningMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var msgs []planner.Message
	if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestGetPlanningMessages_WithHistory(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	h.planner = p

	cs := p.Conversation()
	_ = cs.AppendMessage(planner.Message{Role: "user", Content: "hello", Timestamp: time.Now().UTC()})
	_ = cs.AppendMessage(planner.Message{Role: "assistant", Content: "hi", Timestamp: time.Now().UTC()})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/messages", nil)
	h.GetPlanningMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var msgs []planner.Message
	if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("msg[0] = %+v, want user/hello", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hi" {
		t.Errorf("msg[1] = %+v, want assistant/hi", msgs[1])
	}
}

func TestGetPlanningMessages_Pagination(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	h.planner = p

	cs := p.Conversation()
	t1 := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	_ = cs.AppendMessage(planner.Message{Role: "user", Content: "first", Timestamp: t1})
	_ = cs.AppendMessage(planner.Message{Role: "user", Content: "second", Timestamp: t2})
	_ = cs.AppendMessage(planner.Message{Role: "user", Content: "third", Timestamp: t3})

	// Filter: only messages before t3.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/messages?before="+t3.Format(time.RFC3339Nano), nil)
	h.GetPlanningMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var msgs []planner.Message
	if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages before t3, got %d", len(msgs))
	}
}

func TestGetPlanningMessages_NilPlanner(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/messages", nil)
	h.GetPlanningMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestSendPlanningMessage_AutoStarts(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	h.planner = p
	// Planner not started — should auto-start and return 202.
	// The exec will fail in the background (no backend) but the
	// HTTP response is 202 immediately.

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/planning/messages", body)
	h.SendPlanningMessage(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d (auto-start + accepted)", rec.Code, http.StatusAccepted)
	}
}

func TestSendPlanningMessage_Busy(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	_ = p.Start(context.Background())
	p.SetBusy(true)
	h.planner = p

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/planning/messages", body)
	h.SendPlanningMessage(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d (busy)", rec.Code, http.StatusConflict)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp["error"] != "agent is busy" {
		t.Errorf("error = %v, want 'agent is busy'", resp["error"])
	}
}

func TestSendPlanningMessage_EmptyMessage(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	_ = p.Start(context.Background())
	h.planner = p

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"message":"  "}`)
	req := httptest.NewRequest(http.MethodPost, "/api/planning/messages", body)
	h.SendPlanningMessage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestClearPlanningMessages(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	h.planner = p

	cs := p.Conversation()
	_ = cs.AppendMessage(planner.Message{Role: "user", Content: "hello", Timestamp: time.Now().UTC()})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/planning/messages", nil)
	h.ClearPlanningMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	msgs, _ := cs.Messages()
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after clear, got %d", len(msgs))
	}
}

func TestClearPlanningMessages_NilPlanner(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/planning/messages", nil)
	h.ClearPlanningMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// --- Stream ---

func TestStreamPlanningMessages_NotBusy(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	h.planner = p

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/messages/stream", nil)
	h.StreamPlanningMessages(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d (not busy)", rec.Code, http.StatusNoContent)
	}
}

func TestStreamPlanningMessages_NilPlanner(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/messages/stream", nil)
	h.StreamPlanningMessages(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestStreamPlanningMessages_LiveData(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	h.planner = p

	// Simulate a live log with some data, then close it.
	ll := p.StartLiveLog()
	_, _ = ll.Write([]byte(`{"result":"hello"}`))
	ll.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/messages/stream", nil)
	h.StreamPlanningMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `{"result":"hello"}`) {
		t.Errorf("response body missing data: %q", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("response body missing done event: %q", body)
	}
}

// --- Commands ---

func TestGetPlanningCommands(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	h.SetPlanner(p)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/commands", nil)
	h.GetPlanningCommands(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var cmds []planner.Command
	if err := json.Unmarshal(rec.Body.Bytes(), &cmds); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(cmds) != 7 {
		t.Fatalf("expected 7 commands, got %d", len(cmds))
	}

	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
		if c.Description == "" {
			t.Errorf("command %q has empty description", c.Name)
		}
		if c.Usage == "" {
			t.Errorf("command %q has empty usage", c.Name)
		}
	}
	for _, want := range []string{"summarize", "break-down", "create", "status", "validate", "impact", "dispatch"} {
		if !names[want] {
			t.Errorf("missing command %q", want)
		}
	}
}

func TestGetPlanningCommands_NilRegistry(t *testing.T) {
	h := newTestHandler(t)
	// No planner set — commandRegistry is nil.

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/commands", nil)
	h.GetPlanningCommands(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// --- Interrupt ---

func TestInterruptPlanningMessage_NotBusy(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	h.planner = p
	// Not busy — should return 409.

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/messages/interrupt", nil)
	h.InterruptPlanningMessage(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestInterruptPlanningMessage_Busy(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	_ = p.Start(context.Background())
	p.SetBusy(true)
	_ = p.StartLiveLog()
	h.planner = p

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/messages/interrupt", nil)
	h.InterruptPlanningMessage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if p.IsBusy() {
		t.Error("planner should not be busy after interrupt")
	}
	if p.LogReader() != nil {
		t.Error("live log should be nil after interrupt")
	}
}

func TestInterruptPlanningMessage_NilPlanner(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/messages/interrupt", nil)
	h.InterruptPlanningMessage(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
