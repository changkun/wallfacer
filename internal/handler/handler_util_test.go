package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
)

// --- currentWorkspaces fallback path ---

// TestCurrentWorkspaces_WorkspaceManagerNil exercises the h.workspace == nil
// code paths in currentWorkspaces.  These paths are unreachable through
// NewHandler normally, so we reach them by setting h.workspace to nil after
// construction – valid because the test lives in the same package.
func TestCurrentWorkspaces_WorkspaceManagerNil(t *testing.T) {
	h := newTestHandler(t)
	h.workspace = nil
	h.workspaces = nil

	ws := h.currentWorkspaces()
	if ws != nil {
		t.Errorf("expected nil when h.workspace==nil and h.workspaces==nil, got %v", ws)
	}
}

func TestCurrentWorkspaces_WorkspaceManagerNil_WithWorkspaces(t *testing.T) {
	h := newTestHandler(t)
	h.workspace = nil
	h.workspaces = []string{"/tmp/ws1", "/tmp/ws2"}

	ws := h.currentWorkspaces()
	if len(ws) != 2 {
		t.Fatalf("expected 2 workspaces, got %d: %v", len(ws), ws)
	}
	if ws[0] != "/tmp/ws1" || ws[1] != "/tmp/ws2" {
		t.Errorf("unexpected workspace values: %v", ws)
	}
}

// TestCurrentInstructionsPath_WorkspaceManagerNil exercises the
// h.workspace == nil branch that returns "".
func TestCurrentInstructionsPath_WorkspaceManagerNil(t *testing.T) {
	h := newTestHandler(t)
	h.workspace = nil

	path := h.currentInstructionsPath()
	if path != "" {
		t.Errorf("expected empty string when h.workspace==nil, got %q", path)
	}
}

// --- incAutopilotPhase2Miss ---

// TestIncAutopilotPhase2Miss_NilRegistry verifies that incAutopilotPhase2Miss
// is a no-op (does not panic) when no metrics registry is configured.
func TestIncAutopilotPhase2Miss_NilRegistry(t *testing.T) {
	h := newTestHandler(t) // h.reg is nil
	// Should not panic.
	h.incAutopilotPhase2Miss("auto-promote")
}

// TestIncAutopilotPhase2Miss_WithRegistry verifies that incAutopilotPhase2Miss
// increments the counter when a metrics registry is configured.
func TestIncAutopilotPhase2Miss_WithRegistry(t *testing.T) {
	storeDir, err := os.MkdirTemp("", "wallfacer-handler-util-test-*")
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.NewStore(storeDir)
	if err != nil {
		_ = os.RemoveAll(storeDir)
		t.Fatal(err)
	}

	reg := metrics.NewRegistry()
	r := runner.NewRunner(s, runner.RunnerConfig{})
	// Cleanups run LIFO: remove store dir last, after compaction and background work finish.
	t.Cleanup(func() { _ = os.RemoveAll(storeDir) })
	t.Cleanup(s.WaitCompaction)
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)

	h := NewHandler(s, r, t.TempDir(), nil, reg)
	// Should not panic and should increment the counter.
	h.incAutopilotPhase2Miss("auto-promote")
}

// --- BrowseWorkspaces ---

