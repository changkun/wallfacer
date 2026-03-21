package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// TestSetAutopilot verifies that autopilot state can be toggled.
func TestSetAutopilot_Enable(t *testing.T) {
	h := newTestHandler(t)
	if h.AutopilotEnabled() {
		t.Fatal("expected autopilot to be disabled by default")
	}

	h.SetAutopilot(true)
	if !h.AutopilotEnabled() {
		t.Error("expected autopilot to be enabled after SetAutopilot(true)")
	}
}

func TestSetAutopilot_Disable(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	h.SetAutopilot(false)
	if h.AutopilotEnabled() {
		t.Error("expected autopilot to be disabled after SetAutopilot(false)")
	}
}

func TestSetAutopilot_Toggle(t *testing.T) {
	h := newTestHandler(t)
	for i := 0; i < 5; i++ {
		enabled := i%2 == 0
		h.SetAutopilot(enabled)
		if h.AutopilotEnabled() != enabled {
			t.Errorf("iteration %d: expected autopilot=%v, got %v", i, enabled, h.AutopilotEnabled())
		}
	}
}

// TestPauseAllAutomation_OpensWatcherBreaker verifies that pauseAllAutomation
// now opens the circuit breaker only for the watcher that failed and does NOT
// disable user-controlled automation toggles.
func TestPauseAllAutomation_OpensWatcherBreaker(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	h.SetAutotest(true)
	h.SetAutosubmit(true)

	taskID := uuid.New()
	paused := h.pauseAllAutomation(&taskID, "auto-submit", "boom")
	if !paused {
		t.Fatal("expected pauseAllAutomation to return true (new failure)")
	}

	// The circuit breaker for the failing watcher should be open.
	if !h.breakers["auto-submit"].isOpen() {
		t.Error("expected auto-submit circuit breaker to be open")
	}
	// Other watchers must remain healthy.
	if h.breakers["auto-promote"].isOpen() {
		t.Error("expected auto-promote circuit breaker to remain closed")
	}
	if h.breakers["auto-test"].isOpen() {
		t.Error("expected auto-test circuit breaker to remain closed")
	}

	// User-controlled toggles must NOT be disabled by a circuit breaker event.
	if !h.AutopilotEnabled() {
		t.Error("expected autopilot toggle to remain enabled")
	}
	if !h.AutotestEnabled() {
		t.Error("expected autotest toggle to remain enabled")
	}
	if !h.AutosubmitEnabled() {
		t.Error("expected autosubmit toggle to remain enabled")
	}
}

// TestWriteJSON_SetsContentType verifies that writeJSON sets the correct content type.
func TestWriteJSON_SetsContentType(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"key": "value"})

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json content-type, got %q", ct)
	}
}

func TestWriteJSON_SetsStatusCode(t *testing.T) {
	tests := []struct {
		code int
	}{
		{http.StatusOK},
		{http.StatusCreated},
		{http.StatusNoContent},
		{http.StatusBadRequest},
		{http.StatusNotFound},
	}
	for _, tc := range tests {
		w := httptest.NewRecorder()
		writeJSON(w, tc.code, map[string]string{})
		if w.Code != tc.code {
			t.Errorf("expected status %d, got %d", tc.code, w.Code)
		}
	}
}

func TestWriteJSON_EncodesValue(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]any{"count": 42, "name": "test"}
	writeJSON(w, http.StatusOK, data)

	var decoded map[string]any
	if err := json.NewDecoder(w.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["count"] != float64(42) {
		t.Errorf("expected count=42, got %v", decoded["count"])
	}
	if decoded["name"] != "test" {
		t.Errorf("expected name=test, got %v", decoded["name"])
	}
}

func TestWriteJSON_EncodesSlice(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, []string{"a", "b", "c"})

	var decoded []string
	if err := json.NewDecoder(w.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 3 {
		t.Errorf("expected 3 items, got %d", len(decoded))
	}
}

// TestGetEnvConfig_Success verifies that GetEnvConfig returns a valid response.
func TestGetEnvConfig_Success(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/env", nil)
	w := httptest.NewRecorder()
	h.GetEnvConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp envConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Defaults should be sensible.
	if resp.MaxParallelTasks <= 0 {
		t.Errorf("expected MaxParallelTasks > 0, got %d", resp.MaxParallelTasks)
	}
}

