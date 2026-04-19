package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/planner"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
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
	p.SetBusy(true, "")
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
	// Plain text stream (no SSE framing).
	if strings.Contains(body, "event:") {
		t.Errorf("should not contain SSE framing: %q", body)
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
	if len(cmds) != 12 {
		t.Fatalf("expected 12 commands, got %d", len(cmds))
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
	for _, want := range []string{
		"summarize", "create", "validate", "impact", "status",
		"break-down", "review-breakdown", "dispatch", "review-impl", "diff", "wrapup",
	} {
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
	p.SetBusy(true, "")
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
	if p.LogReader("") != nil {
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

// --- Planning round usage persistence ---

// planningSuccessStdout builds a stream-json result line with the supplied
// tokens and cost. It matches the shape emitted by the agent container.
func planningSuccessStdout(input, output, cacheRead, cacheCreation int, cost float64) []byte {
	payload := map[string]any{
		"type":           "result",
		"stop_reason":    "end_turn",
		"result":         "done",
		"session_id":     "s1",
		"is_error":       false,
		"total_cost_usd": cost,
		"usage": map[string]any{
			"input_tokens":                input,
			"output_tokens":               output,
			"cache_read_input_tokens":     cacheRead,
			"cache_creation_input_tokens": cacheCreation,
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func TestPlanningHandler_PersistsRoundUsage(t *testing.T) {
	ws := t.TempDir()
	h := newStaticWorkspaceHandler(t, []string{ws})

	raw := planningSuccessStdout(120, 40, 15, 5, 0.0123)
	h.persistPlanningRoundUsage(raw)

	key := store.PlanningGroupKey([]string{ws})
	recs, err := store.ReadPlanningUsage(h.configDir, key, time.Time{})
	if err != nil {
		t.Fatalf("ReadPlanningUsage: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	got := recs[0]
	if got.InputTokens != 120 || got.OutputTokens != 40 {
		t.Errorf("tokens: got (%d,%d), want (120,40)", got.InputTokens, got.OutputTokens)
	}
	if got.CacheReadInputTokens != 15 || got.CacheCreationTokens != 5 {
		t.Errorf("cache tokens: got (%d,%d), want (15,5)", got.CacheReadInputTokens, got.CacheCreationTokens)
	}
	if got.CostUSD != 0.0123 {
		t.Errorf("cost: got %v, want 0.0123", got.CostUSD)
	}
	if got.StopReason != "end_turn" {
		t.Errorf("stop_reason: got %q, want end_turn", got.StopReason)
	}
	if got.Sandbox != sandbox.Claude {
		t.Errorf("sandbox: got %q, want claude", got.Sandbox)
	}
	if got.SubAgent != store.SandboxActivityPlanning {
		t.Errorf("sub_agent: got %q, want planning", got.SubAgent)
	}
	if got.Turn != 1 {
		t.Errorf("turn: got %d, want 1", got.Turn)
	}
}

func TestPlanningHandler_IncrementsTurn(t *testing.T) {
	ws := t.TempDir()
	h := newStaticWorkspaceHandler(t, []string{ws})

	h.persistPlanningRoundUsage(planningSuccessStdout(10, 5, 0, 0, 0.001))
	h.persistPlanningRoundUsage(planningSuccessStdout(20, 8, 0, 0, 0.002))

	key := store.PlanningGroupKey([]string{ws})
	recs, err := store.ReadPlanningUsage(h.configDir, key, time.Time{})
	if err != nil {
		t.Fatalf("ReadPlanningUsage: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
	if recs[0].Turn != 1 || recs[1].Turn != 2 {
		t.Errorf("turns: got (%d,%d), want (1,2)", recs[0].Turn, recs[1].Turn)
	}
}

func TestPlanningHandler_FailedExecDoesNotPersist(t *testing.T) {
	ws := t.TempDir()
	h := newStaticWorkspaceHandler(t, []string{ws})

	errLine := []byte(`{"type":"result","stop_reason":"end_turn","result":"boom","session_id":"s1","is_error":true,"total_cost_usd":0.001}`)
	h.persistPlanningRoundUsage(errLine)

	key := store.PlanningGroupKey([]string{ws})
	recs, err := store.ReadPlanningUsage(h.configDir, key, time.Time{})
	if err != nil {
		t.Fatalf("ReadPlanningUsage: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("want 0 records on failed round, got %d", len(recs))
	}
}

func TestArchivedSpecGuard(t *testing.T) {
	ws := t.TempDir()

	// Helper: write a spec file with a given status.
	write := func(rel, status string) {
		t.Helper()
		body := "---\n" +
			"title: T\n" +
			"status: " + status + "\n" +
			"depends_on: []\n" +
			"affects: []\n" +
			"effort: small\n" +
			"created: 2026-01-01\n" +
			"updated: 2026-01-01\n" +
			"author: t\n" +
			"dispatched_task_id: null\n" +
			"---\n\n# T\n\nBody.\n"
		abs := ws + "/" + rel
		if err := os.MkdirAll(ws+"/specs/local", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("specs/local/arch.md", "archived")
	write("specs/local/live.md", "validated")

	tests := []struct {
		name    string
		focused string
		want    bool // whether guard should be non-empty
	}{
		{"archived spec yields guard", "specs/local/arch.md", true},
		{"validated spec yields no guard", "specs/local/live.md", false},
		{"empty path yields no guard", "", false},
		{"missing path yields no guard", "specs/local/missing.md", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := archivedSpecGuard([]string{ws}, tc.focused)
			if tc.want && got == "" {
				t.Errorf("expected non-empty guard for %q, got empty", tc.focused)
			}
			if !tc.want && got != "" {
				t.Errorf("expected empty guard for %q, got %q", tc.focused, got)
			}
			if tc.want && !strings.Contains(got, "archived") {
				t.Errorf("guard should mention 'archived', got %q", got)
			}
			if tc.want && !strings.Contains(got, "unarchive") {
				t.Errorf("guard should instruct to unarchive, got %q", got)
			}
		})
	}
}

func TestPlanningHandler_AppendErrorDoesNotFailRound(t *testing.T) {
	ws := t.TempDir()
	h := newStaticWorkspaceHandler(t, []string{ws})

	// Replace configDir with a path that cannot host a directory: point to a
	// regular file so MkdirAll inside AppendPlanningUsage fails. The helper
	// must log-and-continue, never panic.
	blocker := h.configDir + "-blocker"
	if err := os.WriteFile(blocker, []byte("not a dir"), 0644); err != nil {
		t.Fatalf("prepare blocker: %v", err)
	}
	h.configDir = blocker

	// Must not panic.
	h.persistPlanningRoundUsage(planningSuccessStdout(10, 5, 0, 0, 0.001))
}

// TestSendPlanningMessage_BothFocusedFields verifies that setting both
// focused_spec and focused_task in a single message is rejected with 422.
func TestSendPlanningMessage_BothFocusedFields(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)

	body := strings.NewReader(`{"message":"hi","focused_spec":"specs/foo.md","focused_task":"11111111-1111-1111-1111-111111111111"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/messages", body)
	h.SendPlanningMessage(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d (both focused fields)", rec.Code, http.StatusUnprocessableEntity)
	}
}

// TestSendPlanningMessage_UnknownFocusedTask verifies that a focused_task
// pointing to a non-existent task returns 404.
func TestSendPlanningMessage_UnknownFocusedTask(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)

	body := strings.NewReader(`{"message":"hi","focused_task":"00000000-0000-0000-0000-000000000001"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/messages", body)
	h.SendPlanningMessage(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (unknown task)", rec.Code, http.StatusNotFound)
	}
}

// TestSendPlanningMessage_ModeMismatch verifies that sending a spec-mode
// message to a task-mode thread (and vice versa) returns 409.
func TestSendPlanningMessage_ModeMismatch(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)

	// Create a task so we have a valid task ID.
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt:  "test task",
		Timeout: 15,
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	taskID := task.ID.String()

	tm := h.planner.Threads()
	threadList := tm.List(false)
	if len(threadList) == 0 {
		t.Fatal("expected at least one thread")
	}
	threadID := threadList[0].ID

	// Pin this thread to task-mode by saving a session with FocusedTask.
	cs, err := tm.Store(threadID)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := cs.SaveSession(planner.SessionInfo{FocusedTask: taskID}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// Sending a spec-mode message to a task-mode thread should yield 409.
	body := strings.NewReader(`{"message":"hi","focused_spec":"specs/foo.md","thread":"` + threadID + `"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/messages", body)
	h.SendPlanningMessage(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d (mode mismatch)", rec.Code, http.StatusConflict)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if errMsg, _ := resp["error"].(string); !strings.Contains(errMsg, "task-mode") {
		t.Errorf("error = %q, want mention of task-mode", errMsg)
	}
}

// --- UpdateTaskPromptTool ---

func TestUpdateTaskPromptTool_WritesPrompt(t *testing.T) {
	ctx := context.Background()
	h := newPlannerHandlerWithThreads(t)
	created, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  "original prompt",
		Timeout: 15,
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}

	tm := h.planner.Threads()
	threads := tm.List(false)
	threadID := threads[0].ID
	cs, _ := tm.Store(threadID)
	_ = cs.SaveSession(planner.SessionInfo{FocusedTask: created.ID.String()})

	h2 := h

	body := strings.NewReader(`{"task_id":"` + created.ID.String() + `","prompt":"updated prompt","thread_id":"` + threadID + `"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/tool/update_task_prompt", body)
	h2.UpdateTaskPromptTool(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["prev_prompt"] != "original prompt" {
		t.Errorf("prev_prompt = %v, want 'original prompt'", resp["prev_prompt"])
	}
	if resp["round"].(float64) != 1 {
		t.Errorf("round = %v, want 1", resp["round"])
	}

	updated, err := h2.store.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.Prompt != "updated prompt" {
		t.Errorf("task.Prompt = %q, want 'updated prompt'", updated.Prompt)
	}

	events, err := h2.store.GetEvents(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.EventType == store.EventTypePromptRound {
			found = true
		}
	}
	if !found {
		t.Error("expected prompt_round event to be appended")
	}
}

func TestUpdateTaskPromptTool_WrongThreadMode(t *testing.T) {
	ctx := context.Background()
	h := newPlannerHandlerWithThreads(t)
	created, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  "original",
		Timeout: 15,
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}

	// Use the default thread, which is NOT pinned to any task (spec/file-mode).
	tm := h.planner.Threads()
	threads := tm.List(false)
	threadID := threads[0].ID

	body := strings.NewReader(`{"task_id":"` + created.ID.String() + `","prompt":"new","thread_id":"` + threadID + `"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/tool/update_task_prompt", body)
	h.UpdateTaskPromptTool(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d (wrong mode)", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestUpdateTaskPromptTool_MismatchedTaskID(t *testing.T) {
	ctx := context.Background()

	// Create task A (pinned in thread) and task B (sent in request).
	h := newPlannerHandlerWithThreads(t)
	taskA, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "A", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask A: %v", err)
	}
	taskB, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "B", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask B: %v", err)
	}

	// Pin thread to task A.
	tm := h.planner.Threads()
	threads := tm.List(false)
	threadID := threads[0].ID
	cs, err := tm.Store(threadID)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := cs.SaveSession(planner.SessionInfo{FocusedTask: taskA.ID.String()}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// Send request with task B's ID — should be rejected.
	body := strings.NewReader(`{"task_id":"` + taskB.ID.String() + `","prompt":"new","thread_id":"` + threadID + `"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/tool/update_task_prompt", body)
	h.UpdateTaskPromptTool(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d (mismatched task)", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestUpdateTaskPromptTool_ResumeHintOnWaiting(t *testing.T) {
	ctx := context.Background()
	h := newPlannerHandlerWithThreads(t)
	created, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  "original",
		Timeout: 15,
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, created.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	// Pin thread to task.
	tm := h.planner.Threads()
	threads := tm.List(false)
	threadID := threads[0].ID
	cs, err := tm.Store(threadID)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := cs.SaveSession(planner.SessionInfo{FocusedTask: created.ID.String()}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	body := strings.NewReader(`{"task_id":"` + created.ID.String() + `","prompt":"updated","thread_id":"` + threadID + `"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/tool/update_task_prompt", body)
	h.UpdateTaskPromptTool(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	events, err := h.store.GetEvents(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	var payload store.PromptRoundData
	var found bool
	for _, ev := range events {
		if ev.EventType == store.EventTypePromptRound {
			if err := json.Unmarshal(ev.Data, &payload); err != nil {
				t.Fatalf("unmarshal PromptRoundData: %v", err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected prompt_round event")
	}
	if !payload.ResumeHint {
		t.Error("expected ResumeHint=true for waiting task")
	}
}

func TestTaskPromptRefine_TemplateFieldsPopulated(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	d := prompts.RefinementData{
		CreatedAt: "2026-01-01 00:00:00",
		Today:     "2026-04-19",
		AgeDays:   108,
		Status:    "backlog",
		Prompt:    "implement feature X",
	}
	out := mgr.TaskPromptRefine(d)
	if out == "" {
		t.Fatal("TaskPromptRefine returned empty string")
	}
	for _, want := range []string{"implement feature X", "backlog", "2026-01-01"} {
		if !strings.Contains(out, want) {
			t.Errorf("template output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestIsTaskLocked_TrueDuringTurn(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)
	tm := h.planner.Threads()
	threads := tm.List(false)
	if len(threads) == 0 {
		t.Fatal("expected at least one thread")
	}
	threadID := threads[0].ID

	// Pin the thread to a synthetic task ID.
	const taskID = "00000000-0000-0000-0000-000000000001"
	cs, err := tm.Store(threadID)
	if err != nil {
		t.Fatalf("tm.Store: %v", err)
	}
	if err := cs.SaveSession(planner.SessionInfo{FocusedTask: taskID}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// Not busy yet — should return false.
	if locked, _ := h.isTaskLockedByPlanner(taskID); locked {
		t.Error("isTaskLockedByPlanner: want false when not busy")
	}

	// Mark the planner busy on this thread.
	h.planner.SetBusy(true, threadID)
	defer h.planner.SetBusy(false, "")

	locked, gotThreadID := h.isTaskLockedByPlanner(taskID)
	if !locked {
		t.Error("isTaskLockedByPlanner: want true while exec in flight")
	}
	if gotThreadID != threadID {
		t.Errorf("thread_id = %q, want %q", gotThreadID, threadID)
	}

	// A different task ID should not be locked.
	if locked2, _ := h.isTaskLockedByPlanner("other-task-id"); locked2 {
		t.Error("isTaskLockedByPlanner: want false for unrelated task")
	}

	// Clear busy — should return false again.
	h.planner.SetBusy(false, "")
	if locked3, _ := h.isTaskLockedByPlanner(taskID); locked3 {
		t.Error("isTaskLockedByPlanner: want false after exec ends")
	}
}
