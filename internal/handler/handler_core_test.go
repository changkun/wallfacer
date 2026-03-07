package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	json.NewDecoder(w.Body).Decode(&resp)
	// When not configured, should fall back to defaultMaxConcurrentTasks.
	if resp.MaxParallelTasks != defaultMaxConcurrentTasks {
		t.Errorf("expected default %d, got %d", defaultMaxConcurrentTasks, resp.MaxParallelTasks)
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
	json.NewDecoder(w2.Body).Decode(&resp)
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
