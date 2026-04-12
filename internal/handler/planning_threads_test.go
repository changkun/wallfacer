package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newPlannerHandlerWithThreads returns a handler backed by a planner
// whose ThreadManager is rooted at a temp directory. Tests use this to
// exercise the thread CRUD routes without spinning up a real sandbox.
func newPlannerHandlerWithThreads(t *testing.T) *Handler {
	t.Helper()
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	_ = p.Start(context.Background())
	h.planner = p
	return h
}

func TestListPlanningThreads_DefaultChat1(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/threads", nil)
	h.ListPlanningThreads(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Threads  []map[string]any `json:"threads"`
		ActiveID string           `json:"active_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Threads) != 1 {
		t.Fatalf("threads = %d, want 1", len(resp.Threads))
	}
	if resp.Threads[0]["name"] != "Chat 1" {
		t.Errorf("name = %v, want Chat 1", resp.Threads[0]["name"])
	}
	if resp.ActiveID == "" {
		t.Error("active_id should be set")
	}
}

func TestCreateRenameArchivePlanningThread(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)

	// Create.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/threads",
		strings.NewReader(`{"name":"Auth"}`))
	h.CreatePlanningThread(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)
	if id == "" {
		t.Fatal("created id is empty")
	}

	// Rename.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/planning/threads/"+id,
		strings.NewReader(`{"name":"Auth refactor"}`))
	req.SetPathValue("id", id)
	h.RenamePlanningThread(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d: %s", rec.Code, rec.Body.String())
	}

	// Archive.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/planning/threads/"+id+"/archive", nil)
	req.SetPathValue("id", id)
	h.ArchivePlanningThread(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status = %d: %s", rec.Code, rec.Body.String())
	}

	// List without archived should no longer include it.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/planning/threads", nil)
	h.ListPlanningThreads(rec, req)
	if strings.Contains(rec.Body.String(), id) {
		t.Errorf("archived thread still in non-archived list: %s", rec.Body.String())
	}

	// List with archived includes it.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/planning/threads?includeArchived=true", nil)
	h.ListPlanningThreads(rec, req)
	if !strings.Contains(rec.Body.String(), id) {
		t.Errorf("archived thread missing from includeArchived list: %s", rec.Body.String())
	}

	// Unarchive brings it back.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/planning/threads/"+id+"/unarchive", nil)
	req.SetPathValue("id", id)
	h.UnarchivePlanningThread(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d", rec.Code)
	}
}

func TestArchivePlanningThread_RejectsInFlight(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)
	threads := h.planner.Threads().List(false)
	id := threads[0].ID
	// Simulate an exec in flight owned by this thread.
	h.planner.SetBusy(true, id)
	defer h.planner.SetBusy(false, "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/planning/threads/"+id+"/archive", nil)
	req.SetPathValue("id", id)
	h.ArchivePlanningThread(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (busy)", rec.Code)
	}
}

func TestUndoPlanningRound_ThreadScoped(t *testing.T) {
	ws := initPlanningTestRepo(t)
	h := newStaticWorkspaceHandler(t, []string{ws})

	// Thread A writes round 1.
	writeSpec(t, ws, "a.md", "# a\n")
	seedPlanningCommitWithThread(t, ws, 1, "drafted a", "thread-a")
	// Thread B writes round 2 on top.
	writeSpec(t, ws, "b.md", "# b\n")
	seedPlanningCommitWithThread(t, ws, 2, "drafted b", "thread-b")

	// Undo from thread A — should revert A's round 1 even though B's
	// round 2 sits on top, thanks to the git revert flow.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/planning/undo?thread=thread-a", nil)
	h.UndoPlanningRound(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("undo status = %d: %s", rec.Code, rec.Body.String())
	}

	// a.md should be gone (revert removed it) but b.md should remain
	// (thread B's round 2 is untouched).
	subjects := gitLogSubjects(t, ws)
	var sawRevert bool
	for _, s := range subjects {
		if strings.Contains(s, "revert round 1") {
			sawRevert = true
		}
	}
	if !sawRevert {
		t.Errorf("expected a revert commit for round 1; subjects = %v", subjects)
	}
}

func TestUndoPlanningRound_NoCommitsForThread(t *testing.T) {
	ws := initPlanningTestRepo(t)
	h := newStaticWorkspaceHandler(t, []string{ws})

	// A commit exists but it belongs to a different thread.
	writeSpec(t, ws, "x.md", "# x\n")
	seedPlanningCommitWithThread(t, ws, 1, "drafted x", "thread-other")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/planning/undo?thread=thread-missing", nil)
	h.UndoPlanningRound(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

// seedPlanningCommitWithThread creates a planning commit carrying both
// Plan-Round and Plan-Thread trailers. Mirrors the output of the
// production commitPlanningRound path once the revert change landed.
func seedPlanningCommitWithThread(t *testing.T, ws string, round int, summary, threadID string) {
	t.Helper()
	runGit(t, ws, "add", "specs/")
	msg := fmt.Sprintf("specs(plan): %s\n\nseeded by test\n\nPlan-Round: %d\nPlan-Thread: %s",
		summary, round, threadID)
	runGit(t, ws, "commit", "-m", msg)
}
