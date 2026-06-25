package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"latere.ai/x/wallfacer/internal/spec"
)

func TestStaleCandidates_FlagsChangedAffects(t *testing.T) {
	repo := initPlanningTestRepo(t)
	h, ws := newTestHandlerWithWorkspacesFromRepo(t, repo)

	// A code file under internal/x/, committed now (commit date = today).
	if err := os.MkdirAll(filepath.Join(ws, "internal/x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "internal/x/foo.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, ws, "add", "internal/x/foo.go")
	runGit(t, ws, "commit", "-m", "add foo.go")

	// A complete spec that affects foo.go, last updated long before the commit.
	writeFanoutSpec(t, ws, "done.md", "complete", "shipped", nil, []string{"internal/x/foo.go"})
	runGit(t, ws, "add", "specs/")
	runGit(t, ws, "commit", "-m", "seed spec")

	got := fetchStaleCandidates(t, h)
	if len(got) != 1 || got[0].Path != "specs/done.md" {
		t.Fatalf("candidates = %+v, want only specs/done.md", got)
	}

	// Bump the spec's updated date past every commit; it is no longer flagged.
	if err := spec.UpdateFrontmatter(filepath.Join(ws, "specs/done.md"), map[string]any{
		"updated": "2099-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	if got := fetchStaleCandidates(t, h); len(got) != 0 {
		t.Errorf("after updated bump, candidates = %+v, want none", got)
	}
}

func fetchStaleCandidates(t *testing.T, h *Handler) []spec.StaleCandidate {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/specs/stale-candidates", nil)
	w := httptest.NewRecorder()
	h.StaleCandidates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Candidates []spec.StaleCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Candidates
}
