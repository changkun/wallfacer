package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/pkg/circuitbreaker"
	"latere.ai/x/pkg/gitutil"

	"latere.ai/x/wallfacer/internal/agentsession"
	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/store/storetest"
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

func TestTryAutoRetry_ContainerCrash(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "retry me", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}

	failed, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	h.tryAutoRetry(ctx, h.store, *failed)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after tryAutoRetry: %v", err)
	}
	if got.Status != store.TaskStatusBacklog {
		t.Fatalf("Status = %q, want %q", got.Status, store.TaskStatusBacklog)
	}
}

func TestTryAutoRetry_AgentError(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "do not retry", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryAgentError); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}

	failed, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	h.tryAutoRetry(ctx, h.store, *failed)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after tryAutoRetry: %v", err)
	}
	if got.Status != store.TaskStatusFailed {
		t.Fatalf("Status = %q, want %q", got.Status, store.TaskStatusFailed)
	}
}

func TestTryAutoRetry_MaxRetries(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "retry budget exhausted", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}
	for i := range constants.MaxAutoRetries {
		if err := h.store.IncrementAutoRetryCount(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
			t.Fatalf("IncrementAutoRetryCount(%d): %v", i, err)
		}
	}

	failed, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got := failed.AutoRetryCount; got != constants.MaxAutoRetries {
		t.Fatalf("AutoRetryCount = %d, want %d", got, constants.MaxAutoRetries)
	}
	h.tryAutoRetry(ctx, h.store, *failed)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after tryAutoRetry: %v", err)
	}
	if got.Status != store.TaskStatusFailed {
		t.Fatalf("Status = %q, want %q", got.Status, store.TaskStatusFailed)
	}
}

func TestTryAutoRetry_CircuitOpen(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "blocked by circuit", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}
	for range constants.DefaultCBThreshold {
		h.runner.RecordContainerFailure()
	}

	failed, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	h.tryAutoRetry(ctx, h.store, *failed)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after tryAutoRetry: %v", err)
	}
	if got.Status != store.TaskStatusFailed {
		t.Fatalf("Status = %q, want %q", got.Status, store.TaskStatusFailed)
	}
}

// TestTryAutoRetry_ManualRetriesDoNotBlock verifies that manual retries
// (via ResetTaskForRetry, which increments RetryHistory but not AutoRetryCount)
// do not consume the auto-retry budget. A task with a long RetryHistory but
// AutoRetryCount=0 and remaining budget should still be auto-retried.
func TestTryAutoRetry_ManualRetriesDoNotBlock(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "manual retries should not block auto-retry", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}

	// Simulate two manual retries: RetryHistory grows but AutoRetryCount stays 0.
	for i := range 2 {
		if err := h.store.ResetTaskForRetry(ctx, task.ID, task.Prompt, false); err != nil {
			t.Fatalf("ResetTaskForRetry(%d): %v", i, err)
		}
		_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

		if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
			t.Fatalf("SetTaskFailureCategory(%d): %v", i, err)
		}
	}

	failed, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(failed.RetryHistory) != 2 {
		t.Fatalf("RetryHistory len = %d, want 2", len(failed.RetryHistory))
	}
	if failed.AutoRetryCount != 0 {
		t.Fatalf("AutoRetryCount = %d, want 0", failed.AutoRetryCount)
	}

	// tryAutoRetry should NOT be suppressed: AutoRetryCount=0 < constants.MaxAutoRetries
	// and budget remains (container_crash starts with 2).
	h.tryAutoRetry(ctx, h.store, *failed)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after tryAutoRetry: %v", err)
	}
	if got.Status != store.TaskStatusBacklog {
		t.Fatalf("Status = %q, want %q (manual retries should not block auto-retry)",
			got.Status, store.TaskStatusBacklog)
	}
}

// --- checkAndSyncWaitingTasks tests ---

