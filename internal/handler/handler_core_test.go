package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/executor"
)

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

// TestUpdateEnvConfig_ReviewForksIsGone pins the removal of the review fork
// count: a single reviewer has no forks, so the setting is no longer accepted.
func TestUpdateEnvConfig_ReviewForksIsGone(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(`{"review_forks":2}`)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("PUT review_forks = %d, want 400 for an unknown field", w.Code)
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

// TestEnvConfig_ResourceGovernanceRoundTrip proves the resource knobs
// (max_agents, agent_nice) report their defaults on GET and persist through
// PUT.
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

	// Defaults: backend default nice, unlimited agents.
	d := get()
	if d.AgentNice != executor.DefaultAgentNice {
		t.Errorf("agent_nice default = %d, want %d", d.AgentNice, executor.DefaultAgentNice)
	}
	if d.MaxAgents != 0 {
		t.Errorf("max_agents default = %d, want 0 (unlimited)", d.MaxAgents)
	}

	// Persist new values via PUT.
	body := `{"max_agents":4,"agent_nice":15}`
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("PUT = %d: %s", w.Code, w.Body.String())
	}

	// GET reflects the persisted values.
	g := get()
	if g.MaxAgents != 4 || g.AgentNice != 15 {
		t.Errorf("after PUT = max_agents %d nice %d; want 4/15", g.MaxAgents, g.AgentNice)
	}
}
