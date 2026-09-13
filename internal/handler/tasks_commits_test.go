package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/store"
)

func TestTaskCommitsAreTaskScoped(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	a, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "b"})
	if err != nil {
		t.Fatal(err)
	}
	for id, patch := range map[uuid.UUID]string{a.ID: "+task a", b.ID: "+task b"} {
		if err := h.store.AppendTaskCommits(id, []store.TaskCommit{{Repository: "/repo", Hash: "123", Patch: patch, Turn: 1, Attempt: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	h.TaskCommits(w, httptest.NewRequest(http.MethodGet, "/api/tasks/"+a.ID.String()+"/commits", nil), a.ID)
	var commits []store.TaskCommit
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &commits); err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].Patch != "+task a" {
		t.Fatal("another task's commits leaked into response")
	}
	w = httptest.NewRecorder()
	h.TaskCommits(w, httptest.NewRequest(http.MethodGet, "/api/tasks/missing/commits", nil), uuid.New())
	if w.Code != 404 {
		t.Fatalf("missing task status: %d", w.Code)
	}
}

func TestTaskCommitsEmptyAndNoWorkspace(t *testing.T) {
	h := newTestHandler(t)
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.TaskCommits(w, httptest.NewRequest(http.MethodGet, "/api/tasks/commits", nil), task.ID)
	if w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatalf("empty response: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	(&Handler{}).TaskCommits(w, httptest.NewRequest(http.MethodGet, "/api/tasks/commits", nil), task.ID)
	if w.Code != 503 {
		t.Fatalf("no-workspace status: %d", w.Code)
	}
}