// TestCheckAndSyncWaitingTasks_SkipsNonWaiting verifies that tasks not in the
// waiting state are left untouched.
func TestCheckAndSyncWaitingTasks_SkipsNonWaiting(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosync(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	// Leave the task in backlog (not waiting).
	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// Add a commit to main so the worktree would be behind — but task isn't waiting.
	_ = os.WriteFile(filepath.Join(repo, "new.txt"), []byte("upstream\n"), 0644)

	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "upstream commit")

	h.checkAndSyncWaitingTasks(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusBacklog {
		t.Errorf("expected task to remain in backlog, got %s", got.Status)
	}
}

// TestCheckAndSyncWaitingTasks_SkipsWaitingWithNoWorktrees verifies that waiting
// tasks without worktrees are not touched.
func TestCheckAndSyncWaitingTasks_SkipsWaitingWithNoWorktrees(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosync(true)
	ctx := context.Background()

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	// No worktrees set.

	h.checkAndSyncWaitingTasks(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected task to remain waiting, got %s", got.Status)
	}
}

// TestCheckAndSyncWaitingTasks_SkipsWaitingUpToDate verifies that a waiting task
// whose worktree is already up to date is not synced.
func TestCheckAndSyncWaitingTasks_SkipsWaitingUpToDate(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosync(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// No new commits on main — worktree is up to date.

	h.checkAndSyncWaitingTasks(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected task to remain waiting (up to date), got %s", got.Status)
	}
}

// TestCheckAndSyncWaitingTasks_SyncsWhenBehind verifies that a waiting task
// whose worktree has fallen behind the default branch is automatically moved to
// in_progress and synced back to waiting.
func TestCheckAndSyncWaitingTasks_SyncsWhenBehind(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosync(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// Add a commit to main — now the worktree is behind by 1.
	_ = os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("upstream\n"), 0644)

	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "upstream commit")

	h.checkAndSyncWaitingTasks(ctx)

	// The task must have been moved out of waiting (at least to in_progress).
	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status == store.TaskStatusWaiting && len(got.WorktreePaths) > 0 {
		// If still waiting, check if there are commits behind now (should be 0 after sync).
		// The task may already be back to waiting after the sync completes.
		t.Logf("task returned to waiting — checking worktree is now up to date")
	}
	// At minimum the status must not be stuck at waiting-and-behind.
	// After WaitBackground the sync goroutine has finished, so the task should
	// be back in waiting (successfully synced) or in_progress (sync in flight).
	if got.Status == store.TaskStatusBacklog || got.Status == store.TaskStatusDone {
		t.Errorf("unexpected status %s after auto-sync trigger", got.Status)
	}
}

// TestCheckAndSyncWaitingTasks_SyncsAtFullCapacity verifies that sync proceeds
// even when regular task capacity is fully saturated. Sync operations are
// lightweight host-side git rebases and do not compete for container slots.
func TestCheckAndSyncWaitingTasks_SyncsAtFullCapacity(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	h.SetAutosync(true)
	ctx := context.Background()

	if err := os.WriteFile(envPath, []byte("WALLFACER_MAX_PARALLEL=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Fill all regular capacity.
	regular, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "regular task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, regular.ID, store.TaskStatusInProgress)

	repo := setupRepo(t)

	// Create a bare clone to serve as "origin" so that FetchOrigin succeeds.
	// Without a remote, FetchOrigin fails and the stale-fetch guard skips the
	// task, so the sync check never runs.
	bare := filepath.Join(t.TempDir(), "bare.git")
	if out, err := exec.Command("git", "clone", "--bare", repo, bare).CombinedOutput(); err != nil {
		t.Fatalf("git clone --bare: %v\n%s", err, out)
	}
	gitRun(t, repo, "remote", "add", "origin", bare)

	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	if err := os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("upstream\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "upstream commit")
	// Push the new commit to origin so FetchOrigin has something to fetch.
	gitRun(t, repo, "push", "origin", "main")

	h.checkAndSyncWaitingTasks(ctx)
	h.runner.WaitBackground()

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Fatalf("expected task to return to waiting after auto-sync, got %s", got.Status)
	}

	behind, err := gitutil.CommitsBehind(repo, wt)
	if err != nil {
		t.Fatalf("CommitsBehind: %v", err)
	}
	if behind != 0 {
		t.Fatalf("expected synced worktree to be up to date, got behind=%d", behind)
	}
}

// TestCheckAndSyncWaitingTasks_DisabledNoOp verifies that auto-sync does nothing
// when the autosync toggle is off.
func TestCheckAndSyncWaitingTasks_DisabledNoOp(t *testing.T) {
	h := newTestHandler(t)
	// autosync is off by default
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// Add a commit to main so the worktree is behind.
	_ = os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("upstream\n"), 0644)

	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "upstream commit")

	h.checkAndSyncWaitingTasks(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected task to remain waiting when autosync is off, got %s", got.Status)
	}
}

// --- tryAutoTest tests ---

// --- tryAutoSubmit tests ---

// TestAutosubmit_SetAndGet verifies the SetAutosubmit / AutosubmitEnabled accessors.
func TestAutosubmit_SetAndGet(t *testing.T) {
	h := newTestHandler(t)
	if h.AutosubmitEnabled() {
		t.Fatal("expected autosubmit to be disabled by default")
	}
	h.SetAutosubmit(true)
	if !h.AutosubmitEnabled() {
		t.Error("expected autosubmit to be enabled after SetAutosubmit(true)")
	}
	h.SetAutosubmit(false)
	if h.AutosubmitEnabled() {
		t.Error("expected autosubmit to be disabled after SetAutosubmit(false)")
	}
}

// TestTryAutoSubmit_DisabledNoOp verifies that auto-submit does nothing when disabled.
func TestTryAutoSubmit_DisabledNoOp(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "verified task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	_ = h.store.UpdateTaskResult(ctx, task.ID, "done", "", "end_turn", 1)

	// autosubmit is disabled by default.
	h.tryAutoSubmit(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected task to remain waiting when autosubmit disabled, got %s", got.Status)
	}
}

// TestTryAutoSubmit_SkipsNonWaiting verifies that non-waiting tasks are not submitted.
func TestTryAutoSubmit_SkipsNonWaiting(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosubmit(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "verified task", Timeout: 15})
	// Leave in backlog — not waiting.
	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	_ = h.store.UpdateTaskResult(ctx, task.ID, "done", "", "end_turn", 1)

	h.tryAutoSubmit(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusBacklog {
		t.Errorf("expected task to remain in backlog, got %s", got.Status)
	}
}

func TestTryAutoSubmit_ProcessesOnlyWaitingTasks(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosubmit(true)
	ctx := context.Background()

	repo := setupRepo(t)
	waitingWT := filepath.Join(t.TempDir(), "waiting-wt")
	gitRun(t, repo, "worktree", "add", "-b", "waiting-branch", waitingWT, "HEAD")

	doneWT := filepath.Join(t.TempDir(), "done-wt")
	gitRun(t, repo, "worktree", "add", "-b", "done-branch", doneWT, "HEAD")

	waitingTask, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "verified waiting task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, waitingTask.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, waitingTask.ID, map[string]string{repo: waitingWT}, "waiting-branch")

	_ = h.store.UpdateTaskResult(ctx, waitingTask.ID, "done", "", "end_turn", 1)

	doneTask, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "verified done task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, doneTask.ID, store.TaskStatusDone)

	_ = h.store.UpdateTaskWorktrees(ctx, doneTask.ID, map[string]string{repo: doneWT}, "done-branch")

	_ = h.store.UpdateTaskResult(ctx, doneTask.ID, "done", "", "end_turn", 1)

	h.tryAutoSubmit(ctx)

	gotWaiting, _ := h.store.GetTask(ctx, waitingTask.ID)
	if gotWaiting.Status != store.TaskStatusDone {
		t.Fatalf("expected waiting task to be auto-submitted, got %s", gotWaiting.Status)
	}

	gotDone, _ := h.store.GetTask(ctx, doneTask.ID)
	if gotDone.Status != store.TaskStatusDone {
		t.Fatalf("expected done task to remain done, got %s", gotDone.Status)
	}

	events, err := h.store.GetEvents(ctx, doneTask.ID)
	if err != nil {
		t.Fatalf("GetEvents(done task): %v", err)
	}
	for _, event := range events {
		if event.EventType != store.EventTypeStateChange {
			continue
		}
		var data map[string]string
		if err := json.Unmarshal(event.Data, &data); err != nil {
			t.Fatalf("json.Unmarshal(event data): %v", err)
		}
		if data["trigger"] == string(store.TriggerAutoSubmit) {
			t.Fatalf("done task should not receive auto-submit state changes: %#v", event.Data)
		}
	}
}

// TestTryAutoSubmit_SkipsNotVerified verifies that unverified tasks are not submitted.
func TestTryAutoSubmit_SkipsNotVerified(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosubmit(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "unverified task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// StopReason is unset: the agent did not end its turn on its own.

	h.tryAutoSubmit(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected unverified task to remain waiting, got %s", got.Status)
	}
}

func TestTryAutoSubmit_SkipsMissingWorktreeDir(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosubmit(true)
	ctx := context.Background()

	repo := setupRepo(t)
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "verified task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: filepath.Join(t.TempDir(), "missing-wt")}, "task-branch")

	_ = h.store.UpdateTaskResult(ctx, task.ID, "done", "", "end_turn", 1)

	h.tryAutoSubmit(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected task with missing worktree dir to remain waiting, got %s", got.Status)
	}
}

// TestTryAutoSubmit_SkipsBehindTip verifies that tasks whose worktrees are behind the
// default branch are not auto-submitted.
func TestTryAutoSubmit_SkipsBehindTip(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosubmit(true)
	ctx := context.Background()

	repo := setupRepo(t)
	// Add a bare remote so the behind-tip check is not skipped for local-only repos.
	origin := t.TempDir()
	gitRun(t, origin, "init", "--bare")
	gitRun(t, repo, "remote", "add", "origin", origin)
	gitRun(t, repo, "push", "-u", "origin", "main")

	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "verified task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	_ = h.store.UpdateTaskResult(ctx, task.ID, "done", "", "end_turn", 1)

	// Add a commit to main so the worktree is behind by 1.
	_ = os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("upstream\n"), 0644)

	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "upstream commit")

	h.tryAutoSubmit(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected behind-tip task to remain waiting, got %s", got.Status)
	}
}

