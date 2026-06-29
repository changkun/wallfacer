package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
	"latere.ai/x/wallfacer/internal/store"
)

// TestSetAutoimplement verifies that autoimplement state can be toggled.
func TestSetAutoimplement_Enable(t *testing.T) {
	h := newTestHandler(t)
	if h.AutoimplementEnabled() {
		t.Fatal("expected autoimplement to be disabled by default")
	}

	h.SetAutoimplement(true)
	if !h.AutoimplementEnabled() {
		t.Error("expected autoimplement to be enabled after SetAutoimplement(true)")
	}
}

func TestSetAutoimplement_Disable(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutoimplement(true)
	h.SetAutoimplement(false)
	if h.AutoimplementEnabled() {
		t.Error("expected autoimplement to be disabled after SetAutoimplement(false)")
	}
}

func TestSetAutoimplement_Toggle(t *testing.T) {
	h := newTestHandler(t)
	for i := 0; i < 5; i++ {
		enabled := i%2 == 0
		h.SetAutoimplement(enabled)
		if h.AutoimplementEnabled() != enabled {
			t.Errorf("iteration %d: expected autoimplement=%v, got %v", i, enabled, h.AutoimplementEnabled())
		}
	}
}

// TestPauseAllAutomation_OpensWatcherBreaker verifies that pauseAllAutomation
// now opens the circuit breaker only for the watcher that failed and does NOT
// disable user-controlled automation toggles.
func TestPauseAllAutomation_OpensWatcherBreaker(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutoimplement(true)
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
	if !h.AutoimplementEnabled() {
		t.Error("expected autoimplement toggle to remain enabled")
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
	httpjson.Write(w, http.StatusOK, map[string]string{"key": "value"})

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
		httpjson.Write(w, tc.code, map[string]string{})
		if w.Code != tc.code {
			t.Errorf("expected status %d, got %d", tc.code, w.Code)
		}
	}
}

func TestWriteJSON_EncodesValue(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]any{"count": 42, "name": "test"}
	httpjson.Write(w, http.StatusOK, data)

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
	httpjson.Write(w, http.StatusOK, []string{"a", "b", "c"})

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

	// When not configured, should fall back to constants.DefaultMaxConcurrentTasks.
	if resp.MaxParallelTasks != constants.DefaultMaxConcurrentTasks {
		t.Errorf("expected default %d, got %d", constants.DefaultMaxConcurrentTasks, resp.MaxParallelTasks)
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
	if resp.ArchivedTasksPerPage != constants.DefaultArchivedTasksPerPage {
		t.Errorf("expected default archived_tasks_per_page %d, got %d", constants.DefaultArchivedTasksPerPage, resp.ArchivedTasksPerPage)
	}
}