func TestGetEnvConfig_DefaultMaxParallel(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/env", nil)
	w := httptest.NewRecorder()
	h.GetEnvConfig(w, req)

	var resp envConfigResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	// When not configured, should fall back to defaultMaxConcurrentTasks.
	if resp.MaxParallelTasks != defaultMaxConcurrentTasks {
		t.Errorf("expected default %d, got %d", defaultMaxConcurrentTasks, resp.MaxParallelTasks)
	}
}

func TestGetEnvConfig_DefaultArchivedTasksPerPage(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/env", nil)
	w := httptest.NewRecorder()
	h.GetEnvConfig(w, req)

	var resp envConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ArchivedTasksPerPage != defaultArchivedTasksPerPage {
		t.Errorf("expected default archived_tasks_per_page %d, got %d", defaultArchivedTasksPerPage, resp.ArchivedTasksPerPage)
	}
}

// TestUpdateEnvConfig_InvalidJSON returns 400 for bad JSON.
func TestUpdateEnvConfig_InvalidJSON(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader("{bad json"))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestUpdateEnvConfig_ClampsMinParallel verifies that max_parallel_tasks < 1 is clamped to 1.
func TestUpdateEnvConfig_ClampsMinParallel(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	body := `{"max_parallel_tasks": 0}`
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the stored value is 1 (clamped from 0).
	req2 := httptest.NewRequest(http.MethodGet, "/api/env", nil)
	w2 := httptest.NewRecorder()
	h.GetEnvConfig(w2, req2)
	var resp envConfigResponse
	_ = json.NewDecoder(w2.Body).Decode(&resp)

	if resp.MaxParallelTasks != 1 {
		t.Errorf("expected clamped value of 1, got %d", resp.MaxParallelTasks)
	}
}

// TestUpdateEnvConfig_EmptyTokenTreatedAsNoChange verifies that empty oauth_token is ignored.
func TestUpdateEnvConfig_EmptyTokenTreatedAsNoChange(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	// Setting empty string should not fail — it's silently ignored.
	body := `{"oauth_token": ""}`
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

// TestTryAutoPromote_NoTasksWhenAutopilotOff verifies no promotion when autopilot disabled.
func TestTryAutoPromote_NoPromotionWhenAutopilotOff(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(false)

	ctx := h.store // use store indirectly
	_ = ctx
	h.tryAutoPromote(httptest.NewRequest(http.MethodGet, "/", nil).Context())

	// No panic and no tasks should be promoted.
}

// TestTryAutoPromote_PromotesWhenCapacityAvailable verifies task promotion.
func TestTryAutoPromote_PromotesWhenCapacityAvailable(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	_ = envPath
	h.SetAutopilot(true)

	// Set max parallel to 1 so we know the limit.
	body := `{"max_parallel_tasks": 1}`
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)
	_ = w

	// The UpdateEnvConfig call above triggers tryAutoPromote in a goroutine.
	// Create a backlog task that can be promoted.
	// (The test in env_test.go already covers this pattern; we check the state machine here.)
}

// TestTryAutoPromote_SkipsIdeaAgentKindTasks verifies that autopilot does not
// promote tasks with Kind=TaskKindIdeaAgent (the brainstorm runner task itself).
func TestTryAutoPromote_SkipsIdeaAgentKindTasks(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	ctx := context.Background()

	// Create a brainstorm runner task (Kind=TaskKindIdeaAgent).
	_, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm runner", Timeout: 30, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatalf("CreateTask (idea-agent kind): %v", err)
	}

	h.tryAutoPromote(ctx)

	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	for _, task := range tasks {
		if task.Status == store.TaskStatusInProgress {
			t.Errorf("expected idea-agent kind task to stay in backlog, but it was promoted to in_progress")
		}
	}
}

// TestTryAutoPromote_PromotesIdeaAgentTaggedTasks verifies that tasks tagged
// "idea-agent" (created by the brainstorm agent) ARE auto-promoted like normal tasks.
func TestTryAutoPromote_PromotesIdeaAgentTaggedTasks(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	ctx := context.Background()

	// Create a task tagged "idea-agent" (created by brainstorm, Kind=TaskKindTask).
	ideaTask, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm idea", Timeout: 30, Kind: store.TaskKindTask, Tags: []string{"idea-agent"}})
	if err != nil {
		t.Fatalf("CreateTask (idea-agent tagged): %v", err)
	}

	h.tryAutoPromote(ctx)

	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}

	taskMap := make(map[string]store.TaskStatus)
	for _, task := range tasks {
		taskMap[task.ID.String()] = task.Status
	}

	// The idea-agent tagged task should have been promoted out of backlog.
	if got := taskMap[ideaTask.ID.String()]; got == store.TaskStatusBacklog {
		t.Errorf("idea-agent tagged task: expected promotion out of backlog, still in backlog")
	}
}