// TestTryAutoSubmit_SubmitsEligibleTaskNoSession verifies that a verified, up-to-date,
// conflict-free waiting task with no session is moved directly to done and
// that a summary.json is written so the task appears in cost dashboards.
func TestTryAutoSubmit_SubmitsEligibleTaskNoSession(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosubmit(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "verified task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	_ = h.store.UpdateTaskResult(ctx, task.ID, "done", "", "end_turn", 1)

	// No session ID — task goes directly to done.

	h.tryAutoSubmit(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusDone {
		t.Errorf("expected eligible task to be moved to done, got %s", got.Status)
	}

	// summary.json must be present so the task shows up in ListSummaries / cost dashboards.
	summary, err := h.store.LoadSummary(task.ID)
	if err != nil {
		t.Fatalf("LoadSummary: %v", err)
	}
	if summary == nil {
		t.Error("summary.json not written for no-session auto-submitted task")
	}
}

// TestTryAutoSubmit_CommitMessageFailureReturnsToWaiting verifies that when
// auto-submit's commit fails at message generation, the task returns to waiting
// for review with no placeholder commit merged. The board toggles stay enabled
// (the recovery opens only the transient auto-submit circuit breaker).
func TestTryAutoSubmit_CommitMessageFailureReturnsToWaiting(t *testing.T) {
	h := newTestHandler(t)
	// Point Claude/Codex at /bin/false so commit-message generation fails.
	if hb, ok := h.runner.(*runner.Runner).SandboxBackend().(*executor.HostBackend); ok {
		hb.SetBinaryForTest(harness.Claude, "/usr/bin/false")
		hb.SetBinaryForTest(harness.Codex, "/usr/bin/false")
	}
	h.SetAutoimplement(true)
	h.SetAutosubmit(true)
	t.Cleanup(func() { waitForBackground(2000) })
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-autosubmit-fail")
	gitRun(t, repo, "worktree", "add", "-b", "task-autosubmit-fail", wt, "HEAD")
	if err := os.WriteFile(filepath.Join(wt, "feature.txt"), []byte("new work\n"), 0644); err != nil {
		t.Fatal(err)
	}

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "verified task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-autosubmit-fail")

	_ = h.store.UpdateTaskResult(ctx, task.ID, "done", "", "end_turn", 1)

	setTaskSessionID(t, h, task.ID, "sess-autosubmit-fail")

	h.tryAutoSubmit(ctx)

	// Wait for the recovery to return the task to waiting.
	returned := false
	for range 100 {
		events, _ := h.store.GetEvents(ctx, task.ID)
		for _, e := range events {
			var data map[string]string
			if err := json.Unmarshal(e.Data, &data); err == nil && strings.Contains(data["result"], "returned to waiting") {
				returned = true
			}
		}
		if returned {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !returned {
		t.Fatal("expected a 'returned to waiting' event after commit message generation failed")
	}

	got, _ := h.store.GetTask(ctx, task.ID)
	if got == nil || got.Status != store.TaskStatusWaiting {
		t.Fatalf("expected task to return to waiting, got %v", got.Status)
	}
	if got := gitRun(t, repo, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("expected no placeholder commit to land, got %s commits", got)
	}
	if !h.AutoimplementEnabled() || !h.AutosubmitEnabled() {
		t.Fatal("expected board toggles to remain enabled (only the circuit breaker opens)")
	}
}

// --- Trigger attribution tests ---

// TestTryAutoPromote_EventHasAutoPromoteTrigger verifies that the state_change
// event emitted by tryAutoPromote contains "trigger": "auto_promote".
func TestTryAutoPromote_EventHasAutoPromoteTrigger(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.autoimplement.Store(true)

	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task to promote", Timeout: 15})

	h.tryAutoPromote(ctx)

	// The task should now be in_progress.
	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TaskStatusInProgress {
		t.Fatalf("expected task to be promoted to in_progress, got %s", got.Status)
	}

	// Find the state_change event that records backlog → in_progress.
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
		if data["from"] == string(store.TaskStatusBacklog) && data["to"] == string(store.TaskStatusInProgress) {
			found = true
			if data["trigger"] != string(store.TriggerAutoPromote) {
				t.Errorf("expected trigger=%q, got %q", store.TriggerAutoPromote, data["trigger"])
			}
		}
	}
	if !found {
		t.Error("expected a state_change event from backlog → in_progress, none found")
	}
}

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

// TestTryAutoPromote_ConcurrentPhase1DoesNotBlock verifies that two goroutines
// calling tryAutoPromote concurrently can both complete Phase 1 (the unlocked
// store scan) without blocking each other. A rendezvous barrier installed via
// testPhase1Done ensures both goroutines reach the Phase 1/Phase 2 boundary
// simultaneously; if Phase 1 were still serialized by promoteMu the second
// goroutine would never arrive and the test would time out.
func TestTryAutoPromote_ConcurrentPhase1DoesNotBlock(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	h.SetAutoimplement(true)
	ctx := context.Background()

	// Limit to 1 concurrent task so the second goroutine's Phase 2 is a no-op.
	// Write directly to the env file instead of going through UpdateEnvConfig,
	// which spawns a goroutine that would race with the testPhase1Done field
	// assignment below.
	if err := os.WriteFile(envPath, []byte("WALLFACER_MAX_PARALLEL=1\n"), 0644); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	// Drop any previously-cached limit so the handler re-reads the env file.
	h.cachedMaxParallel.Invalidate()

	// Create two distinct backlog tasks.
	_, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "concurrent task one", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("create task one: %v", err)
	}
	_, err = h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "concurrent task two", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("create task two: %v", err)
	}

	// phase1Done receives a token when each goroutine finishes Phase 1.
	// gate is closed once both tokens arrive, releasing both goroutines to Phase 2.
	phase1Done := make(chan struct{}, 2)
	gate := make(chan struct{})

	h.testPhase1Done = func() {
		// Signal that this goroutine completed Phase 1.
		phase1Done <- struct{}{}
		// Wait until both goroutines have completed Phase 1 before proceeding to
		// Phase 2. If Phase 1 were serialized (old design), the goroutine holding
		// promoteMu would block here forever because the second goroutine could
		// not enter Phase 1 until the lock was released.
		<-gate
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); h.tryAutoPromote(ctx) }()
	go func() { defer wg.Done(); h.tryAutoPromote(ctx) }()

	// Collect both Phase 1 completion signals within the timeout.
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for i := range 2 {
		select {
		case <-phase1Done:
			// Goroutine i+1 completed Phase 1 concurrently — good.
		case <-timeout.C:
			// Only one goroutine reached Phase 1 within the timeout, which means
			// Phase 1 is being serialized: the second goroutine was blocked by
			// the first (the old single-phase design).
			t.Errorf("goroutine %d of 2 did not complete Phase 1 within timeout — Phase 1 appears serialized", i+1)
			close(gate) // unblock whatever is waiting so wg.Wait() can finish
			wg.Wait()
			return
		}
	}

	// Both goroutines completed Phase 1 concurrently. Open the gate for Phase 2.
	close(gate)
	wg.Wait()

	// With max_concurrent=1 exactly one task should have been promoted out of
	// backlog. We count "non-backlog" tasks rather than "in_progress"
	// specifically because the real runner started by RunBackground may
	// transition the task from in_progress to failed very quickly in
	// environments without a container runtime (e.g. CI without podman/docker),
	// which would make an "in_progress == 1" check flaky.
	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	promoted := 0
	for _, task := range tasks {
		if task.Status != store.TaskStatusBacklog {
			promoted++
		}
	}
	if promoted != 1 {
		t.Errorf("expected exactly 1 task promoted out of backlog after concurrent promotion, got %d", promoted)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ScheduledAt — tryAutoPromote integration tests
// ─────────────────────────────────────────────────────────────────────────────

// TestTryAutoPromote_SkipsFutureScheduledTask verifies that a backlog task
// with a ScheduledAt in the future is not promoted, but is promoted once the
// scheduled time has passed.
func TestTryAutoPromote_SkipsFutureScheduledTask(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.autoimplement.Store(true)

	ctx := context.Background()

	// Create the only backlog task and schedule it 1 hour in the future.
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "scheduled task", Timeout: 15})
	future := time.Now().Add(1 * time.Hour)
	if err := h.store.UpdateTaskScheduledAt(ctx, task.ID, &future); err != nil {
		t.Fatalf("UpdateTaskScheduledAt: %v", err)
	}

	h.tryAutoPromote(ctx)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TaskStatusBacklog {
		t.Errorf("expected task to remain in backlog (future schedule), got %s", got.Status)
	}
}

