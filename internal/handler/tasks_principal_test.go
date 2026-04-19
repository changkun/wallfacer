package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"changkun.de/x/wallfacer/internal/auth"
	"changkun.de/x/wallfacer/internal/store"
)

// withClaims returns a request whose context carries the given claims,
// as if cloud-mode JWT middleware had already validated the caller.
// Used in place of signing a real JWT for handler-level tests.
func withClaims(r *http.Request, c *auth.Claims) *http.Request {
	return r.WithContext(auth.WithClaims(r.Context(), c))
}

// TestCreateTask_PopulatesPrincipalFields confirms that when the
// handler sees claims in context, the created task records the
// caller's sub and org_id. Anonymous creation leaves both empty,
// matching today's on-disk layout.
func TestCreateTask_PopulatesPrincipalFields(t *testing.T) {
	h := newTestHandler(t)

	body := bytes.NewBufferString(`{"prompt":"hello","timeout":60}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", body)
	req = withClaims(req, &auth.Claims{Sub: "user-abc", OrgID: "org-42"})
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var got store.Task
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.CreatedBy != "user-abc" {
		t.Errorf("CreatedBy = %q, want user-abc", got.CreatedBy)
	}
	if got.OrgID != "org-42" {
		t.Errorf("OrgID = %q, want org-42", got.OrgID)
	}
}

// TestCreateTask_AnonymousLeavesPrincipalFieldsEmpty covers the local
// and Phase-1-cloud call paths that don't carry claims yet: the task
// must be indistinguishable from a pre-Phase-2 record.
func TestCreateTask_AnonymousLeavesPrincipalFieldsEmpty(t *testing.T) {
	h := newTestHandler(t)

	body := bytes.NewBufferString(`{"prompt":"hello","timeout":60}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", body)
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	var got store.Task
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.CreatedBy != "" || got.OrgID != "" {
		t.Errorf("anonymous create leaked principal: CreatedBy=%q OrgID=%q", got.CreatedBy, got.OrgID)
	}
}

// TestListTasks_OrgScopedFiltering confirms that GET /api/tasks with
// claims in context only returns tasks whose OrgID matches. A legacy
// anonymous task and a task from a different org stay hidden.
func TestListTasks_OrgScopedFiltering(t *testing.T) {
	h := newTestHandler(t)

	// Pre-populate: one anonymous, two orgA, one orgB.
	for _, opts := range []store.TaskCreateOptions{
		{Prompt: "anon", Timeout: 60},
		{Prompt: "alice1", Timeout: 60, OrgID: "org-a", CreatedBy: "alice"},
		{Prompt: "alice2", Timeout: 60, OrgID: "org-a", CreatedBy: "alice"},
		{Prompt: "bob", Timeout: 60, OrgID: "org-b", CreatedBy: "bob"},
	} {
		if _, err := h.store.CreateTaskWithOptions(t.Context(), opts); err != nil {
			t.Fatalf("CreateTaskWithOptions: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req = withClaims(req, &auth.Claims{Sub: "alice", OrgID: "org-a"})
	w := httptest.NewRecorder()
	h.ListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var tasks []store.Task
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("orgA view returned %d tasks, want 2", len(tasks))
	}
	for _, task := range tasks {
		if task.OrgID != "org-a" {
			t.Errorf("leaked task OrgID=%q into org-a view", task.OrgID)
		}
	}
}

// TestListTasks_LocalModeSeesEverything confirms the local / anonymous
// call path keeps today's behavior: no claims means no filtering.
func TestListTasks_LocalModeSeesEverything(t *testing.T) {
	h := newTestHandler(t)
	for _, opts := range []store.TaskCreateOptions{
		{Prompt: "anon", Timeout: 60},
		{Prompt: "orgA", Timeout: 60, OrgID: "org-a"},
		{Prompt: "orgB", Timeout: 60, OrgID: "org-b"},
	} {
		if _, err := h.store.CreateTaskWithOptions(t.Context(), opts); err != nil {
			t.Fatalf("CreateTaskWithOptions: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil) // no claims
	w := httptest.NewRecorder()
	h.ListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var tasks []store.Task
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("local-mode list returned %d tasks, want 3", len(tasks))
	}
}
