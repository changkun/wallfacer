package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/agentsession"
	"latere.ai/x/wallfacer/internal/store"
)

// TestListTasks_Empty verifies that an empty store returns an empty JSON array.
func TestListTasks_Empty(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var tasks []store.Task
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

// TestListTasks_IncludesCreated verifies that created tasks appear in the list.
func TestListTasks_IncludesCreated(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	_, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task one", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task two", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var tasks []store.Task
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestListTasks_InvalidFailureCategory(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?failure_category=not-a-category", nil)
	w := httptest.NewRecorder()

	h.ListTasks(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestListTasks_ExcludesArchivedByDefault checks that archived tasks are not
// returned when include_archived is not set.
func TestListTasks_ExcludesArchivedByDefault(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "archived task", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	_ = h.store.SetTaskArchived(ctx, task.ID, true)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)

	var tasks []store.Task
	_ = json.NewDecoder(w.Body).Decode(&tasks)

	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks (archived excluded), got %d", len(tasks))
	}
}

// TestListTasks_IncludeArchived checks that archived tasks appear when requested.
func TestListTasks_IncludeArchived(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "archived task", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	_ = h.store.SetTaskArchived(ctx, task.ID, true)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks?include_archived=true", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)

	var tasks []store.Task
	_ = json.NewDecoder(w.Body).Decode(&tasks)

	if len(tasks) != 1 {
		t.Errorf("expected 1 task (archived included), got %d", len(tasks))
	}
}

func TestListTasks_ArchivedPaged(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	archivedIDs := make([]uuid.UUID, 0, 5)
	for i := range 5 {
		task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: fmt.Sprintf("archived %d", i), Timeout: 15})
		if err != nil {
			t.Fatal(err)
		}
		_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

		_ = h.store.SetTaskArchived(ctx, task.ID, true)

		archivedIDs = append(archivedIDs, task.ID)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks?include_archived=true&archived_page_size=2", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var page1 struct {
		Tasks         []store.Task `json:"tasks"`
		TotalArchived int          `json:"total_archived"`
		HasMoreBefore bool         `json:"has_more_before"`
		HasMoreAfter  bool         `json:"has_more_after"`
		BeforeCursor  string       `json:"before_cursor"`
		AfterCursor   string       `json:"after_cursor"`
	}
	if err := json.NewDecoder(w.Body).Decode(&page1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if page1.TotalArchived != 5 {
		t.Fatalf("expected total_archived=5, got %d", page1.TotalArchived)
	}
	if len(page1.Tasks) != 2 {
		t.Fatalf("expected 2 tasks in first page, got %d", len(page1.Tasks))
	}
	if !page1.HasMoreBefore {
		t.Fatal("expected has_more_before=true on first page")
	}
	if page1.HasMoreAfter {
		t.Fatal("expected has_more_after=false on first page")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/tasks?include_archived=true&archived_page_size=2&archived_before="+page1.BeforeCursor, nil)
	w2 := httptest.NewRecorder()
	h.ListTasks(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var page2 struct {
		Tasks         []store.Task `json:"tasks"`
		TotalArchived int          `json:"total_archived"`
		HasMoreBefore bool         `json:"has_more_before"`
		HasMoreAfter  bool         `json:"has_more_after"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if len(page2.Tasks) != 2 {
		t.Fatalf("expected 2 tasks in second page, got %d", len(page2.Tasks))
	}
	if !page2.HasMoreAfter {
		t.Fatal("expected has_more_after=true for page loaded with archived_before")
	}
	for _, t1 := range page1.Tasks {
		for _, t2 := range page2.Tasks {
			if t1.ID == t2.ID {
				t.Fatalf("task %s appeared in both pages", t1.ID)
			}
		}
	}
	_ = archivedIDs
}

func TestListTasks_ArchivedPagedRejectsInvalidCursor(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?include_archived=true&archived_page_size=5&archived_before=not-a-uuid", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestCreateTask_RejectsEmptyPrompt verifies that an empty prompt returns 400.
func TestCreateTask_RejectsEmptyPrompt(t *testing.T) {
	h := newTestHandler(t)
	body := `{"prompt": "   "}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestCreateTask_PersistsCriteria verifies the create request's criteria field
// is persisted on the task.
func TestCreateTask_PersistsCriteria(t *testing.T) {
	h := newTestHandler(t)
	body := `{"prompt": "build a thing", "criteria": "run make test; /health returns 200", "timeout": 30}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task store.Task
	if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if task.Criteria != "run make test; /health returns 200" {
		t.Errorf("criteria not persisted on create: %q", task.Criteria)
	}
}

// TestUpdateTask_PersistsCriteriaForBacklog verifies a PATCH sets criteria on a
// backlog task.
func TestUpdateTask_PersistsCriteriaForBacklog(t *testing.T) {
	h := newTestHandler(t)
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "p", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	body := `{"criteria": "verify the migration is reversible"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Criteria != "verify the migration is reversible" {
		t.Errorf("criteria not persisted via PATCH: %q", got.Criteria)
	}
}

// TestCreateTask_RejectsInvalidJSON verifies that bad JSON returns 400.
func TestCreateTask_RejectsInvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader("{bad json"))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestCreateTask_Success verifies that a valid request creates a task.
func TestCreateTask_Success(t *testing.T) {
	h := newTestHandler(t)
	body := `{"prompt": "build a thing", "timeout": 30}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task store.Task
	if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if task.Prompt != "build a thing" {
		t.Errorf("expected prompt 'build a thing', got %q", task.Prompt)
	}
	if task.Status != store.TaskStatusBacklog {
		t.Errorf("expected status backlog, got %s", task.Status)
	}
}

// TestCreateTask_RejectsSandboxField covers the per-task sandbox field
// on POST: it is rejected, and the error names PATCH /api/tasks/{id},
// where a task's harness is set after creation.
func TestCreateTask_RejectsSandboxField(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	body := `{"prompt": "build a thing", "sandbox": "codex"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PATCH") {
		t.Errorf("expected error body to reference PATCH migration path; got %q", w.Body.String())
	}
}

// TestCreateTask_RejectsSandboxByActivity is the sibling guard for the
// per-activity sandbox map: rejected on POST, with the error naming
// PATCH /api/tasks/{id} as the place to set it.
func TestCreateTask_RejectsSandboxByActivity(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	body := `{"prompt": "build a thing", "sandbox_by_activity": {"implementation": "codex"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PATCH") {
		t.Errorf("expected error body to name the PATCH path; got %q", w.Body.String())
	}
}

// TestPatchTask_RejectsCodexWhenUntested preserves the coverage
// of the sandbox-readiness gate under the new workflow: callers
// POST without a sandbox field, then PATCH to set it.
func TestPatchTask_RejectsCodexWhenUntested(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	reqEnv := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(`{"openai_api_key":"sk-test"}`))
	wEnv := httptest.NewRecorder()
	h.UpdateEnvConfig(wEnv, reqEnv)
	if wEnv.Code != http.StatusNoContent {
		t.Fatalf("expected env update 204, got %d: %s", wEnv.Code, wEnv.Body.String())
	}
	// Create a backlog task, then try to patch sandbox=codex before
	// codex has been validated via the test probe.
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt: "build a thing", Timeout: 15,
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	patch := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(),
		strings.NewReader(`{"sandbox":"codex"}`))
	rec := httptest.NewRecorder()
	h.UpdateTask(rec, patch, task.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestPatchTask_AllowsCodexWithHostAuthCache mirrors the
// host-auth-cache readiness path on PATCH: when codex auth is
// already cached on disk, setting sandbox=codex via PATCH goes
// through without 400 and the store reflects the new sandbox.
func TestPatchTask_AllowsCodexWithHostAuthCache(t *testing.T) {
	h, _, _ := newTestHandlerWithEnvAndCodexAuth(t)

	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt: "build a thing", Timeout: 15,
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	patch := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(),
		strings.NewReader(`{"sandbox":"codex"}`))
	rec := httptest.NewRecorder()
	h.UpdateTask(rec, patch, task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, _ := h.store.GetTask(context.Background(), task.ID)
	if updated.Sandbox != "codex" {
		t.Errorf("expected sandbox 'codex', got %q", updated.Sandbox)
	}
}

// TestPatchTask_RejectsWaitingToInProgress verifies that a bare PATCH moving a
// waiting task to in_progress is rejected with 400 rather than flipping the
// status (which would consume a concurrency slot without launching a worker,
// leaving the task stuck in_progress). Resuming must go through resume/test.
func TestPatchTask_RejectsWaitingToInProgress(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "build a thing", Timeout: 15,
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(),
		strings.NewReader(`{"status":"in_progress"}`))
	rec := httptest.NewRecorder()
	h.UpdateTask(rec, patch, task.ID)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.Status != store.TaskStatusWaiting {
		t.Errorf("status flipped despite rejection: got %q, want waiting", updated.Status)
	}
}

// TestUpdateTask_NotFound verifies that updating a non-existent task returns 404.
func TestUpdateTask_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	body := `{"position": 0}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+id.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestUpdateTask_InvalidJSON verifies that bad JSON returns 400.
func TestUpdateTask_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestUpdateTask_UpdatesPosition verifies that position can be updated.
func TestUpdateTask_UpdatesPosition(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	body := `{"position": 5}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)

	if updated.Position != 5 {
		t.Errorf("expected position 5, got %d", updated.Position)
	}
}

// TestUpdateTask_UpdatesBacklogFields verifies that prompt/timeout can be updated for backlog tasks.
func TestUpdateTask_UpdatesBacklogFields(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "original prompt", Timeout: 15})

	body := `{"prompt": "new prompt", "timeout": 60}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)

	if updated.Prompt != "new prompt" {
		t.Errorf("expected 'new prompt', got %q", updated.Prompt)
	}
	if updated.Timeout != 60 {
		t.Errorf("expected timeout 60, got %d", updated.Timeout)
	}
}

// TestUpdateTask_RetryFromDone verifies that a done task can be retried to backlog.
func TestUpdateTask_RetryFromDone(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	body := `{"status": "backlog"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)

	if updated.Status != store.TaskStatusBacklog {
		t.Errorf("expected backlog, got %s", updated.Status)
	}
}

// TestUpdateTask_RetryFromFailed verifies that a failed task can be retried.
func TestUpdateTask_RetryFromFailed(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	body := `{"status": "backlog"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)

	if updated.Status != store.TaskStatusBacklog {
		t.Errorf("expected backlog, got %s", updated.Status)
	}
}

// TestUpdateTask_RetryWithNewPrompt verifies that retry can supply a new prompt.
func TestUpdateTask_RetryWithNewPrompt(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "old prompt", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCancelled)

	body := `{"status": "backlog", "prompt": "new retry prompt"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)

	if updated.Prompt != "new retry prompt" {
		t.Errorf("expected 'new retry prompt', got %q", updated.Prompt)
	}
}

// TestDeleteTask_NotFound verifies that deleting a non-existent task returns 500.
func TestDeleteTask_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/"+id.String(), nil)
	w := httptest.NewRecorder()
	h.DeleteTask(w, req, id)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for non-existent task, got %d", w.Code)
	}
}

// TestDeleteTask_Success verifies that an existing task is deleted.
func TestDeleteTask_Success(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "to delete", Timeout: 15})

	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/"+task.ID.String(), nil)
	w := httptest.NewRecorder()
	h.DeleteTask(w, req, task.ID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	tasks, _ := h.store.ListTasks(ctx, false)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks after delete, got %d", len(tasks))
	}
}

// --- GetEvents backward-compatibility and pagination tests ---

// TestGetEvents_Empty verifies that no events returns an empty array.
func TestGetEvents_Empty(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var events []store.TaskEvent
	_ = json.NewDecoder(w.Body).Decode(&events)

	// Events may include the initial state_change from CreateTask in the handler.
	// We just verify the response is valid JSON.
	if events == nil {
		t.Error("expected non-nil events slice")
	}
}

// TestGetEvents_ContainsInserted verifies that inserted events are returned.
func TestGetEvents_ContainsInserted(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, map[string]string{

		"from": "backlog",
		"to":   "in_progress",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var events []store.TaskEvent
	_ = json.NewDecoder(w.Body).Decode(&events)

	if len(events) == 0 {
		t.Error("expected at least one event")
	}
	found := false
	for _, ev := range events {
		if ev.EventType == store.EventTypeStateChange {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected state_change event")
	}
}

// TestGetEvents_BackwardCompatNoParams verifies that calling without params returns
// a plain JSON array (not a paged envelope), preserving backward compatibility.
func TestGetEvents_BackwardCompatNoParams(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, "hello")

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Must decode as a plain array, not an object.
	var events []store.TaskEvent
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatalf("expected plain JSON array: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected at least one event")
	}
}

// TestGetEvents_PagedEnvelopeWithLimitParam verifies that providing limit= returns
// the paginated envelope shape.
func TestGetEvents_PagedEnvelopeWithLimitParam(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	for i := range 5 {
		_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, i)

	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events?limit=3", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp eventsPageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(resp.Events) != 3 {
		t.Errorf("expected 3 events in page, got %d", len(resp.Events))
	}
	if !resp.HasMore {
		t.Error("expected HasMore=true")
	}
}

// TestGetEvents_PagedAfterCursor verifies that the after= cursor excludes earlier events.
func TestGetEvents_PagedAfterCursor(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	for i := range 5 {
		_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, i)

	}

	// Get first page.
	req1 := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events?limit=3", nil)
	w1 := httptest.NewRecorder()
	h.GetEvents(w1, req1, task.ID)

	var page1 eventsPageResponse
	_ = json.NewDecoder(w1.Body).Decode(&page1)

	// Use cursor from page1 to get page2.
	url2 := fmt.Sprintf("/api/tasks/%s/events?after=%d&limit=10", task.ID.String(), page1.NextAfter)
	req2 := httptest.NewRequest(http.MethodGet, url2, nil)
	w2 := httptest.NewRecorder()
	h.GetEvents(w2, req2, task.ID)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for page2, got %d", w2.Code)
	}
	var page2 eventsPageResponse
	_ = json.NewDecoder(w2.Body).Decode(&page2)

	// All events in page2 must have IDs strictly greater than the cursor.
	for _, ev := range page2.Events {
		if ev.ID <= page1.NextAfter {
			t.Errorf("event ID %d should be > cursor %d", ev.ID, page1.NextAfter)
		}
	}
	if page2.HasMore {
		t.Error("expected HasMore=false for last page")
	}
}

// TestGetEvents_TypeFilter verifies that the types= param filters by event type.
func TestGetEvents_TypeFilter(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, "state")

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, "out1")

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeError, "err")

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, "out2")

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?types=output", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp eventsPageResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	for _, ev := range resp.Events {
		if ev.EventType != store.EventTypeOutput {
			t.Errorf("unexpected event type %q, want output", ev.EventType)
		}
	}
	if resp.TotalFiltered != 2 {
		t.Errorf("TotalFiltered = %d, want 2", resp.TotalFiltered)
	}
}

// TestGetEvents_MultiTypeFilter verifies comma-separated types= filter.
func TestGetEvents_MultiTypeFilter(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, "state")

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, "out")

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeError, "err")

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?types=state_change,error", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp eventsPageResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.TotalFiltered != 2 {
		t.Errorf("TotalFiltered = %d, want 2", resp.TotalFiltered)
	}
	for _, ev := range resp.Events {
		if ev.EventType != store.EventTypeStateChange && ev.EventType != store.EventTypeError {
			t.Errorf("unexpected event type %q", ev.EventType)
		}
	}
}

// TestGetEvents_BadAfterParam verifies 400 for a non-integer after= value.
func TestGetEvents_BadAfterParam(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?after=notanumber", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestGetEvents_NegativeAfterParam verifies 400 for a negative after= value.
func TestGetEvents_NegativeAfterParam(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?after=-1", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative after, got %d", w.Code)
	}
}

// TestGetEvents_BadLimitParam verifies 400 for a non-integer limit= value.
func TestGetEvents_BadLimitParam(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?limit=abc", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestGetEvents_ZeroLimitParam verifies 400 for limit=0.
func TestGetEvents_ZeroLimitParam(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?limit=0", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for limit=0, got %d", w.Code)
	}
}

// TestGetEvents_LimitCappedAt1000 verifies that limit > 1000 is accepted but capped.
func TestGetEvents_LimitCappedAt1000(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	for i := range 3 {
		_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, i)

	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?limit=9999", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	// Should succeed (limit is silently capped, not rejected).
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp eventsPageResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Events) == 0 {
		t.Error("expected some events")
	}
}

// TestGetEvents_UnknownTypeParam verifies 400 for an unknown event type.
func TestGetEvents_UnknownTypeParam(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?types=bogus_type", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown type, got %d", w.Code)
	}
}

// TestGetEvents_AllValidTypes verifies that all known event types are accepted.
func TestGetEvents_AllValidTypes(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	allTypes := "state_change,output,error,system,feedback,span_start,span_end"
	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?types="+allTypes, nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for all valid types, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetEvents_EmptyTypesParamNoFilter verifies that ?types= (empty) is treated
// as no type filter (returns all event types).
func TestGetEvents_EmptyTypesParamNoFilter(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, "s")

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, "o")

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?types=", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp eventsPageResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.TotalFiltered < 2 {
		t.Errorf("expected at least 2 events (no type filter), got TotalFiltered=%d", resp.TotalFiltered)
	}
}

// TestGetEvents_HasMoreFalseWhenAllFit verifies HasMore=false when results fit in one page.
func TestGetEvents_HasMoreFalseWhenAllFit(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	for i := range 3 {
		_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, i)

	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?limit=100", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	var resp eventsPageResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.HasMore {
		t.Error("expected HasMore=false when all events fit in one page")
	}
}

// TestGetEvents_TotalFilteredAccountsForTypeFilter verifies TotalFiltered counts
// only type-matching events, not all events.
func TestGetEvents_TotalFilteredAccountsForTypeFilter(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	for i := range 6 {
		_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, i)

	}
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeError, "e")

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?types=output&limit=2", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	var resp eventsPageResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Events) != 2 {
		t.Errorf("expected 2 events in page, got %d", len(resp.Events))
	}
	if resp.TotalFiltered != 6 {
		t.Errorf("TotalFiltered = %d, want 6 (output-only count)", resp.TotalFiltered)
	}
	if !resp.HasMore {
		t.Error("expected HasMore=true (6 output events, limit=2)")
	}
}

// TestServeOutput_NotFound returns 404 for a missing file.
func TestServeOutput_NotFound(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/outputs/turn-0001.json", nil)
	w := httptest.NewRecorder()
	h.ServeOutput(w, req, task.ID, "turn-0001.json")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestServeOutput_PathTraversal verifies that path traversal filenames are rejected.
func TestServeOutput_PathTraversal(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/outputs/../secret", nil)
	w := httptest.NewRecorder()
	h.ServeOutput(w, req, task.ID, "../secret")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal, got %d", w.Code)
	}
}

// TestServeOutput_WithSlash verifies that filenames with slashes are rejected.
func TestServeOutput_WithSlash(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/outputs/sub/file.json", nil)
	w := httptest.NewRecorder()
	h.ServeOutput(w, req, task.ID, "sub/file.json")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for filename with slash, got %d", w.Code)
	}
}

// TestServeOutput_JSONFile verifies that a JSON file is served with the correct content type.
func TestServeOutput_JSONFile(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	// Manually create the outputs directory and a turn file.
	outputsDir := filepath.Join(h.store.DataDir(), task.ID.String(), "outputs")
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(outputsDir, "turn-0001.json")
	if err := os.WriteFile(filePath, []byte(`{"turn": 1}`), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/outputs/turn-0001.json", nil)
	w := httptest.NewRecorder()
	h.ServeOutput(w, req, task.ID, "turn-0001.json")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json content-type, got %q", ct)
	}
}

// --- PATCH depends_on tests ---

// --- Trigger attribution tests ---

// TestSubmitFeedback_EventHasFeedbackTrigger verifies that the state_change
// event emitted by SubmitFeedback contains "trigger": "feedback".
func TestSubmitFeedback_EventHasFeedbackTrigger(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	body := `{"message": "please continue"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/feedback", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.SubmitFeedback(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Find the state_change event that records waiting → in_progress.
	events, err := h.store.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.EventType != store.EventTypeStateChange {
			continue
		}
		var data map[string]string
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		if data["from"] == string(store.TaskStatusWaiting) && data["to"] == string(store.TaskStatusInProgress) {
			found = true
			if data["trigger"] != string(store.TriggerFeedback) {
				t.Errorf("expected trigger=%q, got %q", store.TriggerFeedback, data["trigger"])
			}
		}
	}
	if !found {
		t.Error("expected a state_change event from waiting → in_progress, none found")
	}
}

// --- strict JSON decoding ---

// TestCreateTask_RejectsUnknownFields verifies that unknown JSON keys return 400.
func TestCreateTask_RejectsUnknownFields(t *testing.T) {
	h := newTestHandler(t)
	body := `{"prompt": "build a thing", "unknown_field": true}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown fields, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateTask_RejectsTrailingContent verifies that trailing data after the
// JSON object returns 400.
func TestCreateTask_RejectsTrailingContent(t *testing.T) {
	h := newTestHandler(t)
	body := `{"prompt": "build a thing"} extra garbage`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for trailing content, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateTask_RejectsUnknownFields verifies that unknown JSON keys return 400.
func TestUpdateTask_RejectsUnknownFields(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})

	body := `{"position": 0, "sneaky_field": "value"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown fields, got %d: %s", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Model override
// ─────────────────────────────────────────────────────────────────────────────

// TestUpdateTask_PatchModelOverride verifies that PATCH with {"model":"claude-haiku-4-5"}
// persists ModelOverride on the task.
func TestUpdateTask_PatchModelOverride(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test model override", Timeout: 15})

	body := `{"model":"claude-haiku-4-5"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelOverride == nil {
		t.Fatal("expected ModelOverride to be set, got nil")
	}
	if *got.ModelOverride != "claude-haiku-4-5" {
		t.Errorf("expected ModelOverride 'claude-haiku-4-5', got %q", *got.ModelOverride)
	}
}

// TestUpdateTask_PatchModelOverrideClear verifies that PATCH with {"model":""}
// clears ModelOverride (sets to nil).
func TestUpdateTask_PatchModelOverrideClear(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test model clear", Timeout: 15})
	// Pre-set a model override.
	if err := h.store.UpdateTaskModelOverride(ctx, task.ID, "claude-opus-4-6"); err != nil {
		t.Fatalf("pre-set model override: %v", err)
	}

	body := `{"model":""}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelOverride != nil {
		t.Errorf("expected ModelOverride to be nil after clearing, got %q", *got.ModelOverride)
	}
}

// TestListTasks_ModelOverrideSerialized verifies that a task with ModelOverride set
// serializes model_override in the GET /api/tasks response.
func TestListTasks_ModelOverrideSerialized(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test serialize model", Timeout: 15})
	if err := h.store.UpdateTaskModelOverride(ctx, task.ID, "claude-haiku-4-5"); err != nil {
		t.Fatalf("set model override: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var tasks []store.Task
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ModelOverride == nil {
		t.Fatal("expected model_override to be set in response, got nil")
	}
	if *tasks[0].ModelOverride != "claude-haiku-4-5" {
		t.Errorf("expected model_override 'claude-haiku-4-5', got %q", *tasks[0].ModelOverride)
	}
}

// --- Atomic creation tests ---

// TestCreateTask_EmitsOnlyOneDelta verifies that creating a fully configured
// task (sandbox + budget + schedule) emits exactly one SSE delta, not one per
// post-create update call.
func TestCreateTask_EmitsOnlyOneDelta(t *testing.T) {
	h := newTestHandler(t)

	subID, deltaCh := h.store.Subscribe()
	defer h.store.Unsubscribe(subID)

	body, _ := json.Marshal(map[string]any{
		"prompt":           "delta count test",
		"max_cost_usd":     3.0,
		"max_input_tokens": 500,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Drain the channel for a short window and count deltas.
	count := 0
	deadline := time.After(50 * time.Millisecond)
drain:
	for {
		select {
		case <-deltaCh:
			count++
		case <-deadline:
			break drain
		}
	}

	if count != 1 {
		t.Errorf("expected exactly 1 SSE delta for task creation, got %d", count)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SearchTasks
// ─────────────────────────────────────────────────────────────────────────────

// TestSearchTasks_ShortQuery verifies that q < 2 runes returns 400.
func TestSearchTasks_ShortQuery(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/search?q=a", nil)
	w := httptest.NewRecorder()
	h.SearchTasks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short query, got %d", w.Code)
	}
}

// TestSearchTasks_EmptyQuery verifies that an empty q returns 400.
func TestSearchTasks_EmptyQuery(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/search", nil)
	w := httptest.NewRecorder()
	h.SearchTasks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty query, got %d", w.Code)
	}
}

// TestSearchTasks_NoResults verifies that a valid query with no matches returns an empty array.
func TestSearchTasks_NoResults(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/search?q=nonexistent", nil)
	w := httptest.NewRecorder()
	h.SearchTasks(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var results []any
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestSearchTasks_MatchesPrompt verifies that a query matching a task's prompt is returned.
func TestSearchTasks_MatchesPrompt(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	_, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "fix authentication bug in login", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/search?q=authentication", nil)
	w := httptest.NewRecorder()
	h.SearchTasks(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var results []any
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least 1 result for matching query")
	}
}

// TestUpdateTask_SetTags verifies that tags can be updated via PATCH.
func TestUpdateTask_SetTags(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "tagged task", Timeout: 15})

	body := `{"tags": ["alpha", "beta", "gamma"]}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)

	if len(updated.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(updated.Tags), updated.Tags)
	}
	if updated.Tags[0] != "alpha" || updated.Tags[1] != "beta" || updated.Tags[2] != "gamma" {
		t.Errorf("unexpected tags: %v", updated.Tags)
	}
}

// TestUpdateTask_SetTagsEmpty verifies that an empty tags slice clears existing tags.
func TestUpdateTask_SetTagsEmpty(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "tagged task", Timeout: 15})

	// First, set some tags.
	_ = h.store.UpdateTaskTags(ctx, task.ID, []string{"old-tag"})

	body := `{"tags": []}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)

	if len(updated.Tags) != 0 {
		t.Errorf("expected empty tags, got %v", updated.Tags)
	}
}

// TestUpdateTask_WaitingBudgetUpdate verifies that budget limits can be raised
// for a task in the waiting state.
func TestUpdateTask_WaitingBudgetUpdate(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "waiting task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	maxCost := 10.0
	body := fmt.Sprintf(`{"max_cost_usd": %g}`, maxCost)
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)

	if updated.MaxCostUSD != maxCost {
		t.Errorf("expected MaxCostUSD %g, got %g", maxCost, updated.MaxCostUSD)
	}
}

// TestUpdateTask_RetryFromWaiting verifies that a waiting task can be retried to backlog.
func TestUpdateTask_RetryFromWaiting(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "waiting task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	body := `{"status": "backlog"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)

	if updated.Status != store.TaskStatusBacklog {
		t.Errorf("expected backlog, got %s", updated.Status)
	}
}

// TestUpdateTask_InvalidStatusTransition verifies that an invalid status transition
// returns 400.
func TestUpdateTask_InvalidStatusTransition(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 15})
	// Task is in backlog; transition directly to done is invalid.
	body := `{"status": "done"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid transition backlog->done, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateTask_FreshStartRetry verifies that a retry with fresh_start=true
// clears the session and resets to backlog.
func TestUpdateTask_FreshStartRetry(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "original", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	body := `{"status": "backlog", "fresh_start": true}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)

	if updated.Status != store.TaskStatusBacklog {
		t.Errorf("expected backlog, got %s", updated.Status)
	}
	if !updated.FreshStart {
		t.Error("expected FreshStart to be true after fresh_start retry")
	}
}

// TestCreateTask_RejectsFlowField asserts that the task create request no
// longer carries a fleet: a body naming one is rejected as an unknown field
// and no task is stored.
func TestCreateTask_RejectsFlowField(t *testing.T) {
	h := newTestHandler(t)
	body := `{"prompt": "do this", "flow": "custom-fleet", "timeout": 5}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `unknown field "flow"`) {
		t.Errorf("body = %q, want the unknown-field rejection", w.Body.String())
	}
	tasks, err := h.store.ListTasks(context.Background(), true)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("stored %d tasks, want none for rejected requests", len(tasks))
	}
}

func TestPatchTask_RejectedWhenLocked(t *testing.T) {
	h := newAgentSessionHandlerWithThreads(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "lock test"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Pin the thread to this task and start an in-flight turn.
	tm := h.agentSession.Sessions()
	threads := tm.List(false)
	if len(threads) == 0 {
		t.Fatal("expected at least one thread")
	}
	threadID := threads[0].ID
	cs, _ := tm.Store(threadID)
	_ = cs.SaveSession(agentsession.ResumeInfo{FocusedTask: task.ID.String()})
	h.agentSession.SetBusy(true, threadID)
	defer h.agentSession.SetBusy(false, "")

	// Attempt to move the task to in_progress — should be rejected with 409.
	body := `{"status":"in_progress"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	req.SetPathValue("id", task.ID.String())
	h.UpdateTask(rec, req, task.ID)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["thread_id"] != threadID {
		t.Errorf("thread_id = %q, want %q", resp["thread_id"], threadID)
	}
	if !strings.Contains(resp["error"], "in-flight") {
		t.Errorf("error message = %q, want mention of in-flight", resp["error"])
	}

	// Task must still be in backlog.
	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusBacklog {
		t.Errorf("task status = %q, want backlog after rejected transition", got.Status)
	}
}

// TestCreateTask_ReturnsBudgetInBody verifies that the 201 response body
// already contains the budget fields without requiring a subsequent fetch.
func TestCreateTask_ReturnsBudgetInBody(t *testing.T) {
	h := newTestHandler(t)

	body, _ := json.Marshal(map[string]any{
		"prompt":           "atomic creation test",
		"max_cost_usd":     5.5,
		"max_input_tokens": 1000,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task store.Task
	if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if task.MaxCostUSD != 5.5 {
		t.Errorf("max_cost_usd: want 5.5, got %v", task.MaxCostUSD)
	}
	if task.MaxInputTokens != 1000 {
		t.Errorf("max_input_tokens: want 1000, got %d", task.MaxInputTokens)
	}
}