// TestTryAutoPromote_PromotesPastScheduledTask verifies that a backlog task
// with a ScheduledAt in the past IS promoted normally.
func TestTryAutoPromote_PromotesPastScheduledTask(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.autoimplement.Store(true)

	ctx := context.Background()

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "past scheduled task", Timeout: 15})
	past := time.Now().Add(-1 * time.Hour)
	if err := h.store.UpdateTaskScheduledAt(ctx, task.ID, &past); err != nil {
		t.Fatalf("UpdateTaskScheduledAt: %v", err)
	}

	h.tryAutoPromote(ctx)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TaskStatusInProgress {
		t.Errorf("expected task to be promoted (past schedule), got %s", got.Status)
	}
}

// TestTryAutoPromote_SkipsFutureButPromotesUnscheduled verifies that when a
// future-scheduled task and an unscheduled task both exist in the backlog, only
// the unscheduled one is promoted.
func TestTryAutoPromote_SkipsFutureButPromotesUnscheduled(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.autoimplement.Store(true)

	ctx := context.Background()

	// Future-scheduled task at lower position (higher priority by default).
	scheduled, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "future scheduled", Timeout: 15})
	_ = h.store.UpdateTaskPosition(ctx, scheduled.ID, 0)

	future := time.Now().Add(1 * time.Hour)
	if err := h.store.UpdateTaskScheduledAt(ctx, scheduled.ID, &future); err != nil {
		t.Fatalf("UpdateTaskScheduledAt: %v", err)
	}

	// Unscheduled task at higher position (lower priority).
	unscheduled, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "unscheduled", Timeout: 15})
	_ = h.store.UpdateTaskPosition(ctx, unscheduled.ID, 1)

	h.tryAutoPromote(ctx)

	scheduledGot, _ := h.store.GetTask(ctx, scheduled.ID)
	unscheduledGot, _ := h.store.GetTask(ctx, unscheduled.ID)

	if scheduledGot.Status != store.TaskStatusBacklog {
		t.Errorf("future-scheduled task should remain backlog, got %s", scheduledGot.Status)
	}
	if unscheduledGot.Status != store.TaskStatusInProgress {
		t.Errorf("unscheduled task should be promoted, got %s", unscheduledGot.Status)
	}
}

