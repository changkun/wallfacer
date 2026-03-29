package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/prompts"
)

// newTestHandlerWithInstructions creates a Handler with a configDir that has a
// workspace list so instructions endpoints can function.
func newTestHandlerWithInstructions(t *testing.T) (*Handler, string) {
	t.Helper()
	configDir := t.TempDir()
	// Create the instructions directory inside configDir.
	instDir := filepath.Join(configDir, "instructions")
	if err := os.MkdirAll(instDir, 0755); err != nil {
		t.Fatal(err)
	}

	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{})
	t.Cleanup(r.WaitBackground)

	workspaces := []string{t.TempDir()}
	h := NewHandler(s, r, configDir, workspaces, nil)
	return h, configDir
}

// TestGetInstructions_NoFile returns empty content when file doesn't exist.
func TestGetInstructions_NoFile(t *testing.T) {
	h, _ := newTestHandlerWithInstructions(t)
	req := httptest.NewRequest(http.MethodGet, "/api/instructions", nil)
	w := httptest.NewRecorder()
	h.GetInstructions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["content"] != "" {
		t.Errorf("expected empty content when file missing, got %q", resp["content"])
	}
}

// TestGetInstructions_ReturnsContent returns file content when file exists.
func TestGetInstructions_ReturnsContent(t *testing.T) {
	h, configDir := newTestHandlerWithInstructions(t)

	// Write the instructions file directly.
	instPath := prompts.InstructionsFilePath(configDir, h.workspaces)
	if err := os.MkdirAll(filepath.Dir(instPath), 0755); err != nil {
		t.Fatal(err)
	}
	expected := "# My Instructions\n\nDo good things."
	if err := os.WriteFile(instPath, []byte(expected), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/instructions", nil)
	w := httptest.NewRecorder()
	h.GetInstructions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["content"] != expected {
		t.Errorf("expected %q, got %q", expected, resp["content"])
	}
}

// TestUpdateInstructions_InvalidJSON returns 400 for malformed request body.
func TestUpdateInstructions_InvalidJSON(t *testing.T) {
	h, _ := newTestHandlerWithInstructions(t)
	req := httptest.NewRequest(http.MethodPut, "/api/instructions", strings.NewReader("{bad json"))
	w := httptest.NewRecorder()
	h.UpdateInstructions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestUpdateInstructions_WritesContent verifies that content is persisted.
func TestUpdateInstructions_WritesContent(t *testing.T) {
	h, configDir := newTestHandlerWithInstructions(t)

	content := "# Updated Instructions\n\nNew content here."
	body := `{"content": "# Updated Instructions\n\nNew content here."}`
	req := httptest.NewRequest(http.MethodPut, "/api/instructions", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateInstructions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the file was written.
	instPath := prompts.InstructionsFilePath(configDir, h.workspaces)
	written, err := os.ReadFile(instPath)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	// The JSON string will have the literal \n, not a real newline.
	if !strings.Contains(string(written), "Updated Instructions") {
		t.Errorf("expected written file to contain 'Updated Instructions', got: %q", string(written))
	}
	_ = content
}

// TestUpdateInstructions_ReturnsOK verifies the response status field.
func TestUpdateInstructions_ReturnsOK(t *testing.T) {
	h, _ := newTestHandlerWithInstructions(t)

	body := `{"content": "some content"}`
	req := httptest.NewRequest(http.MethodPut, "/api/instructions", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateInstructions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", resp["status"])
	}
}

// TestReinitInstructions_Success verifies that reinit generates a file.
func TestReinitInstructions_Success(t *testing.T) {
	h, configDir := newTestHandlerWithInstructions(t)

	req := httptest.NewRequest(http.MethodPost, "/api/instructions/reinit", nil)
	w := httptest.NewRecorder()
	h.ReinitInstructions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["content"] == "" {
		t.Error("expected non-empty content after reinit")
	}

	// Verify the file was created.
	instPath := prompts.InstructionsFilePath(configDir, h.workspaces)
	if _, err := os.Stat(instPath); err != nil {
		t.Errorf("expected instructions file to exist after reinit: %v", err)
	}
}

// TestReinitInstructions_ContentType verifies JSON response headers.
func TestReinitInstructions_ContentType(t *testing.T) {
	h, _ := newTestHandlerWithInstructions(t)

	req := httptest.NewRequest(http.MethodPost, "/api/instructions/reinit", nil)
	w := httptest.NewRecorder()
	h.ReinitInstructions(w, req)

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json, got %q", ct)
	}
}

// --- strict JSON decoding ---

// TestUpdateInstructions_RejectsUnknownFields verifies that unknown JSON keys return 400.
func TestUpdateInstructions_RejectsUnknownFields(t *testing.T) {
	h, _ := newTestHandlerWithInstructions(t)
	body := `{"content": "# hello", "unknown_field": true}`
	req := httptest.NewRequest(http.MethodPut, "/api/instructions", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateInstructions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown fields, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateInstructions_WriteError verifies that UpdateInstructions returns 500
// when the instructions file cannot be written (e.g. directory is read-only).
func TestUpdateInstructions_WriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix shell")
	}
	h, configDir := newTestHandlerWithInstructions(t)

	// Make the instructions directory read-only so os.WriteFile fails.
	instDir := filepath.Join(configDir, "instructions")
	if err := os.Chmod(instDir, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Restore permissions after test so TempDir cleanup works.
	t.Cleanup(func() { _ = os.Chmod(instDir, 0755) })

	body := `{"content": "some content"}`
	req := httptest.NewRequest(http.MethodPut, "/api/instructions", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateInstructions(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when write fails, got %d: %s", w.Code, w.Body.String())
	}
}

// TestReinitInstructions_WriteError verifies that ReinitInstructions returns 500
// when the instructions directory cannot be written to.
func TestReinitInstructions_WriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix shell")
	}
	h, configDir := newTestHandlerWithInstructions(t)

	// Make the instructions directory read-only so instructions.Reinit fails.
	instDir := filepath.Join(configDir, "instructions")
	if err := os.Chmod(instDir, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(instDir, 0755) })

	req := httptest.NewRequest(http.MethodPost, "/api/instructions/reinit", nil)
	w := httptest.NewRecorder()
	h.ReinitInstructions(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when reinit write fails, got %d: %s", w.Code, w.Body.String())
	}
}
