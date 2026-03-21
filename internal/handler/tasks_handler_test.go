package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// TestListSummaries_Empty verifies that ListSummaries returns an empty JSON
// array when no tasks have summary.json files.
func TestListSummaries_Empty(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/summaries", nil)
	w := httptest.NewRecorder()
	h.ListSummaries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var summaries []store.TaskSummary
	if err := json.NewDecoder(w.Body).Decode(&summaries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("expected empty slice, got %d summaries", len(summaries))
	}
}

// TestListSummaries_WithSavedSummary verifies that ListSummaries returns a
// summary that was written to disk via Store.SaveSummary.
func TestListSummaries_WithSavedSummary(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "summary task", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	summary := store.TaskSummary{
		TaskID:      task.ID,
		Title:       "summary task",
		Status:      store.TaskStatusDone,
		CompletedAt: time.Now(),
		CreatedAt:   time.Now(),
	}
	if err := h.store.SaveSummary(task.ID, summary); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/summaries", nil)
	w := httptest.NewRecorder()
	h.ListSummaries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var summaries []store.TaskSummary
	if err := json.NewDecoder(w.Body).Decode(&summaries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].TaskID != task.ID {
		t.Errorf("summary TaskID mismatch: got %s, want %s", summaries[0].TaskID, task.ID)
	}
}

// TestListDeletedTasks_Empty verifies that ListDeletedTasks returns an empty
// JSON array when no tasks have been soft-deleted.
func TestListDeletedTasks_Empty(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/deleted", nil)
	w := httptest.NewRecorder()
	h.ListDeletedTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var tasks []store.Task
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected empty slice, got %d tasks", len(tasks))
	}
}

// TestListDeletedTasks_AfterSoftDelete verifies that a soft-deleted task appears
// in the ListDeletedTasks response.
func TestListDeletedTasks_AfterSoftDelete(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "to be deleted", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.DeleteTask(ctx, task.ID, "test reason"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/deleted", nil)
	w := httptest.NewRecorder()
	h.ListDeletedTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var tasks []store.Task
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 deleted task, got %d", len(tasks))
	}
	if tasks[0].ID != task.ID {
		t.Errorf("deleted task ID mismatch: got %s, want %s", tasks[0].ID, task.ID)
	}
}

// TestRestoreTask_NotFound verifies that RestoreTask returns 500 when the task
// ID does not correspond to a deleted task.
func TestRestoreTask_NotFound(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/restore", nil)
	w := httptest.NewRecorder()
	h.RestoreTask(w, req, uuid.New())
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for unknown task ID, got %d", w.Code)
	}
}