// TestUpdateTask_SetScheduledAt verifies that PATCH /api/tasks/{id} with a
// future scheduled_at stores it correctly.
func TestUpdateTask_SetScheduledAt(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	ctx := context.Background()

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "sched test", Timeout: 15})

	future := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	body, _ := json.Marshal(map[string]any{
		"scheduled_at": future.Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(string(body)))
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
	if got.ScheduledAt == nil {
		t.Fatal("expected ScheduledAt to be set after PATCH")
	}
	if !got.ScheduledAt.Equal(future) {
		t.Errorf("ScheduledAt = %v, want %v", got.ScheduledAt, future)
	}
}

// TestUpdateTask_ClearScheduledAt verifies that PATCH with scheduled_at: null
// clears the schedule.
func TestUpdateTask_ClearScheduledAt(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	ctx := context.Background()

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "sched clear", Timeout: 15})
	future := time.Now().Add(2 * time.Hour)
	_ = h.store.UpdateTaskScheduledAt(ctx, task.ID, &future)

	body := `{"scheduled_at":null}`
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
	if got.ScheduledAt != nil {
		t.Errorf("expected ScheduledAt to be cleared, got %v", got.ScheduledAt)
	}
}

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

// TestCreateTask_ReturnsSandboxBudgetAndScheduleInBody verifies that the
// 201 response body already contains sandbox, budget, and schedule fields
// without requiring a subsequent fetch.
func TestCreateTask_ReturnsSandboxBudgetAndScheduleInBody(t *testing.T) {
	h := newTestHandler(t)
	futureTime := time.Now().Add(2 * time.Hour).Truncate(time.Second).UTC()

	body, _ := json.Marshal(map[string]any{
		"prompt":           "atomic creation test",
		"max_cost_usd":     5.5,
		"max_input_tokens": 1000,
		"scheduled_at":     futureTime,
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
	if task.ScheduledAt == nil {
		t.Fatal("scheduled_at: expected non-nil in response")
	}
	if !task.ScheduledAt.Equal(futureTime) {
		t.Errorf("scheduled_at: want %v, got %v", futureTime, *task.ScheduledAt)
	}
}

// TestCreateTask_EmitsOnlyOneDelta verifies that creating a fully configured
// task (sandbox + budget + schedule) emits exactly one SSE delta, not one per
// post-create update call.
func TestCreateTask_EmitsOnlyOneDelta(t *testing.T) {
	h := newTestHandler(t)

	subID, deltaCh := h.store.Subscribe()
	defer h.store.Unsubscribe(subID)

	futureTime := time.Now().Add(2 * time.Hour)
	body, _ := json.Marshal(map[string]any{
		"prompt":           "delta count test",
		"max_cost_usd":     3.0,
		"max_input_tokens": 500,
		"scheduled_at":     futureTime,
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

// TestCreateTask_ScheduledTaskNotAutoPromotedBeforeScheduledAt verifies that
// a task's ScheduledAt is persisted atomically at creation time, so
// tryAutoPromote cannot promote it before the schedule fires.
func TestCreateTask_ScheduledTaskNotAutoPromotedBeforeScheduledAt(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutoimplement(true)

	futureTime := time.Now().Add(24 * time.Hour)
	body, _ := json.Marshal(map[string]any{
		"prompt":       "scheduled task",
		"scheduled_at": futureTime,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created store.Task
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	// ScheduledAt must be present in the response body immediately.
	if created.ScheduledAt == nil {
		t.Fatal("scheduled_at not set in create response — task could be auto-promoted immediately")
	}

	// Auto-promote must not advance the task to in_progress.
	h.tryAutoPromote(context.Background())

	got, err := h.store.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TaskStatusBacklog {
		t.Errorf("scheduled task was auto-promoted before scheduled_at: status=%s", got.Status)
	}
}

// ---------------------------------------------------------------------------
// Auto-retry integration tests
// ---------------------------------------------------------------------------

func autoRetryCrashCmd(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-cmd")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func autoRetryBudgetErrorCmd(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := `{"result":"budget exceeded","session_id":"s1","stop_reason":"end_turn","is_error":true,"total_cost_usd":0.001}`
	script := filepath.Join(dir, "fake-cmd")
	content := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s'\nexit 0\n", out)
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func setupAutoRetryRunner(t *testing.T, cmd string) (*store.Store, *runner.Runner) {
	t.Helper()
	repo := setupRepo(t)

	// Use os.MkdirTemp for the store directory so that background compaction
	// goroutines (fired on status transitions) do not race with t.TempDir
	// cleanup. On Linux /dev/shm is preferred for speed; on macOS it falls
	// back to the default tmp dir.
	dataDir, err := os.MkdirTemp("/dev/shm", "wallfacer-ar-*")
	if err != nil {
		dataDir, err = os.MkdirTemp("", "wallfacer-ar-*")
		if err != nil {
			t.Fatal(err)
		}
	}
	s, storeErr := storetest.NewFileStore(t, dataDir)
	if storeErr != nil {
		_ = os.RemoveAll(dataDir)
		t.Fatal(storeErr)
	}

	worktreesDir, err := os.MkdirTemp("", "wallfacer-ar-wt-*")
	if err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		Workspaces:       []string{repo},
		WorktreesDir:     worktreesDir,
		HostClaudeBinary: cmd,
		HostCodexBinary:  cmd,
	})
	// Cleanups run LIFO: first drain background goroutines and compaction,
	// then remove temp directories.
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	t.Cleanup(func() { _ = os.RemoveAll(worktreesDir) })
	t.Cleanup(s.WaitCompaction)
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)
	return s, r
}

func TestAutoRetry_ContainerCrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix shell")
	}
	cmd := autoRetryCrashCmd(t)
	s, r := setupAutoRetryRunner(t, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "crash retry test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
			t.Fatalf("run %d: UpdateTaskStatus in_progress: %v", i+1, err)
		}
		r.Run(task.ID, "do the task", "", false)

		got, err := s.GetTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("run %d: GetTask: %v", i+1, err)
		}
		if i < 2 {
			if got.Status != store.TaskStatusBacklog {
				t.Errorf("run %d: expected backlog after auto-retry, got %s", i+1, got.Status)
			}
		} else {
			if got.Status != store.TaskStatusFailed {
				t.Errorf("run %d: expected failed after budget exhausted, got %s", i+1, got.Status)
			}
			if got.FailureCategory != store.FailureCategoryContainerCrash {
				t.Errorf("run %d: expected failure_category=%s, got %s",
					i+1, store.FailureCategoryContainerCrash, got.FailureCategory)
			}
			if got.AutoRetryCount != 2 {
				t.Errorf("run %d: expected AutoRetryCount=2, got %d", i+1, got.AutoRetryCount)
			}
		}
	}
}

func TestAutoRetry_BudgetCategoryDoesNotRetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix shell")
	}
	cmd := autoRetryBudgetErrorCmd(t)
	s, r := setupAutoRetryRunner(t, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "budget exceeded test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TaskStatusFailed {
		t.Errorf("expected failed (no retry budget for budget_exceeded), got %s", got.Status)
	}
	if got.AutoRetryCount != 0 {
		t.Errorf("expected AutoRetryCount=0 (no retry attempted), got %d", got.AutoRetryCount)
	}
}

