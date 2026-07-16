package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeArtifactFixture builds a workspace whose artifacts/ dir holds a deck, a
// nested artifact, and a non-web file, plus a whitelisted file OUTSIDE the
// artifacts dir to probe containment.
func writeArtifactFixture(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	art := filepath.Join(ws, "artifacts")
	if err := os.MkdirAll(filepath.Join(art, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(art, "deck.html"):     "<title>DECK</title><h1>slides</h1>",
		filepath.Join(art, "sub", "r.html"): "<p>nested</p>",
		filepath.Join(art, "notes.go"):      "package main // not a web file",
		filepath.Join(ws, "escape.html"):    "<p>outside artifacts</p>",
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

func serveArtifact(t *testing.T, h *Handler, relPath string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/artifact/"+relPath, nil)
	req.SetPathValue("path", relPath)
	rec := httptest.NewRecorder()
	h.ServeArtifact(rec, req)
	return rec
}

func TestServeArtifact_ServesHTMLWithType(t *testing.T) {
	h := &Handler{workspaces: []string{writeArtifactFixture(t)}}
	rec := serveArtifact(t, h, "deck.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content-type = %q, want text/html; charset=utf-8", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("cache-control = %q, want no-store", cc)
	}
	if !strings.Contains(rec.Body.String(), "DECK") {
		t.Errorf("body missing artifact contents: %q", rec.Body.String())
	}
}

func TestServeArtifact_NestedPath(t *testing.T) {
	h := &Handler{workspaces: []string{writeArtifactFixture(t)}}
	rec := serveArtifact(t, h, "sub/r.html")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "nested") {
		t.Fatalf("nested artifact not served: status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestServeArtifact_RejectsTraversalOutOfRoot(t *testing.T) {
	// escape.html has a whitelisted extension but lives outside artifacts/, so
	// only os.OpenRoot containment (not the content-type policy) can stop it.
	h := &Handler{workspaces: []string{writeArtifactFixture(t)}}
	rec := serveArtifact(t, h, "../escape.html")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("traversal returned %d, want 404; body=%q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "outside artifacts") {
		t.Fatal("traversal leaked a file outside the artifacts root")
	}
}

func TestServeArtifact_RejectsNonWebExtension(t *testing.T) {
	h := &Handler{workspaces: []string{writeArtifactFixture(t)}}
	rec := serveArtifact(t, h, "notes.go")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-web file returned %d, want 404", rec.Code)
	}
}

func TestServeArtifact_MissingFileIs404(t *testing.T) {
	h := &Handler{workspaces: []string{writeArtifactFixture(t)}}
	if rec := serveArtifact(t, h, "nope.html"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing file returned %d, want 404", rec.Code)
	}
}

func TestServeArtifact_NoWorkspaceIs404(t *testing.T) {
	h := &Handler{}
	if rec := serveArtifact(t, h, "deck.html"); rec.Code != http.StatusNotFound {
		t.Fatalf("no-workspace returned %d, want 404", rec.Code)
	}
}

func TestListArtifacts_ListsWebFilesOnly(t *testing.T) {
	h := &Handler{workspaces: []string{writeArtifactFixture(t)}}
	req := httptest.NewRequest(http.MethodGet, "/api/artifacts", nil)
	rec := httptest.NewRecorder()
	h.ListArtifacts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp struct {
		Artifacts []ArtifactInfo `json:"artifacts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	paths := map[string]ArtifactInfo{}
	for _, a := range resp.Artifacts {
		paths[a.Path] = a
	}
	if _, ok := paths["deck.html"]; !ok {
		t.Errorf("deck.html not listed; got %v", paths)
	}
	if _, ok := paths["sub/r.html"]; !ok {
		t.Errorf("nested sub/r.html not listed; got %v", paths)
	}
	if _, ok := paths["notes.go"]; ok {
		t.Error("non-web notes.go should not be listed")
	}
	if a := paths["deck.html"]; a.URL != "/artifact/deck.html" {
		t.Errorf("deck url = %q, want /artifact/deck.html", a.URL)
	}
}

func TestListArtifacts_EmptyWhenNoDir(t *testing.T) {
	h := &Handler{workspaces: []string{t.TempDir()}} // no artifacts/ subdir
	req := httptest.NewRequest(http.MethodGet, "/api/artifacts", nil)
	rec := httptest.NewRecorder()
	h.ListArtifacts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"artifacts":[]`) {
		t.Errorf("expected empty artifacts array, got %q", rec.Body.String())
	}
}