// TestRestoreTask_Success verifies that RestoreTask removes the tombstone and
// makes the task reappear in the active task list.
func TestRestoreTask_Success(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "restore me", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.DeleteTask(ctx, task.ID, ""); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	// Confirm it appears as deleted.
	deleted, err := h.store.ListDeletedTasks(ctx)
	if err != nil || len(deleted) != 1 {
		t.Fatalf("expected 1 deleted task before restore, got %d (err=%v)", len(deleted), err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/restore", nil)
	w := httptest.NewRecorder()
	h.RestoreTask(w, req, task.ID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Task should now be active again.
	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	found := false
	for _, tsk := range tasks {
		if tsk.ID == task.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("restored task did not reappear in active task list")
	}

	// And it should no longer appear in deleted list.
	deleted, err = h.store.ListDeletedTasks(ctx)
	if err != nil {
		t.Fatalf("ListDeletedTasks after restore: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deleted tasks after restore, got %d", len(deleted))
	}
}

// TestFilterByFailureCategory_MatchesCorrectly verifies that
// filterByFailureCategory returns only tasks whose FailureCategory matches the
// requested category.
func TestFilterByFailureCategory_MatchesCorrectly(t *testing.T) {
	tasks := []store.Task{
		{ID: uuid.New(), FailureCategory: store.FailureCategoryTimeout},
		{ID: uuid.New(), FailureCategory: store.FailureCategoryBudget},
		{ID: uuid.New(), FailureCategory: store.FailureCategoryTimeout},
		{ID: uuid.New(), FailureCategory: store.FailureCategoryUnknown},
	}

	got := filterByFailureCategory(tasks, store.FailureCategoryTimeout)
	if len(got) != 2 {
		t.Fatalf("expected 2 timeout tasks, got %d", len(got))
	}
	for _, tsk := range got {
		if tsk.FailureCategory != store.FailureCategoryTimeout {
			t.Errorf("unexpected FailureCategory in result: %s", tsk.FailureCategory)
		}
	}
}

// TestFilterByFailureCategory_EmptyInput verifies that a nil input returns a
// non-nil empty slice.
func TestFilterByFailureCategory_EmptyInput(t *testing.T) {
	got := filterByFailureCategory(nil, store.FailureCategoryTimeout)
	if got == nil {
		t.Error("expected non-nil slice from nil input")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 results for nil input, got %d", len(got))
	}
}

// TestFilterByFailureCategory_NoMatch verifies that no tasks are returned when
// none match the requested category.
func TestFilterByFailureCategory_NoMatch(t *testing.T) {
	tasks := []store.Task{
		{ID: uuid.New(), FailureCategory: store.FailureCategoryBudget},
		{ID: uuid.New(), FailureCategory: store.FailureCategoryWorktree},
	}
	got := filterByFailureCategory(tasks, store.FailureCategoryTimeout)
	if len(got) != 0 {
		t.Errorf("expected 0 results, got %d", len(got))
	}
}

// TestFilterByFailureCategory_ViaListTasksHandler verifies that the
// failure_category query parameter on ListTasks correctly filters tasks through
// the full handler path, covering the filterByFailureCategory call site.
func TestFilterByFailureCategory_ViaListTasksHandler(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task1, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "timeout task", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	task2, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "budget task", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.SetTaskFailureCategory(ctx, task1.ID, store.FailureCategoryTimeout); err != nil {
		t.Fatalf("SetTaskFailureCategory timeout: %v", err)
	}
	if err := h.store.SetTaskFailureCategory(ctx, task2.ID, store.FailureCategoryBudget); err != nil {
		t.Fatalf("SetTaskFailureCategory budget: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks?failure_category=timeout", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var tasks []store.Task
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 timeout task, got %d", len(tasks))
	}
	if tasks[0].ID != task1.ID {
		t.Errorf("unexpected task in filtered result: %s", tasks[0].ID)
	}
}

// --- GetContainers ---

// TestGetContainers_RuntimeError verifies that GetContainers returns 500 when the
// container runtime is not available (no command configured in the runner).
func TestGetContainers_RuntimeError(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	w := httptest.NewRecorder()
	h.GetContainers(w, req)
	// With no container runtime configured, ListContainers fails → 500.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when runtime fails, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetContainers_EmptyWithMock verifies that GetContainers returns 200 with an
// empty JSON array when the runner reports no containers.
func TestGetContainers_EmptyWithMock(t *testing.T) {
	m := &runner.MockRunner{}
	h, _ := newTestHandlerWithMockRunner(t, m)
	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	w := httptest.NewRecorder()
	h.GetContainers(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- ListTasks pagination tests ---

// TestListTasks_ArchivedPageSizeWithoutFlag returns 400 when archived_page_size
// is given without include_archived=true.
func TestListTasks_ArchivedPageSizeWithoutFlag(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?archived_page_size=10", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListTasks_ArchivedPageSizeInvalid returns 400 for non-numeric page size.
func TestListTasks_ArchivedPageSizeInvalid(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?include_archived=true&archived_page_size=abc", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListTasks_ArchivedBeforeInvalid returns 400 for an invalid archived_before UUID.
func TestListTasks_ArchivedBeforeInvalid(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?include_archived=true&archived_page_size=10&archived_before=not-a-uuid", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListTasks_ArchivedAfterInvalid returns 400 for an invalid archived_after UUID.
func TestListTasks_ArchivedAfterInvalid(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?include_archived=true&archived_page_size=10&archived_after=not-a-uuid", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListTasks_ArchivedPaginationSuccess returns 200 with a paged response
// including a tasks field when include_archived=true and archived_page_size is set.
func TestListTasks_ArchivedPaginationSuccess(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	// Create a task, force it to done, then archive it.
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "archived task", Timeout: 30})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := h.store.SetTaskArchived(ctx, task.ID, true); err != nil {
		t.Fatalf("SetTaskArchived: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks?include_archived=true&archived_page_size=10", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["tasks"]; !ok {
		t.Error("expected 'tasks' field in paginated response")
	}
	if _, ok := resp["total_archived"]; !ok {
		t.Error("expected 'total_archived' field in paginated response")
	}
}

// TestListTasks_ArchivedPaginationBothCursorsError verifies that providing both
// archived_before and archived_after in the same request returns an error.
func TestListTasks_ArchivedPaginationBothCursorsError(t *testing.T) {
	h := newTestHandler(t)
	id1 := uuid.New().String()
	id2 := uuid.New().String()
	url := "/api/tasks?include_archived=true&archived_page_size=10&archived_before=" + id1 + "&archived_after=" + id2
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)
	// The store rejects mutually exclusive cursors.
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for mutually exclusive cursors, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListTasks_FailureCategoryInvalid returns 400 for an unknown failure_category value.
func TestListTasks_FailureCategoryInvalid(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?failure_category=unknown-category", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid failure_category, got %d: %s", w.Code, w.Body.String())
	}
}