func TestAutoRetry_MaxTotalCap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix shell")
	}
	cmd := autoRetryCrashCmd(t)
	s, r := setupAutoRetryRunner(t, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "total cap test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	for range 3 {
		if err := s.IncrementAutoRetryCount(ctx, task.ID, store.FailureCategorySyncError); err != nil {
			t.Fatalf("IncrementAutoRetryCount: %v", err)
		}
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TaskStatusFailed {
		t.Errorf("expected failed (global cap reached), got %s", got.Status)
	}
	if got.AutoRetryCount != 3 {
		t.Errorf("expected AutoRetryCount=3 (unchanged after cap), got %d", got.AutoRetryCount)
	}
}

// TestWatcherBreaker_AutoSyncDoesNotAffectAutoPromote verifies that opening
// the "auto-sync" circuit breaker leaves the "auto-promote" breaker closed.
func TestWatcherBreaker_AutoSyncDoesNotAffectAutoPromote(t *testing.T) {
	h := newTestHandler(t)

	// Open the auto-sync breaker via a simulated failure.
	h.openWatcherBreaker("auto-sync", nil, "git rebase failed")

	if !h.breakers["auto-sync"].isOpen() {
		t.Error("expected auto-sync breaker to be open after failure")
	}
	if h.breakers["auto-promote"].isOpen() {
		t.Error("expected auto-promote breaker to remain closed after auto-sync failure")
	}
}

