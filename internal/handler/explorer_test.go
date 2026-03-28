package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestExplorerTree_Basic(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	// Create dirs and files in the workspace.
	for _, d := range []string{"Beta", "alpha"} {
		if err := os.Mkdir(filepath.Join(ws, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"zebra.txt", "apple.txt"} {
		if err := os.WriteFile(filepath.Join(ws, f), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/explorer/tree?path="+ws+"&workspace="+ws, nil)
	w := httptest.NewRecorder()
	h.ExplorerTree(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var entries []explorerEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}

	// Expect dirs first (case-insensitive: alpha, Beta), then files (apple.txt, zebra.txt).
	want := []string{"alpha", "Beta", "apple.txt", "zebra.txt"}
	if len(entries) != len(want) {
		t.Fatalf("expected %d entries, got %d: %+v", len(want), len(entries), entries)
	}
	for i, name := range want {
		if entries[i].Name != name {
			t.Errorf("entry[%d]: expected %q, got %q", i, name, entries[i].Name)
		}
	}

	// Verify types.
	if entries[0].Type != "dir" || entries[1].Type != "dir" {
		t.Error("first two entries should be dirs")
	}
	if entries[2].Type != "file" || entries[3].Type != "file" {
		t.Error("last two entries should be files")
	}

	// Files should have size > 0.
	if entries[2].Size == 0 {
		t.Error("file entry should have non-zero size")
	}
	// Dirs should omit size (zero value).
	if entries[0].Size != 0 {
		t.Error("dir entry should have zero size")
	}
}

func TestExplorerTree_HiddenEntries(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	if err := os.Mkdir(filepath.Join(ws, ".hidden"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".gitignore"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/explorer/tree?path="+ws+"&workspace="+ws, nil)
	w := httptest.NewRecorder()
	h.ExplorerTree(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var entries []explorerEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (hidden dir + hidden file), got %d", len(entries))
	}
	if entries[0].Name != ".hidden" || entries[0].Type != "dir" {
		t.Errorf("expected .hidden dir, got %+v", entries[0])
	}
	if entries[1].Name != ".gitignore" || entries[1].Type != "file" {
		t.Errorf("expected .gitignore file, got %+v", entries[1])
	}
}

func TestExplorerTree_MissingParams(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	tests := []struct {
		name string
		url  string
	}{
		{"missing both", "/api/explorer/tree"},
		{"missing path", "/api/explorer/tree?workspace=" + ws},
		{"missing workspace", "/api/explorer/tree?path=" + ws},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := httptest.NewRecorder()
			h.ExplorerTree(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestExplorerTree_WorkspaceNotConfigured(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	bogus := t.TempDir()
	req := httptest.NewRequest(http.MethodGet, "/api/explorer/tree?path="+bogus+"&workspace="+bogus, nil)
	w := httptest.NewRecorder()
	h.ExplorerTree(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIsWithinWorkspace_Valid(t *testing.T) {
	ws := t.TempDir()
	sub := filepath.Join(ws, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}

	got, err := isWithinWorkspace(sub, ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The resolved path should end with /sub.
	if filepath.Base(got) != "sub" {
		t.Errorf("expected resolved path ending in 'sub', got %q", got)
	}
}

func TestIsWithinWorkspace_TraversalAttack(t *testing.T) {
	ws := t.TempDir()
	// Create a sibling directory to traverse into.
	sibling := t.TempDir()

	// Construct a path that tries to escape via ..
	attack := filepath.Join(ws, "..", filepath.Base(sibling))
	_, err := isWithinWorkspace(attack, ws)
	if err == nil {
		t.Error("expected error for traversal attack, got nil")
	}
}

func TestIsWithinWorkspace_SymlinkEscape(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()

	// Create a symlink inside ws that points outside.
	link := filepath.Join(ws, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	_, err := isWithinWorkspace(link, ws)
	if err == nil {
		t.Error("expected error for symlink escape, got nil")
	}
}

func TestIsWithinWorkspace_ExactWorkspaceRoot(t *testing.T) {
	ws := t.TempDir()

	got, err := isWithinWorkspace(ws, ws)
	if err != nil {
		t.Fatalf("workspace root should be allowed, got error: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty resolved path")
	}
}