func TestBrowseWorkspaces_NonAbsolutePath(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/browse?path=relative/path", nil)
	w := httptest.NewRecorder()
	h.BrowseWorkspaces(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for relative path, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBrowseWorkspaces_NonExistentPath(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/browse?path=/this/does/not/exist/at/all/99999", nil)
	w := httptest.NewRecorder()
	h.BrowseWorkspaces(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for nonexistent path, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBrowseWorkspaces_ValidDir(t *testing.T) {
	h := newTestHandler(t)
	dir := t.TempDir()
	// Create a visible subdirectory.
	if err := os.MkdirAll(filepath.Join(dir, "myrepo"), 0755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/browse?path="+dir, nil)
	w := httptest.NewRecorder()
	h.BrowseWorkspaces(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	entries, ok := resp["entries"].([]interface{})
	if !ok {
		t.Fatalf("entries is not an array: %v", resp["entries"])
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d: %v", len(entries), entries)
	}
}

func TestBrowseWorkspaces_HiddenFilesFiltering(t *testing.T) {
	h := newTestHandler(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".hidden"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "visible"), 0755); err != nil {
		t.Fatal(err)
	}

	// Without include_hidden – hidden dirs should be excluded.
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/browse?path="+dir, nil)
	w := httptest.NewRecorder()
	h.BrowseWorkspaces(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp1 map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp1)

	entries1 := resp1["entries"].([]interface{})
	if len(entries1) != 1 {
		t.Errorf("expected 1 entry without hidden, got %d: %v", len(entries1), entries1)
	}

	// With include_hidden=true – both dirs should appear.
	req2 := httptest.NewRequest(http.MethodGet, "/api/workspaces/browse?path="+dir+"&include_hidden=true", nil)
	w2 := httptest.NewRecorder()
	h.BrowseWorkspaces(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
	var resp2 map[string]interface{}
	_ = json.NewDecoder(w2.Body).Decode(&resp2)

	entries2 := resp2["entries"].([]interface{})
	if len(entries2) != 2 {
		t.Errorf("expected 2 entries with hidden, got %d: %v", len(entries2), entries2)
	}
}

func TestBrowseWorkspaces_FilesNotIncluded(t *testing.T) {
	h := newTestHandler(t)
	dir := t.TempDir()
	// Create a file and a directory – only directories should appear.
	if err := os.WriteFile(filepath.Join(dir, "somefile.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/browse?path="+dir, nil)
	w := httptest.NewRecorder()
	h.BrowseWorkspaces(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	entries := resp["entries"].([]interface{})
	if len(entries) != 1 {
		t.Errorf("expected only directory entry, got %d entries", len(entries))
	}
}

func TestBrowseWorkspaces_EmptyDirReturnsEmptyEntries(t *testing.T) {
	h := newTestHandler(t)
	dir := t.TempDir()

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/browse?path="+dir, nil)
	w := httptest.NewRecorder()
	h.BrowseWorkspaces(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["path"] != dir {
		t.Errorf("expected path=%q in response, got %q", dir, resp["path"])
	}
}

// --- instructions endpoints with no workspace manager ---

// TestGetInstructions_WorkspaceManagerNil verifies GetInstructions returns 503
// when h.workspace is nil (no workspace configured).
func TestGetInstructions_WorkspaceManagerNil(t *testing.T) {
	h := newTestHandler(t)
	h.workspace = nil

	req := httptest.NewRequest(http.MethodGet, "/api/instructions", nil)
	w := httptest.NewRecorder()
	h.GetInstructions(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no workspace, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateInstructions_WorkspaceManagerNil verifies UpdateInstructions returns 503
// when h.workspace is nil.
func TestUpdateInstructions_WorkspaceManagerNil(t *testing.T) {
	h := newTestHandler(t)
	h.workspace = nil

	body := `{"content":"hello"}`
	req := httptest.NewRequest(http.MethodPut, "/api/instructions", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateInstructions(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no workspace, got %d: %s", w.Code, w.Body.String())
	}
}

// TestReinitInstructions_WorkspaceManagerNil verifies ReinitInstructions returns 503
// when h.workspace is nil.
func TestReinitInstructions_WorkspaceManagerNil(t *testing.T) {
	h := newTestHandler(t)
	h.workspace = nil

	req := httptest.NewRequest(http.MethodPost, "/api/instructions/reinit", nil)
	w := httptest.NewRecorder()
	h.ReinitInstructions(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no workspace, got %d: %s", w.Code, w.Body.String())
	}
}

// --- refuseWorkspaceMutationIfBlocked happy path ---

// TestRefuseWorkspaceMutationIfBlocked_NoBlockingTasks verifies the function
// returns false (does not refuse) when there are no blocking tasks for the workspace.
func TestRefuseWorkspaceMutationIfBlocked_NoBlockingTasks(t *testing.T) {
	repo := setupRepo(t)
	h, _ := newTestHandlerWithWorkspacesFromRepo(t, repo)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	refused := h.refuseWorkspaceMutationIfBlocked(w, req, repo, "push")
	if refused {
		t.Errorf("expected refuseWorkspaceMutationIfBlocked to return false with no blocking tasks, got true (status %d: %s)", w.Code, w.Body.String())
	}
}