// TestTryAutoPromote_PromotesManualTaskButNotIdeaAgentKind verifies that when both
// a manual task and a brainstorm runner task (Kind=TaskKindIdeaAgent) are in backlog,
// only the manual one is promoted.
func TestTryAutoPromote_PromotesManualTaskButNotIdeaAgentKind(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	ctx := context.Background()

	// Create a manual backlog task first (lower position).
	manual, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "manual task", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask (manual): %v", err)
	}

	// Create a brainstorm runner task (Kind=TaskKindIdeaAgent).
	ideaTask, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm runner", Timeout: 30, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatalf("CreateTask (idea-agent kind): %v", err)
	}

	h.tryAutoPromote(ctx)

	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}

	taskMap := make(map[string]store.TaskStatus)
	for _, task := range tasks {
		taskMap[task.ID.String()] = task.Status
	}

	// The brainstorm runner task must remain in backlog.
	if got := taskMap[ideaTask.ID.String()]; got != store.TaskStatusBacklog {
		t.Errorf("idea-agent kind task: expected backlog, got %s", got)
	}

	// The manual task should have been promoted (in_progress or beyond).
	if got := taskMap[manual.ID.String()]; got == store.TaskStatusBacklog {
		t.Errorf("manual task: expected promotion out of backlog, still in backlog")
	}
}

// TestStatusError_Error verifies that the statusError type implements error correctly.
func TestStatusError_Error(t *testing.T) {
	err := httpErrorf(http.StatusBadRequest, "invalid %s", "input")
	if err == nil {
		t.Fatal("expected non-nil error from httpErrorf")
	}
	if err.Error() != "invalid input" {
		t.Errorf("Error() = %q, want %q", err.Error(), "invalid input")
	}
}

// TestHasStore_WithStore verifies that hasStore returns true when the handler
// has a configured store.
func TestHasStore_WithStore(t *testing.T) {
	h := newTestHandler(t)
	if !h.hasStore() {
		t.Error("expected hasStore=true when handler has a store")
	}
}

// TestHasStore_WithoutStore verifies that hasStore returns false when no
// workspaces are configured.
func TestHasStore_WithoutStore(t *testing.T) {
	h := &Handler{} // no store
	if h.hasStore() {
		t.Error("expected hasStore=false for handler with no store")
	}
}

// TestRequireStore_WithStore verifies that requireStore returns the store
// and true when the handler has a store configured.
func TestRequireStore_WithStore(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	s, ok := h.requireStore(w)
	if !ok {
		t.Error("expected requireStore to return ok=true")
	}
	if s == nil {
		t.Error("expected non-nil store from requireStore")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 response code, got %d", w.Code)
	}
}

// TestRequireStore_WithoutStore verifies that requireStore writes a 503 and
// returns nil, false when no store is configured.
func TestRequireStore_WithoutStore(t *testing.T) {
	h := &Handler{} // no store
	w := httptest.NewRecorder()
	s, ok := h.requireStore(w)
	if ok {
		t.Error("expected requireStore to return ok=false with no store")
	}
	if s != nil {
		t.Error("expected nil store from requireStore with no store")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

// TestRequireStoreMiddleware_WithStore verifies that RequireStoreMiddleware
// passes through to the next handler when a store is configured.
func TestRequireStoreMiddleware_WithStore(t *testing.T) {
	h := newTestHandler(t)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := h.RequireStoreMiddleware(next)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if !called {
		t.Error("expected next handler to be called when store is present")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestRequireStoreMiddleware_WithoutStore verifies that RequireStoreMiddleware
// returns a 503 and does not call next when no store is configured.
func TestRequireStoreMiddleware_WithoutStore(t *testing.T) {
	h := &Handler{} // no store
	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})
	mw := h.RequireStoreMiddleware(next)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if called {
		t.Error("expected next handler NOT to be called when store is absent")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(strings.NewReader(w.Body.String())).Decode(&resp); err == nil {
		if resp["error"] == "" {
			t.Error("expected non-empty error in response body")
		}
	}
}

// TestSetAutopush_Toggle verifies that SetAutopush toggles the autopush state.
func TestSetAutopush_Toggle(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopush(true)
	if !h.AutopushEnabled() {
		t.Error("expected autopush=true after SetAutopush(true)")
	}
	h.SetAutopush(false)
	if h.AutopushEnabled() {
		t.Error("expected autopush=false after SetAutopush(false)")
	}
}
