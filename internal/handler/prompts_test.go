package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
)

// newTestHandlerWithPrompts creates a Handler wired with a real prompts.Manager
// pointing at a temporary directory.
func newTestHandlerWithPrompts(t *testing.T) (*Handler, *prompts.Manager) {
	t.Helper()
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, "prompts")

	s, err := store.NewFileStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := prompts.NewManager(promptsDir)
	r := runner.NewRunner(s, runner.RunnerConfig{
		Prompts:   mgr,
		AgentsDir: filepath.Join(dir, "agents"),
		FlowsDir:  filepath.Join(dir, "flows"),
	})
	t.Cleanup(r.WaitBackground)
	h := NewHandler(s, r, dir, []string{}, nil)
	return h, mgr
}

// --- ListSystemPrompts ---

func TestListSystemPrompts_ReturnsAll(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodGet, "/api/system-prompts", nil)
	w := httptest.NewRecorder()
	h.ListSystemPrompts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var result []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 11 {
		t.Errorf("len(result) = %d, want 11", len(result))
	}
	for _, item := range result {
		if item["name"] == "" {
			t.Error("item missing name field")
		}
		if _, ok := item["has_override"]; !ok {
			t.Error("item missing has_override field")
		}
		if item["content"] == "" {
			t.Error("item content is empty — embedded default should not be empty")
		}
	}
}

func TestListSystemPrompts_NoOverrideByDefault(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodGet, "/api/system-prompts", nil)
	w := httptest.NewRecorder()
	h.ListSystemPrompts(w, req)

	var result []map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&result)

	for _, item := range result {
		if hasOverride, _ := item["has_override"].(bool); hasOverride {
			t.Errorf("expected has_override=false for %v, got true", item["name"])
		}
	}
}

// --- GetSystemPrompt ---

func TestGetSystemPrompt_KnownName(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodGet, "/api/system-prompts/title", nil)
	req.SetPathValue("name", "title")
	w := httptest.NewRecorder()
	h.GetSystemPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var result map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&result)

	if result["name"] != "title" {
		t.Errorf("name = %v, want title", result["name"])
	}
}

func TestGetSystemPrompt_UnknownName(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodGet, "/api/system-prompts/nonexistent", nil)
	req.SetPathValue("name", "nonexistent")
	w := httptest.NewRecorder()
	h.GetSystemPrompt(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- UpdateSystemPrompt ---

func TestUpdateSystemPrompt_ValidTemplate(t *testing.T) {
	h, mgr := newTestHandlerWithPrompts(t)

	body, _ := json.Marshal(map[string]string{"content": "Custom: {{.Prompt}}"})
	req := httptest.NewRequest(http.MethodPut, "/api/system-prompts/title", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "title")
	w := httptest.NewRecorder()
	h.UpdateSystemPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Confirm the manager uses the override.
	got := mgr.Title("hello")
	if got != "Custom: hello" {
		t.Errorf("rendered override = %q, want %q", got, "Custom: hello")
	}
}

// TestUpdateSystemPrompt_InvalidTemplate verifies that an invalid Go template
// is rejected with status 422 Unprocessable Entity.
func TestUpdateSystemPrompt_InvalidTemplate(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)

	body, _ := json.Marshal(map[string]string{"content": "{{broken"})
	req := httptest.NewRequest(http.MethodPut, "/api/system-prompts/title", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "title")
	w := httptest.NewRecorder()
	h.UpdateSystemPrompt(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
}

// TestUpdateSystemPrompt_ExecutionError verifies that a template which parses
// successfully but fails at execution (e.g. references a non-existent field on
// a typed struct context) is rejected with status 422 and the error body
// contains "execution" so callers can distinguish it from a parse error.
func TestUpdateSystemPrompt_ExecutionError(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)

	// {{.FieldThatDoesNotExist}} parses fine but fails on execution against
	// RefinementData which has no such field.
	body, _ := json.Marshal(map[string]string{"content": "{{.FieldThatDoesNotExist}}"})
	req := httptest.NewRequest(http.MethodPut, "/api/system-prompts/task_prompt_refine", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "task_prompt_refine")
	w := httptest.NewRecorder()
	h.UpdateSystemPrompt(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "execution") {
		t.Errorf("expected body to contain %q, got: %s", "execution", w.Body.String())
	}
}

func TestUpdateSystemPrompt_UnknownName(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)

	body, _ := json.Marshal(map[string]string{"content": "some content"})
	req := httptest.NewRequest(http.MethodPut, "/api/system-prompts/doesnotexist", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "doesnotexist")
	w := httptest.NewRecorder()
	h.UpdateSystemPrompt(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- DeleteSystemPrompt ---

func TestDeleteSystemPrompt_ExistingOverride(t *testing.T) {
	h, mgr := newTestHandlerWithPrompts(t)

	// Create an override first.
	if err := mgr.WriteOverride("title", "custom {{.Prompt}}"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/system-prompts/title", nil)
	req.SetPathValue("name", "title")
	w := httptest.NewRecorder()
	h.DeleteSystemPrompt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	_, hasOverride, _ := mgr.Content("title")
	if hasOverride {
		t.Error("expected has_override=false after delete, got true")
	}
}

func TestDeleteSystemPrompt_MissingOverride(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/system-prompts/title", nil)
	req.SetPathValue("name", "title")
	w := httptest.NewRecorder()
	h.DeleteSystemPrompt(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestDeleteSystemPrompt_UnknownName(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/system-prompts/nonexistent", nil)
	req.SetPathValue("name", "nonexistent")
	w := httptest.NewRecorder()
	h.DeleteSystemPrompt(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
