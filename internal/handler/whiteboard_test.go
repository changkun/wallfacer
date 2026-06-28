package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newWhiteboardHandler returns a handler with an active workspace and a fresh
// scoped data directory, the state under which the whiteboard handlers resolve a
// real path. It returns the handler and the scoped data dir. The dir is
// overridden to an empty temp dir (rather than reusing the store dir) so
// saved-scene assertions are isolated.
func newWhiteboardHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	h := newStaticWorkspaceHandler(t, []string{t.TempDir()})
	dir := t.TempDir()
	h.snapshotMu.Lock()
	h.scopedDataDir = dir
	h.snapshotMu.Unlock()
	return h, dir
}

// TestGetWhiteboard_NoWorkspace returns 503 when no workspace is configured
// (empty ScopedDataDir).
func TestGetWhiteboard_NoWorkspace(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/whiteboard", nil)
	w := httptest.NewRecorder()
	h.GetWhiteboard(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

// TestPutWhiteboard_NoWorkspace returns 503 when no workspace is configured.
func TestPutWhiteboard_NoWorkspace(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/api/whiteboard", strings.NewReader(`{"elements":[]}`))
	w := httptest.NewRecorder()
	h.PutWhiteboard(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetWhiteboard_MissingFile returns an empty 200 body when no scene has been
// saved yet.
func TestGetWhiteboard_MissingFile(t *testing.T) {
	h, _ := newWhiteboardHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/whiteboard", nil)
	w := httptest.NewRecorder()
	h.GetWhiteboard(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Errorf("expected empty body, got %q", w.Body.String())
	}
}

// TestPutWhiteboard_EmptyBody returns 400 so a malformed save cannot clobber an
// existing scene.
func TestPutWhiteboard_EmptyBody(t *testing.T) {
	h, _ := newWhiteboardHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/api/whiteboard", strings.NewReader(""))
	w := httptest.NewRecorder()
	h.PutWhiteboard(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWhiteboard_RoundTrip saves a scene and verifies a subsequent GET returns
// the exact same bytes, and that the file lands in the scoped data dir.
func TestWhiteboard_RoundTrip(t *testing.T) {
	h, dir := newWhiteboardHandler(t)

	scene := `{"type":"excalidraw","elements":[{"id":"a"}],"appState":{}}`
	putReq := httptest.NewRequest(http.MethodPut, "/api/whiteboard", strings.NewReader(scene))
	putW := httptest.NewRecorder()
	h.PutWhiteboard(putW, putReq)

	if putW.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", putW.Code, putW.Body.String())
	}

	// The scene is persisted at <ScopedDataDir>/whiteboard.json verbatim.
	saved, err := os.ReadFile(filepath.Join(dir, "whiteboard.json"))
	if err != nil {
		t.Fatalf("read saved scene: %v", err)
	}
	if string(saved) != scene {
		t.Errorf("saved scene mismatch:\n got %q\nwant %q", saved, scene)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/whiteboard", nil)
	getW := httptest.NewRecorder()
	h.GetWhiteboard(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", getW.Code)
	}
	if getW.Body.String() != scene {
		t.Errorf("GET returned %q, want %q", getW.Body.String(), scene)
	}
	if ct := getW.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// TestWhiteboard_WorkspaceIsolation verifies that switching the scoped data dir
// (as a workspace switch does) isolates each workspace's scene.
func TestWhiteboard_WorkspaceIsolation(t *testing.T) {
	h := newStaticWorkspaceHandler(t, []string{t.TempDir()})

	dirA := t.TempDir()
	dirB := t.TempDir()
	setDir := func(d string) {
		h.snapshotMu.Lock()
		h.scopedDataDir = d
		h.snapshotMu.Unlock()
	}

	// Save scene A in workspace A.
	setDir(dirA)
	sceneA := `{"elements":[{"id":"A"}]}`
	putA := httptest.NewRecorder()
	h.PutWhiteboard(putA, httptest.NewRequest(http.MethodPut, "/api/whiteboard", strings.NewReader(sceneA)))
	if putA.Code != http.StatusOK {
		t.Fatalf("PUT A: expected 200, got %d", putA.Code)
	}

	// Workspace B has no scene yet.
	setDir(dirB)
	getB := httptest.NewRecorder()
	h.GetWhiteboard(getB, httptest.NewRequest(http.MethodGet, "/api/whiteboard", nil))
	if getB.Code != http.StatusOK || getB.Body.Len() != 0 {
		t.Fatalf("GET B: expected empty 200, got %d %q", getB.Code, getB.Body.String())
	}

	// Save a distinct scene in B, then confirm A is unchanged.
	sceneB := `{"elements":[{"id":"B"}]}`
	putB := httptest.NewRecorder()
	h.PutWhiteboard(putB, httptest.NewRequest(http.MethodPut, "/api/whiteboard", strings.NewReader(sceneB)))
	if putB.Code != http.StatusOK {
		t.Fatalf("PUT B: expected 200, got %d", putB.Code)
	}

	setDir(dirA)
	getA := httptest.NewRecorder()
	h.GetWhiteboard(getA, httptest.NewRequest(http.MethodGet, "/api/whiteboard", nil))
	if getA.Body.String() != sceneA {
		t.Errorf("workspace A scene leaked: got %q, want %q", getA.Body.String(), sceneA)
	}
}