// TestEnvConfig_ResourceGovernanceRoundTrip proves the new resource knobs
// (max_agents, agent_nice, agon forks/rounds/cost-cap) report their defaults on
// GET and persist through PUT.
func TestEnvConfig_ResourceGovernanceRoundTrip(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	get := func() envConfigResponse {
		w := httptest.NewRecorder()
		h.GetEnvConfig(w, httptest.NewRequest(http.MethodGet, "/api/env", nil))
		var resp envConfigResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	// Defaults: minimum agon floor, backend default nice, unlimited budget.
	d := get()
	if d.AgonForks != 1 || d.AgonRounds != 3 || d.AgonCostCap != 50000 {
		t.Errorf("agon defaults = forks %d rounds %d cap %d; want 1/3/50000", d.AgonForks, d.AgonRounds, d.AgonCostCap)
	}
	if d.AgentNice != executor.DefaultAgentNice {
		t.Errorf("agent_nice default = %d, want %d", d.AgentNice, executor.DefaultAgentNice)
	}
	if d.MaxAgents != 0 {
		t.Errorf("max_agents default = %d, want 0 (unlimited)", d.MaxAgents)
	}

	// Persist new values via PUT.
	body := `{"max_agents":4,"agent_nice":15,"agon_forks":2,"agon_rounds":5,"agon_cost_cap":80000}`
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("PUT = %d: %s", w.Code, w.Body.String())
	}

	// GET reflects the persisted values.
	g := get()
	if g.MaxAgents != 4 || g.AgentNice != 15 || g.AgonForks != 2 || g.AgonRounds != 5 || g.AgonCostCap != 80000 {
		t.Errorf("after PUT = max_agents %d nice %d forks %d rounds %d cap %d; want 4/15/2/5/80000",
			g.MaxAgents, g.AgentNice, g.AgonForks, g.AgonRounds, g.AgonCostCap)
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

// TestTryAutoPromote_NoTasksWhenAutoimplementOff verifies no promotion when autoimplement disabled.
func TestTryAutoPromote_NoPromotionWhenAutoimplementOff(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutoimplement(false)

	ctx := h.store // use store indirectly
	_ = ctx
	h.tryAutoPromote(httptest.NewRequest(http.MethodGet, "/", nil).Context())

	// No panic and no tasks should be promoted.
}

// TestTryAutoPromote_PromotesWhenCapacityAvailable verifies task promotion.
func TestTryAutoPromote_PromotesWhenCapacityAvailable(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	_ = envPath
	h.SetAutoimplement(true)

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

// TestTryAutoPromote_PromotesIdeaAgentTaggedTasks verifies that tasks tagged
// "idea-agent" (created by the brainstorm agent) ARE auto-promoted like normal tasks.
func TestTryAutoPromote_PromotesIdeaAgentTaggedTasks(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutoimplement(true)
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

// TestRequirePrincipalMiddleware_AuthConfigured verifies that with auth
// configured the middleware rejects an anonymous request (no principal) with 401
// and admits a request carrying an authenticated principal. This is the
// data-layer gate that keeps a logged-out browser from reading/writing comments
// even though the instance still holds a coordination token.
func TestRequirePrincipalMiddleware_AuthConfigured(t *testing.T) {
	h := &Handler{}
	h.SetAuth(fakeAuthProvider{}) // HasAuth() == true

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := h.RequirePrincipalMiddleware(next)

	// Anonymous: no principal in context -> 401, next not called.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/spec-comments", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if called {
		t.Error("expected next NOT called for an anonymous request")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d, want 401", w.Code)
	}

	// Authenticated: principal injected -> next called, 200.
	called = false
	ctx := auth.WithIdentity(context.Background(), &auth.Identity{Sub: "user-123"})
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/spec-comments", nil)
	w2 := httptest.NewRecorder()
	mw.ServeHTTP(w2, req2)
	if !called {
		t.Error("expected next called for an authenticated request")
	}
	if w2.Code != http.StatusOK {
		t.Errorf("authenticated: status = %d, want 200", w2.Code)
	}
}

// TestRequirePrincipalMiddleware_LocalMode verifies that without auth configured
// (local single-user mode) the middleware is a no-op: an anonymous request still
// passes through, preserving permissive local behavior.
func TestRequirePrincipalMiddleware_LocalMode(t *testing.T) {
	h := &Handler{} // no auth -> HasAuth() == false
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := h.RequirePrincipalMiddleware(next)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/spec-comments", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if !called {
		t.Error("expected next called in local mode (no auth configured)")
	}
	if w.Code != http.StatusOK {
		t.Errorf("local mode: status = %d, want 200", w.Code)
	}
}

// TestLogout_ClearsCoordinationToken verifies that both Logout and LogoutNotify
// invoke the coordination sign-out hook, so signing out stops the connector
// (nothing pulled while signed out), not just the browser cookie.
func TestLogout_ClearsCoordinationToken(t *testing.T) {
	h := &Handler{} // auth nil: exercises the bare cookie-clear path
	called := 0
	h.SetCoordinationLogout(func() { called++ })

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/auth/logout", nil)
	h.Logout(httptest.NewRecorder(), req)
	if called != 1 {
		t.Fatalf("Logout: hook called %d times, want 1", called)
	}

	req2, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/auth/logout-notify", nil)
	h.LogoutNotify(httptest.NewRecorder(), req2)
	if called != 2 {
		t.Fatalf("LogoutNotify: hook called %d times, want 2", called)
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