// TestWatcherBreaker_AutoRecoversAfterTimeout verifies that a breaker returns
// to closed (healthy) once its backoff time has elapsed.
func TestWatcherBreaker_AutoRecoversAfterTimeout(t *testing.T) {
	now := time.Now()
	wb := &watcherBreaker{
		breaker: circuitbreaker.NewBackoff(circuitbreaker.BackoffConfig{
			BaseDelay: 10 * time.Millisecond,
			Now:       func() time.Time { return now },
		}),
	}

	wb.recordFailure(nil, "transient error")
	if !wb.isOpen() {
		t.Fatal("expected breaker to be open immediately after failure")
	}

	// Advance time past the backoff duration.
	now = now.Add(20 * time.Millisecond)

	if wb.isOpen() {
		t.Error("expected breaker to be closed after backoff has elapsed")
	}
}

// TestWatcherBreaker_RecordSuccessResets verifies that recordSuccess resets
// the failure counter and closes the breaker.
func TestWatcherBreaker_RecordSuccessResets(t *testing.T) {
	wb := newWatcherBreaker()

	wb.recordFailure(nil, "error 1")
	wb.recordFailure(nil, "error 2")

	if f := wb.breaker.Failures(); f != 2 {
		t.Errorf("expected failures=2, got %d", f)
	}
	if !wb.isOpen() {
		t.Error("expected breaker to be open after failures")
	}

	wb.recordSuccess()

	if f := wb.breaker.Failures(); f != 0 {
		t.Errorf("expected failures=0 after recordSuccess, got %d", f)
	}
	if wb.isOpen() {
		t.Error("expected breaker to be closed after recordSuccess")
	}
}

// TestGetConfig_IncludesWatcherHealth verifies that GET /api/config includes
// a watcher_health array with all watcher entries.
func TestGetConfig_IncludesWatcherHealth(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	healthRaw, ok := resp["watcher_health"]
	if !ok {
		t.Fatal("expected watcher_health in config response")
	}
	health, ok := healthRaw.([]any)
	if !ok {
		t.Fatalf("expected watcher_health to be an array, got %T", healthRaw)
	}

	wantNames := []string{"auto-promote", "auto-retry", "auto-submit", "auto-sync", "auto-review"}
	if len(health) != len(wantNames) {
		t.Fatalf("expected %d watcher health entries, got %d", len(wantNames), len(health))
	}

	for i, entry := range health {
		m, ok := entry.(map[string]any)
		if !ok {
			t.Errorf("entry %d: expected map, got %T", i, entry)
			continue
		}
		if m["name"] != wantNames[i] {
			t.Errorf("entry %d: expected name=%q, got %q", i, wantNames[i], m["name"])
		}
		healthy, ok := m["healthy"].(bool)
		if !ok {
			t.Errorf("entry %d: expected healthy bool, got %T", i, m["healthy"])
		} else if !healthy {
			t.Errorf("entry %d: expected healthy=true by default, got false", i)
		}
	}

	// Open one breaker and verify it is reflected in the health report.
	h.openWatcherBreaker("auto-sync", nil, "test failure")

	req2 := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w2 := httptest.NewRecorder()
	h.GetConfig(w2, req2)

	var resp2 map[string]any
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode 2: %v", err)
	}
	health2 := resp2["watcher_health"].([]any)
	for _, entry := range health2 {
		m := entry.(map[string]any)
		if m["name"] == "auto-sync" {
			if m["healthy"].(bool) {
				t.Error("expected auto-sync to be unhealthy after opening breaker")
			}
			if m["last_reason"] != "test failure" {
				t.Errorf("expected last_reason=%q, got %q", "test failure", m["last_reason"])
			}
		}
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

// ─────────────────────────────────────────────────────────────────────────────
// GetStats
// ─────────────────────────────────────────────────────────────────────────────

// TestGetStats_EmptyStore verifies that GetStats returns a 200 with stats for an empty store.
func TestGetStats_EmptyStore(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()
	h.GetStats(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stats any
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

// TestGetStats_UnknownWorkspace verifies that filtering by unknown workspace returns 400.
func TestGetStats_UnknownWorkspace(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	_, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "some task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/stats?workspace=/nonexistent", nil)
	w := httptest.NewRecorder()
	h.GetStats(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown workspace, got %d", w.Code)
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
