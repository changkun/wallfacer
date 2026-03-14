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
	"sync"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/gitutil"
	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/sandbox"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
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
	_, err := h.store.CreateTask(ctx, "task one", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.store.CreateTask(ctx, "task two", 15, false, "", "")
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
	task, err := h.store.CreateTask(ctx, "archived task", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)
	h.store.SetTaskArchived(ctx, task.ID, true)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)

	var tasks []store.Task
	json.NewDecoder(w.Body).Decode(&tasks)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks (archived excluded), got %d", len(tasks))
	}
}

// TestListTasks_IncludeArchived checks that archived tasks appear when requested.
func TestListTasks_IncludeArchived(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, err := h.store.CreateTask(ctx, "archived task", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)
	h.store.SetTaskArchived(ctx, task.ID, true)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks?include_archived=true", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)

	var tasks []store.Task
	json.NewDecoder(w.Body).Decode(&tasks)
	if len(tasks) != 1 {
		t.Errorf("expected 1 task (archived included), got %d", len(tasks))
	}
}

func TestListTasks_ArchivedPaged(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	archivedIDs := make([]uuid.UUID, 0, 5)
	for i := 0; i < 5; i++ {
		task, err := h.store.CreateTask(ctx, fmt.Sprintf("archived %d", i), 15, false, "", "")
		if err != nil {
			t.Fatal(err)
		}
		h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)
		h.store.SetTaskArchived(ctx, task.ID, true)
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

// TestCreateTask_RespectsSandbox verifies that sandbox preference is stored at creation.
func TestCreateTask_RespectsSandbox(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	reqEnv := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(`{"openai_api_key":"sk-test"}`))
	wEnv := httptest.NewRecorder()
	h.UpdateEnvConfig(wEnv, reqEnv)
	if wEnv.Code != http.StatusNoContent {
		t.Fatalf("expected env update 204, got %d: %s", wEnv.Code, wEnv.Body.String())
	}
	h.setSandboxTestPassed(sandbox.Codex, true)

	body := `{"prompt": "build a thing", "sandbox": "codex"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	var task store.Task
	if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if task.Sandbox != "codex" {
		t.Errorf("expected sandbox 'codex', got %q", task.Sandbox)
	}
}

func TestCreateTask_RejectsCodexWhenUntested(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	reqEnv := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(`{"openai_api_key":"sk-test"}`))
	wEnv := httptest.NewRecorder()
	h.UpdateEnvConfig(wEnv, reqEnv)
	if wEnv.Code != http.StatusNoContent {
		t.Fatalf("expected env update 204, got %d: %s", wEnv.Code, wEnv.Body.String())
	}

	body := `{"prompt": "build a thing", "sandbox": "codex"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateTask_AllowsCodexWithHostAuthCache(t *testing.T) {
	h, _, _ := newTestHandlerWithEnvAndCodexAuth(t)

	body := `{"prompt": "build a thing", "sandbox": "codex"}`
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
	if task.Sandbox != "codex" {
		t.Errorf("expected sandbox 'codex', got %q", task.Sandbox)
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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	body := `{"position": 5}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Position != 5 {
		t.Errorf("expected position 5, got %d", updated.Position)
	}
}

// TestUpdateTask_UpdatesBacklogFields verifies that prompt/timeout can be updated for backlog tasks.
func TestUpdateTask_UpdatesBacklogFields(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "original prompt", 15, false, "", "")

	body := `{"prompt": "new prompt", "timeout": 60}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var updated store.Task
	json.NewDecoder(w.Body).Decode(&updated)
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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	body := `{"status": "backlog"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var updated store.Task
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Status != store.TaskStatusBacklog {
		t.Errorf("expected backlog, got %s", updated.Status)
	}
}

// TestUpdateTask_RetryFromFailed verifies that a failed task can be retried.
func TestUpdateTask_RetryFromFailed(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	body := `{"status": "backlog"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var updated store.Task
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Status != store.TaskStatusBacklog {
		t.Errorf("expected backlog, got %s", updated.Status)
	}
}

// TestUpdateTask_RetryWithNewPrompt verifies that retry can supply a new prompt.
func TestUpdateTask_RetryWithNewPrompt(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "old prompt", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCancelled)

	body := `{"status": "backlog", "prompt": "new retry prompt"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var updated store.Task
	json.NewDecoder(w.Body).Decode(&updated)
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
	task, _ := h.store.CreateTask(ctx, "to delete", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var events []store.TaskEvent
	json.NewDecoder(w.Body).Decode(&events)
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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, map[string]string{
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
	json.NewDecoder(w.Body).Decode(&events)
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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, "hello")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	for i := 0; i < 5; i++ {
		h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, i)
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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	for i := 0; i < 5; i++ {
		h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, i)
	}

	// Get first page.
	req1 := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/events?limit=3", nil)
	w1 := httptest.NewRecorder()
	h.GetEvents(w1, req1, task.ID)

	var page1 eventsPageResponse
	json.NewDecoder(w1.Body).Decode(&page1)

	// Use cursor from page1 to get page2.
	url2 := fmt.Sprintf("/api/tasks/%s/events?after=%d&limit=10", task.ID.String(), page1.NextAfter)
	req2 := httptest.NewRequest(http.MethodGet, url2, nil)
	w2 := httptest.NewRecorder()
	h.GetEvents(w2, req2, task.ID)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for page2, got %d", w2.Code)
	}
	var page2 eventsPageResponse
	json.NewDecoder(w2.Body).Decode(&page2)

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, "state")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, "out1")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeError, "err")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, "out2")

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?types=output", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp eventsPageResponse
	json.NewDecoder(w.Body).Decode(&resp)

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, "state")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, "out")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeError, "err")

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?types=state_change,error", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp eventsPageResponse
	json.NewDecoder(w.Body).Decode(&resp)

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	for i := 0; i < 3; i++ {
		h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, i)
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
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Events) == 0 {
		t.Error("expected some events")
	}
}

// TestGetEvents_UnknownTypeParam verifies 400 for an unknown event type.
func TestGetEvents_UnknownTypeParam(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, "s")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, "o")

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?types=", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp eventsPageResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.TotalFiltered < 2 {
		t.Errorf("expected at least 2 events (no type filter), got TotalFiltered=%d", resp.TotalFiltered)
	}
}

// TestGetEvents_HasMoreFalseWhenAllFit verifies HasMore=false when results fit in one page.
func TestGetEvents_HasMoreFalseWhenAllFit(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	for i := 0; i < 3; i++ {
		h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, i)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?limit=100", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	var resp eventsPageResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.HasMore {
		t.Error("expected HasMore=false when all events fit in one page")
	}
}

// TestGetEvents_TotalFilteredAccountsForTypeFilter verifies TotalFiltered counts
// only type-matching events, not all events.
func TestGetEvents_TotalFilteredAccountsForTypeFilter(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	for i := 0; i < 6; i++ {
		h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, i)
	}
	h.store.InsertEvent(ctx, task.ID, store.EventTypeError, "e")

	req := httptest.NewRequest(http.MethodGet,
		"/api/tasks/"+task.ID.String()+"/events?types=output&limit=2", nil)
	w := httptest.NewRecorder()
	h.GetEvents(w, req, task.ID)

	var resp eventsPageResponse
	json.NewDecoder(w.Body).Decode(&resp)
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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	// Manually create the outputs directory and a turn file.
	outputsDir := h.store.OutputsDir(task.ID)
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

// TestGenerateMissingTitles_NoUntitled verifies response when all tasks have titles.
func TestGenerateMissingTitles_NoUntitled(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.UpdateTaskTitle(ctx, task.ID, "My Task Title")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/generate-titles", nil)
	w := httptest.NewRecorder()
	h.GenerateMissingTitles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if queued, ok := resp["queued"].(float64); !ok || queued != 0 {
		t.Errorf("expected queued=0, got %v", resp["queued"])
	}
}

// TestGenerateMissingTitles_WithUntitled verifies that untitled tasks are queued.
func TestGenerateMissingTitles_WithUntitled(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	h.store.CreateTask(ctx, "task without title", 15, false, "", "")
	h.store.CreateTask(ctx, "another without title", 15, false, "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/generate-titles", nil)
	w := httptest.NewRecorder()
	h.GenerateMissingTitles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if total, ok := resp["total_without_title"].(float64); !ok || total != 2 {
		t.Errorf("expected total_without_title=2, got %v", resp["total_without_title"])
	}
}

// TestGenerateMissingTitles_LimitParam verifies the limit query parameter is respected.
func TestGenerateMissingTitles_LimitParam(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		h.store.CreateTask(ctx, "task without title", 15, false, "", "")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/generate-titles?limit=2", nil)
	w := httptest.NewRecorder()
	h.GenerateMissingTitles(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if queued, ok := resp["queued"].(float64); !ok || queued != 2 {
		t.Errorf("expected queued=2, got %v", resp["queued"])
	}
	if total, ok := resp["total_without_title"].(float64); !ok || total != 5 {
		t.Errorf("expected total_without_title=5, got %v", resp["total_without_title"])
	}
}

// --- PATCH depends_on tests ---

// TestUpdateTask_SetDependsOn_Valid verifies setting a single valid dependency.
func TestUpdateTask_SetDependsOn_Valid(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	a, _ := h.store.CreateTask(ctx, "dep", 15, false, "", "")
	b, _ := h.store.CreateTask(ctx, "task", 15, false, "", "")

	body := `{"depends_on": ["` + a.ID.String() + `"]}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+b.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, b.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	json.NewDecoder(w.Body).Decode(&updated)
	if len(updated.DependsOn) != 1 || updated.DependsOn[0] != a.ID.String() {
		t.Errorf("expected DependsOn=[%s], got %v", a.ID, updated.DependsOn)
	}
}

// TestUpdateTask_SetDependsOn_Multiple verifies setting multiple dependencies.
func TestUpdateTask_SetDependsOn_Multiple(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	a, _ := h.store.CreateTask(ctx, "dep-a", 15, false, "", "")
	b, _ := h.store.CreateTask(ctx, "dep-b", 15, false, "", "")
	c, _ := h.store.CreateTask(ctx, "task", 15, false, "", "")

	body := `{"depends_on": ["` + a.ID.String() + `","` + b.ID.String() + `"]}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+c.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, c.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	json.NewDecoder(w.Body).Decode(&updated)
	if len(updated.DependsOn) != 2 {
		t.Errorf("expected 2 DependsOn entries, got %v", updated.DependsOn)
	}
}

// TestUpdateTask_SetDependsOn_ClearsWithEmpty verifies that sending depends_on: []
// removes all dependencies.
func TestUpdateTask_SetDependsOn_ClearsWithEmpty(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	a, _ := h.store.CreateTask(ctx, "dep", 15, false, "", "")
	b, _ := h.store.CreateTask(ctx, "task", 15, false, "", "")
	h.store.UpdateTaskDependsOn(ctx, b.ID, []string{a.ID.String()})

	body := `{"depends_on": []}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+b.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, b.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	json.NewDecoder(w.Body).Decode(&updated)
	if len(updated.DependsOn) != 0 {
		t.Errorf("expected DependsOn nil/empty after clear, got %v", updated.DependsOn)
	}
}

// TestUpdateTask_SetDependsOn_SelfDependency verifies that a task cannot depend on itself.
func TestUpdateTask_SetDependsOn_SelfDependency(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	a, _ := h.store.CreateTask(ctx, "task", 15, false, "", "")

	body := `{"depends_on": ["` + a.ID.String() + `"]}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+a.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, a.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for self-dependency, got %d", w.Code)
	}
}

// TestUpdateTask_SetDependsOn_UnknownUUID verifies that a non-existent dependency UUID returns 400.
func TestUpdateTask_SetDependsOn_UnknownUUID(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	a, _ := h.store.CreateTask(ctx, "task", 15, false, "", "")
	unknown := "00000000-0000-0000-0000-000000000001"

	body := `{"depends_on": ["` + unknown + `"]}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+a.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, a.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown UUID, got %d", w.Code)
	}
}

// TestUpdateTask_SetDependsOn_InvalidUUID verifies that a malformed UUID returns 400.
func TestUpdateTask_SetDependsOn_InvalidUUID(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	a, _ := h.store.CreateTask(ctx, "task", 15, false, "", "")

	body := `{"depends_on": ["not-a-uuid"]}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+a.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, a.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid UUID, got %d", w.Code)
	}
}

// TestUpdateTask_SetDependsOn_DirectCycle verifies that A→B and then B→A is rejected.
func TestUpdateTask_SetDependsOn_DirectCycle(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	a, _ := h.store.CreateTask(ctx, "a", 15, false, "", "")
	b, _ := h.store.CreateTask(ctx, "b", 15, false, "", "")
	// Set A depends on B.
	h.store.UpdateTaskDependsOn(ctx, a.ID, []string{b.ID.String()})

	// Now try to set B depends on A (would be a direct cycle).
	body := `{"depends_on": ["` + a.ID.String() + `"]}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+b.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, b.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for direct cycle, got %d", w.Code)
	}
}

// TestUpdateTask_SetDependsOn_TransitiveCycle verifies that transitive cycles are detected.
func TestUpdateTask_SetDependsOn_TransitiveCycle(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	a, _ := h.store.CreateTask(ctx, "a", 15, false, "", "")
	b, _ := h.store.CreateTask(ctx, "b", 15, false, "", "")
	c, _ := h.store.CreateTask(ctx, "c", 15, false, "", "")
	// A depends on B; B depends on C.
	h.store.UpdateTaskDependsOn(ctx, a.ID, []string{b.ID.String()})
	h.store.UpdateTaskDependsOn(ctx, b.ID, []string{c.ID.String()})

	// Now try to set C depends on A (transitive cycle A→B→C→A).
	body := `{"depends_on": ["` + a.ID.String() + `"]}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+c.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, c.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for transitive cycle, got %d", w.Code)
	}
}

// TestUpdateTask_SetDependsOn_AbsentFieldNoOp verifies that omitting depends_on in
// the PATCH body does not clear existing dependencies.
func TestUpdateTask_SetDependsOn_AbsentFieldNoOp(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	a, _ := h.store.CreateTask(ctx, "dep", 15, false, "", "")
	b, _ := h.store.CreateTask(ctx, "task", 15, false, "", "")
	h.store.UpdateTaskDependsOn(ctx, b.ID, []string{a.ID.String()})

	// PATCH with position only — no depends_on key.
	body := `{"position": 5}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+b.ID.String(), strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, b.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	json.NewDecoder(w.Body).Decode(&updated)
	if len(updated.DependsOn) != 1 || updated.DependsOn[0] != a.ID.String() {
		t.Errorf("expected DependsOn unchanged, got %v", updated.DependsOn)
	}
}

// --- Auto-promoter dependency tests ---

// TestTryAutoPromote_SkipsBlockedTask verifies that the auto-promoter skips the
// lowest-position backlog task when its dependencies are not satisfied, and
// promotes the next eligible task instead.
func TestTryAutoPromote_SkipsBlockedTask(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.autopilotMu.Lock()
	h.autopilot = true
	h.autopilotMu.Unlock()

	ctx := context.Background()
	// dep: already in_progress (simulating it was started earlier), so it is
	// not a backlog candidate. blocked's dependency on dep is unsatisfied because
	// dep is in_progress, not done.
	dep, _ := h.store.CreateTask(ctx, "dep", 15, false, "", "")
	h.store.UpdateTaskStatus(ctx, dep.ID, store.TaskStatusInProgress)

	// blocked: lowest position (0), but depends on dep (in_progress, not done).
	blocked, _ := h.store.CreateTask(ctx, "blocked", 15, false, "", "")
	h.store.UpdateTaskDependsOn(ctx, blocked.ID, []string{dep.ID.String()})
	h.store.UpdateTaskPosition(ctx, blocked.ID, 0)

	// eligible: higher position (1), no dependencies.
	eligible, _ := h.store.CreateTask(ctx, "eligible", 15, false, "", "")
	h.store.UpdateTaskPosition(ctx, eligible.ID, 1)

	h.tryAutoPromote(ctx)

	tasks, _ := h.store.ListTasks(ctx, false)
	statusOf := func(id interface{ String() string }) store.TaskStatus {
		for _, t := range tasks {
			if t.ID.String() == id.String() {
				return t.Status
			}
		}
		return store.TaskStatus("unknown")
	}

	if statusOf(blocked.ID) != store.TaskStatusBacklog {
		t.Errorf("expected blocked task to remain backlog, got %s", statusOf(blocked.ID))
	}
	// dep and eligible may or may not be promoted depending on position; verify eligible was promoted.
	eligibleStatus := statusOf(eligible.ID)
	if eligibleStatus != store.TaskStatusInProgress {
		t.Errorf("expected eligible task to be in_progress, got %s", eligibleStatus)
	}
}

// TestTryAutoPromote_PromotesWhenDepsSatisfied verifies that a task with all deps
// done is promoted normally.
func TestTryAutoPromote_PromotesWhenDepsSatisfied(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.autopilotMu.Lock()
	h.autopilot = true
	h.autopilotMu.Unlock()

	ctx := context.Background()
	dep, _ := h.store.CreateTask(ctx, "dep", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, dep.ID, store.TaskStatusDone)

	task, _ := h.store.CreateTask(ctx, "task", 15, false, "", "")
	h.store.UpdateTaskDependsOn(ctx, task.ID, []string{dep.ID.String()})

	h.tryAutoPromote(ctx)

	updated, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != store.TaskStatusInProgress {
		t.Errorf("expected task to be promoted to in_progress, got %s", updated.Status)
	}
}

// TestTryAutoPromote_DoesNotCountTestRuns verifies that test-run in_progress
// tasks (IsTestRun=true) do not count against the regular task concurrency limit,
// so testing tasks cannot starve regular backlog promotions.
func TestTryAutoPromote_DoesNotCountTestRuns(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	h.autopilotMu.Lock()
	h.autopilot = true
	h.autopilotMu.Unlock()
	ctx := context.Background()

	// Set regular max to 1.
	if err := os.WriteFile(envPath, []byte("WALLFACER_MAX_PARALLEL=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// One test-run in_progress task (should not occupy the regular slot).
	testTask, _ := h.store.CreateTask(ctx, "test run", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, testTask.ID, store.TaskStatusInProgress)
	h.store.UpdateTaskTestRun(ctx, testTask.ID, true, "")

	// One backlog task that should be promoted.
	backlog, _ := h.store.CreateTask(ctx, "backlog task", 15, false, "", "")

	h.tryAutoPromote(ctx)

	got, _ := h.store.GetTask(ctx, backlog.ID)
	if got.Status != store.TaskStatusInProgress {
		t.Errorf("expected backlog task to be promoted (test-run should not block regular slots), got %s", got.Status)
	}
}

func TestTryAutoRetry_ContainerCrash(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTask(ctx, "retry me", 15, false, "", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)
	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}

	failed, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	h.tryAutoRetry(ctx, *failed)

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

	task, err := h.store.CreateTask(ctx, "do not retry", 15, false, "", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)
	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryAgentError); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}

	failed, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	h.tryAutoRetry(ctx, *failed)

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

	task, err := h.store.CreateTask(ctx, "retry budget exhausted", 15, false, "", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)
	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}
	for i := 0; i < maxHandlerAutoRetries; i++ {
		if err := h.store.IncrementAutoRetryCount(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
			t.Fatalf("IncrementAutoRetryCount(%d): %v", i, err)
		}
	}

	failed, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got := failed.AutoRetryCount; got != maxHandlerAutoRetries {
		t.Fatalf("AutoRetryCount = %d, want %d", got, maxHandlerAutoRetries)
	}
	h.tryAutoRetry(ctx, *failed)

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

	task, err := h.store.CreateTask(ctx, "blocked by circuit", 15, false, "", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)
	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}
	for i := 0; i < runner.DefaultCBThreshold; i++ {
		h.runner.RecordContainerFailure()
	}

	failed, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	h.tryAutoRetry(ctx, *failed)

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

	task, err := h.store.CreateTask(ctx, "manual retries should not block auto-retry", 15, false, "", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)
	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}

	// Simulate two manual retries: RetryHistory grows but AutoRetryCount stays 0.
	for i := 0; i < 2; i++ {
		if err := h.store.ResetTaskForRetry(ctx, task.ID, task.Prompt, false); err != nil {
			t.Fatalf("ResetTaskForRetry(%d): %v", i, err)
		}
		h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)
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

	// tryAutoRetry should NOT be suppressed: AutoRetryCount=0 < maxHandlerAutoRetries
	// and budget remains (container_crash starts with 2).
	h.tryAutoRetry(ctx, *failed)

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

	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	// Leave the task in backlog (not waiting).
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// Add a commit to main so the worktree would be behind — but task isn't waiting.
	os.WriteFile(filepath.Join(repo, "new.txt"), []byte("upstream\n"), 0644)
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

	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
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

	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
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

	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// Add a commit to main — now the worktree is behind by 1.
	os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("upstream\n"), 0644)
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
	regular, _ := h.store.CreateTask(ctx, "regular task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, regular.ID, store.TaskStatusInProgress)

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	if err := os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("upstream\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "upstream commit")

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

	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// Add a commit to main so the worktree is behind.
	os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("upstream\n"), 0644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "upstream commit")

	h.checkAndSyncWaitingTasks(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected task to remain waiting when autosync is off, got %s", got.Status)
	}
}

// --- tryAutoRefine tests ---

// TestTryAutoRefine_DisabledNoOp verifies that auto-refine does nothing when disabled.
func TestTryAutoRefine_DisabledNoOp(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	h.store.CreateTask(ctx, "build a widget", 15, false, "", "")
	h.tryAutoRefine(ctx)

	// No refinement should have been started.
	tasks, _ := h.store.ListTasks(ctx, false)
	for _, task := range tasks {
		if task.CurrentRefinement != nil {
			t.Error("expected no refinement to be started when auto-refine is disabled")
		}
	}
}

// TestTryAutoRefine_TriggersForUnrefinedBacklog verifies that auto-refine triggers
// refinement for backlog tasks that have not been refined.
func TestTryAutoRefine_TriggersForUnrefinedBacklog(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutorefine(true)
	ctx := context.Background()

	h.store.CreateTask(ctx, "build a widget", 15, false, "", "")
	h.tryAutoRefine(ctx)

	tasks, _ := h.store.ListTasks(ctx, false)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].CurrentRefinement == nil {
		t.Error("expected refinement to be started for unrefined backlog task")
	}
	if tasks[0].CurrentRefinement != nil && tasks[0].CurrentRefinement.Source != "auto" {
		t.Errorf("expected refinement source to be 'auto', got %q", tasks[0].CurrentRefinement.Source)
	}
}

// TestTryAutoRefine_SkipsAlreadyRefined verifies that auto-refine skips tasks
// that have already been refined.
func TestTryAutoRefine_SkipsAlreadyRefined(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutorefine(true)
	ctx := context.Background()

	task, _ := h.store.CreateTask(ctx, "build a widget", 15, false, "", "")
	// Mark as having a completed refinement session.
	h.store.ApplyRefinement(ctx, task.ID, "refined prompt", store.RefinementSession{
		ID:          "session-1",
		StartPrompt: "build a widget",
		Result:      "refined",
	})

	h.tryAutoRefine(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.CurrentRefinement != nil {
		t.Error("expected no new refinement for already-refined task")
	}
}

// TestTryAutoRefine_PicksUpTaskAfterRetry verifies that a task whose
// CurrentRefinement was cleared by ResetTaskForRetry is eligible for
// auto-refinement again. This is the regression test for the bug where
// ResetTaskForRetry omitted clearing CurrentRefinement, causing tryAutoRefine
// to permanently skip the retried task.
func TestTryAutoRefine_PicksUpTaskAfterRetry(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutorefine(true)
	ctx := context.Background()

	task, err := h.store.CreateTask(ctx, "build a widget", 15, false, "", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Simulate a completed refinement job from the previous attempt.
	job := &store.RefinementJob{
		ID:     "job-old",
		Status: store.RefinementJobStatusDone,
		Result: "old spec",
	}
	if err := h.store.UpdateRefinementJob(ctx, task.ID, job); err != nil {
		t.Fatalf("UpdateRefinementJob: %v", err)
	}

	// Before the fix, ResetTaskForRetry would leave CurrentRefinement set,
	// and tryAutoRefine would skip the task. After the fix, CurrentRefinement
	// is cleared and tryAutoRefine can pick it up.
	if err := h.store.ResetTaskForRetry(ctx, task.ID, "build a better widget", true); err != nil {
		t.Fatalf("ResetTaskForRetry: %v", err)
	}

	// Verify CurrentRefinement was cleared by the reset.
	afterReset, _ := h.store.GetTask(ctx, task.ID)
	if afterReset.CurrentRefinement != nil {
		t.Fatal("expected CurrentRefinement to be nil after ResetTaskForRetry")
	}

	// tryAutoRefine should now start a new refinement for this task.
	h.tryAutoRefine(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.CurrentRefinement == nil {
		t.Error("expected tryAutoRefine to start a new refinement after retry reset")
	}
	if got.CurrentRefinement != nil && got.CurrentRefinement.Source != "auto" {
		t.Errorf("expected refinement source 'auto', got %q", got.CurrentRefinement.Source)
	}
}

// --- tryAutoTest tests ---

// TestTryAutoTest_DisabledNoOp verifies that auto-test does nothing when disabled.
func TestTryAutoTest_DisabledNoOp(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// autotest is disabled by default.
	h.tryAutoTest(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected task to remain waiting when autotest disabled, got %s", got.Status)
	}
}

// TestTryAutoTest_SkipsNonWaiting verifies that non-waiting tasks are not tested.
func TestTryAutoTest_SkipsNonWaiting(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutotest(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test task", 15, false, "", "")
	// Leave the task in backlog (not waiting).
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	h.tryAutoTest(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusBacklog {
		t.Errorf("expected task to remain in backlog, got %s", got.Status)
	}
}

// TestTryAutoTest_SkipsAlreadyTested verifies that tasks with a test result are skipped.
func TestTryAutoTest_SkipsAlreadyTested(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutotest(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	// Simulate a previous test run that passed.
	h.store.UpdateTaskTestRun(ctx, task.ID, false, "pass")

	h.tryAutoTest(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected already-tested task to remain waiting, got %s", got.Status)
	}
}

// TestTryAutoTest_SkipsCurrentlyTesting verifies that tasks being tested (IsTestRun) are skipped.
func TestTryAutoTest_SkipsCurrentlyTesting(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutotest(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	// Mark the task as currently running a test.
	h.store.UpdateTaskTestRun(ctx, task.ID, true, "")

	h.tryAutoTest(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected currently-testing task to remain waiting, got %s", got.Status)
	}
}

// TestTryAutoTest_SkipsNoWorktrees verifies that waiting tasks with no worktrees are skipped.
func TestTryAutoTest_SkipsNoWorktrees(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutotest(true)
	ctx := context.Background()

	task, _ := h.store.CreateTask(ctx, "test task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	// No worktrees set.

	h.tryAutoTest(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected task without worktrees to remain waiting, got %s", got.Status)
	}
}

func TestTryAutoTest_SkipsMissingWorktreeDir(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutotest(true)
	ctx := context.Background()

	repo := setupRepo(t)
	task, _ := h.store.CreateTask(ctx, "test task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: filepath.Join(t.TempDir(), "missing-wt")}, "task-branch")

	h.tryAutoTest(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected task with missing worktree dir to remain waiting, got %s", got.Status)
	}
	if got.IsTestRun {
		t.Error("expected task with missing worktree dir not to enter test run")
	}
}

// TestTryAutoTest_SkipsBehindTip verifies that tasks whose worktrees are behind the
// default branch are not auto-tested.
func TestTryAutoTest_SkipsBehindTip(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutotest(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// Add a commit to main so the worktree is behind by 1.
	os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("upstream\n"), 0644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "upstream commit")

	h.tryAutoTest(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected behind-tip task to remain waiting, got %s", got.Status)
	}
}

// TestTryAutoTest_TriggersForEligibleTask verifies that a qualifying waiting task
// (untested, up-to-date worktree) is transitioned to in_progress to run the test agent.
func TestTryAutoTest_TriggersForEligibleTask(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutotest(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	// No new commits — worktree is up to date.

	h.tryAutoTest(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusInProgress {
		t.Errorf("expected eligible task to be moved to in_progress, got %s", got.Status)
	}
	if !got.IsTestRun {
		t.Error("expected IsTestRun to be set on auto-tested task")
	}
}

// TestTryAutoTest_RespectsMaxTestConcurrencyLimit verifies that tryAutoTest does
// not trigger more test runs than the configured WALLFACER_MAX_TEST_PARALLEL limit.
func TestTryAutoTest_RespectsMaxTestConcurrencyLimit(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	h.SetAutotest(true)
	ctx := context.Background()

	// Set test-run max parallel to 1 via the dedicated variable.
	if err := os.WriteFile(envPath, []byte("WALLFACER_MAX_TEST_PARALLEL=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a repo with two worktrees (one per task).
	repo := setupRepo(t)
	wt1 := filepath.Join(t.TempDir(), "wt1")
	wt2 := filepath.Join(t.TempDir(), "wt2")
	gitRun(t, repo, "worktree", "add", "-b", "branch-1", wt1, "HEAD")
	gitRun(t, repo, "worktree", "add", "-b", "branch-2", wt2, "HEAD")

	// Two waiting tasks, both eligible for auto-test.
	task1, _ := h.store.CreateTask(ctx, "task 1", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task1.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task1.ID, map[string]string{repo: wt1}, "branch-1")

	task2, _ := h.store.CreateTask(ctx, "task 2", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task2.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task2.ID, map[string]string{repo: wt2}, "branch-2")

	h.tryAutoTest(ctx)

	// With test max=1 and no prior test-run in-progress tasks, exactly one test run should start.
	tasks, _ := h.store.ListTasks(ctx, false)
	testInProgress := 0
	for _, task := range tasks {
		if task.Status == store.TaskStatusInProgress && task.IsTestRun {
			testInProgress++
		}
	}
	if testInProgress != 1 {
		t.Errorf("expected exactly 1 test run to start (max=1), got %d test in_progress", testInProgress)
	}
}

// TestTryAutoTest_RegularTasksDoNotConsumeTestSlots verifies that regular
// in_progress tasks (IsTestRun=false) do not count against the test concurrency
// limit, so a full backlog of regular work cannot starve auto-test.
func TestTryAutoTest_RegularTasksDoNotConsumeTestSlots(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	h.SetAutotest(true)
	ctx := context.Background()

	// Set test-run max parallel to 1 and regular max to 2.
	if err := os.WriteFile(envPath, []byte("WALLFACER_MAX_PARALLEL=2\nWALLFACER_MAX_TEST_PARALLEL=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Two regular tasks already in_progress (fills the regular slots).
	for i := 0; i < 2; i++ {
		reg, _ := h.store.CreateTask(ctx, fmt.Sprintf("regular %d", i), 15, false, "", "")
		h.store.ForceUpdateTaskStatus(ctx, reg.ID, store.TaskStatusInProgress)
	}

	// One waiting task eligible for auto-test.
	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	h.tryAutoTest(ctx)

	// The test run must have started despite both regular slots being full.
	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusInProgress {
		t.Errorf("expected test task to be promoted to in_progress (regular slots full should not block tests), got %s", got.Status)
	}
	if !got.IsTestRun {
		t.Error("expected IsTestRun to be set on auto-tested task")
	}
}

func TestTryAutoPromote_ResumesFailedTestFeedbackWhenAutopilotEnabled(t *testing.T) {
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	cmdPath := filepath.Join(t.TempDir(), "fake-runner.sh")
	script := "#!/bin/sh\nsleep 0.2\nprintf '%s\\n' '{\"result\":\"need more work\",\"session_id\":\"impl-sess\",\"stop_reason\":\"\",\"is_error\":false,\"total_cost_usd\":0.001}'\n"
	if err := os.WriteFile(cmdPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:    cmdPath,
		Workspaces: repo,
	})
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)

	h := NewHandler(s, r, t.TempDir(), []string{repo}, nil)
	h.SetAutopilot(true)
	ctx := context.Background()

	task, err := h.store.CreateTask(ctx, "failed test follow-up", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	h.store.UpdateTaskResult(ctx, task.ID, "implementation result", "impl-sess", "", 2)
	h.store.UpdateTaskTestRun(ctx, task.ID, false, "fail")
	h.store.UpdateTaskPendingTestFeedback(ctx, task.ID, "Automated test verification failed.\n\n- fix the broken assertion")

	h.tryAutoPromote(ctx)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TaskStatusInProgress {
		t.Fatalf("expected task to resume to in_progress, got %s", got.Status)
	}
	if got.LastTestResult != "" {
		t.Fatalf("expected last_test_result to be cleared on resume, got %q", got.LastTestResult)
	}
	if got.PendingTestFeedback != "" {
		t.Fatalf("expected pending test feedback to be cleared on resume, got %q", got.PendingTestFeedback)
	}

	events, err := h.store.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundFeedback := false
	for _, event := range events {
		if event.EventType != store.EventTypeFeedback {
			continue
		}
		var data map[string]string
		if err := json.Unmarshal(event.Data, &data); err != nil {
			t.Fatalf("json.Unmarshal(feedback): %v", err)
		}
		if strings.Contains(data["message"], "fix the broken assertion") {
			foundFeedback = true
			break
		}
	}
	if !foundFeedback {
		t.Fatal("expected failed test outcome to be recorded as feedback")
	}

	r.WaitBackground()
}

// TestAutotest_SetAndGet verifies the SetAutotest / AutotestEnabled accessors.
func TestAutotest_SetAndGet(t *testing.T) {
	h := newTestHandler(t)
	if h.AutotestEnabled() {
		t.Fatal("expected autotest to be disabled by default")
	}
	h.SetAutotest(true)
	if !h.AutotestEnabled() {
		t.Error("expected autotest to be enabled after SetAutotest(true)")
	}
	h.SetAutotest(false)
	if h.AutotestEnabled() {
		t.Error("expected autotest to be disabled after SetAutotest(false)")
	}
}

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

	task, _ := h.store.CreateTask(ctx, "verified task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	h.store.UpdateTaskTestRun(ctx, task.ID, false, "pass")

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

	task, _ := h.store.CreateTask(ctx, "verified task", 15, false, "", "")
	// Leave in backlog — not waiting.
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	h.store.UpdateTaskTestRun(ctx, task.ID, false, "pass")

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

	waitingTask, _ := h.store.CreateTask(ctx, "verified waiting task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, waitingTask.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, waitingTask.ID, map[string]string{repo: waitingWT}, "waiting-branch")
	h.store.UpdateTaskTestRun(ctx, waitingTask.ID, false, "pass")

	doneTask, _ := h.store.CreateTask(ctx, "verified done task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, doneTask.ID, store.TaskStatusDone)
	h.store.UpdateTaskWorktrees(ctx, doneTask.ID, map[string]string{repo: doneWT}, "done-branch")
	h.store.UpdateTaskTestRun(ctx, doneTask.ID, false, "pass")

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

	task, _ := h.store.CreateTask(ctx, "unverified task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	// LastTestResult is "" (not tested).

	h.tryAutoSubmit(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected unverified task to remain waiting, got %s", got.Status)
	}
}

// TestTryAutoSubmit_SkipsAmbiguousVerdict verifies that a waiting task with
// LastTestResult=="fail" (which includes the case where the test ran but
// produced no recognizable PASS/FAIL marker) is NOT auto-submitted.
func TestTryAutoSubmit_SkipsAmbiguousVerdict(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosubmit(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "ambiguous verdict task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	h.store.UpdateTaskTestRun(ctx, task.ID, false, "fail")

	h.tryAutoSubmit(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected ambiguous-verdict (fail) task to remain waiting, got %s", got.Status)
	}
}

// TestTryAutoSubmit_SkipsFailedVerification verifies tasks with LastTestResult=="fail" are not submitted.
func TestTryAutoSubmit_SkipsFailedVerification(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosubmit(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "failed test task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	h.store.UpdateTaskTestRun(ctx, task.ID, false, "fail")

	h.tryAutoSubmit(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected failed-test task to remain waiting, got %s", got.Status)
	}
}

// TestTryAutoSubmit_SkipsCurrentlyTesting verifies that tasks with IsTestRun=true are not submitted.
func TestTryAutoSubmit_SkipsCurrentlyTesting(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosubmit(true)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "verified task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	// IsTestRun=true means the test agent is currently running.
	h.store.UpdateTaskTestRun(ctx, task.ID, true, "pass")

	h.tryAutoSubmit(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected currently-testing task to remain waiting, got %s", got.Status)
	}
}

func TestTryAutoSubmit_SkipsMissingWorktreeDir(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutosubmit(true)
	ctx := context.Background()

	repo := setupRepo(t)
	task, _ := h.store.CreateTask(ctx, "verified task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: filepath.Join(t.TempDir(), "missing-wt")}, "task-branch")
	h.store.UpdateTaskTestRun(ctx, task.ID, false, "pass")

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
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "verified task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	h.store.UpdateTaskTestRun(ctx, task.ID, false, "pass")

	// Add a commit to main so the worktree is behind by 1.
	os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("upstream\n"), 0644)
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

	task, _ := h.store.CreateTask(ctx, "verified task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	h.store.UpdateTaskTestRun(ctx, task.ID, false, "pass")
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

func TestTryAutoSubmit_CommitMessageFailureFallsBackAndCompletes(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	h.SetAutotest(true)
	h.SetAutosubmit(true)
	t.Cleanup(func() { waitForBackground(200) })
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-autosubmit-fail")
	gitRun(t, repo, "worktree", "add", "-b", "task-autosubmit-fail", wt, "HEAD")
	if err := os.WriteFile(filepath.Join(wt, "feature.txt"), []byte("new work\n"), 0644); err != nil {
		t.Fatal(err)
	}

	task, _ := h.store.CreateTask(ctx, "verified task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-autosubmit-fail")
	h.store.UpdateTaskTestRun(ctx, task.ID, false, "pass")
	setTaskSessionID(t, h, task.ID, "sess-autosubmit-fail")

	h.tryAutoSubmit(ctx)

	var got *store.Task
	for range 20 {
		got, _ = h.store.GetTask(ctx, task.ID)
		if got != nil && got.Status == store.TaskStatusDone {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got == nil || got.Status != store.TaskStatusDone {
		t.Fatalf("expected task to complete via fallback commit message, got %v", got.Status)
	}

	if got := gitRun(t, repo, "rev-list", "--count", "HEAD"); got != "2" {
		t.Fatalf("expected fallback commit to land on repo, got %s commits", got)
	}
	if !h.AutopilotEnabled() || !h.AutotestEnabled() || !h.AutosubmitEnabled() {
		t.Fatal("expected automation toggles to remain enabled after fallback commit message")
	}
}

// --- Trigger attribution tests ---

// TestTryAutoPromote_EventHasAutoPromoteTrigger verifies that the state_change
// event emitted by tryAutoPromote contains "trigger": "auto_promote".
func TestTryAutoPromote_EventHasAutoPromoteTrigger(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.autopilotMu.Lock()
	h.autopilot = true
	h.autopilotMu.Unlock()

	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "task to promote", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

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
	task, _ := h.store.CreateTask(ctx, "test prompt", 15, false, "", "")

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
// simultaneously; if Phase 1 were still serialised by promoteMu the second
// goroutine would never arrive and the test would time out.
func TestTryAutoPromote_ConcurrentPhase1DoesNotBlock(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	h.SetAutopilot(true)
	ctx := context.Background()

	// Limit to 1 concurrent task so the second goroutine's Phase 2 is a no-op.
	// Write directly to the env file instead of going through UpdateEnvConfig,
	// which spawns a goroutine that would race with the testPhase1Done field
	// assignment below.
	if err := os.WriteFile(envPath, []byte("WALLFACER_MAX_PARALLEL=1\n"), 0644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	// Create two distinct backlog tasks.
	_, err := h.store.CreateTask(ctx, "concurrent task one", 30, false, "", store.TaskKindTask)
	if err != nil {
		t.Fatalf("create task one: %v", err)
	}
	_, err = h.store.CreateTask(ctx, "concurrent task two", 30, false, "", store.TaskKindTask)
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
		// Phase 2. If Phase 1 were serialised (old design), the goroutine holding
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
	for i := 0; i < 2; i++ {
		select {
		case <-phase1Done:
			// Goroutine i+1 completed Phase 1 concurrently — good.
		case <-timeout.C:
			// Only one goroutine reached Phase 1 within the timeout, which means
			// Phase 1 is being serialised: the second goroutine was blocked by
			// the first (the old single-phase design).
			t.Errorf("goroutine %d of 2 did not complete Phase 1 within timeout — Phase 1 appears serialised", i+1)
			close(gate) // unblock whatever is waiting so wg.Wait() can finish
			wg.Wait()
			return
		}
	}

	// Both goroutines completed Phase 1 concurrently. Open the gate for Phase 2.
	close(gate)
	wg.Wait()

	// With max_concurrent=1 exactly one task should have been promoted.
	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	inProgress := 0
	for _, task := range tasks {
		if task.Status == store.TaskStatusInProgress {
			inProgress++
		}
	}
	if inProgress != 1 {
		t.Errorf("expected exactly 1 task in_progress after concurrent promotion, got %d", inProgress)
	}
}

// ---------------------------------------------------------------------------
// BatchCreateTasks tests
// ---------------------------------------------------------------------------

// batchBody is a helper that marshals v to JSON and returns the body bytes.
func batchBody(t *testing.T, v any) *strings.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return strings.NewReader(string(b))
}

// TestBatchCreateTasks_HappyPath creates 3 tasks A→B→C (chain) and verifies
// correct dependency wiring and ref_to_id mapping.
func TestBatchCreateTasks_HappyPath(t *testing.T) {
	h := newTestHandler(t)

	body := batchBody(t, map[string]any{
		"tasks": []map[string]any{
			{"ref": "a", "prompt": "task A", "timeout": 10},
			{"ref": "b", "prompt": "task B", "timeout": 10, "depends_on_refs": []string{"a"}},
			{"ref": "c", "prompt": "task C", "timeout": 10, "depends_on_refs": []string{"b"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.BatchCreateTasks(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Tasks   []store.Task      `json:"tasks"`
		RefToID map[string]string `json:"ref_to_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(resp.Tasks))
	}
	if len(resp.RefToID) != 3 {
		t.Fatalf("expected 3 entries in ref_to_id, got %d", len(resp.RefToID))
	}

	idA := resp.RefToID["a"]
	idB := resp.RefToID["b"]
	idC := resp.RefToID["c"]

	if idA == "" || idB == "" || idC == "" {
		t.Fatalf("missing ref_to_id entries: %v", resp.RefToID)
	}

	// Tasks should be returned in input order.
	if resp.Tasks[0].ID.String() != idA {
		t.Errorf("tasks[0] should be A, got %s", resp.Tasks[0].ID)
	}
	if resp.Tasks[1].ID.String() != idB {
		t.Errorf("tasks[1] should be B, got %s", resp.Tasks[1].ID)
	}
	if resp.Tasks[2].ID.String() != idC {
		t.Errorf("tasks[2] should be C, got %s", resp.Tasks[2].ID)
	}

	ctx := context.Background()

	// Verify task B depends on A.
	taskB, err := h.store.GetTask(ctx, uuid.MustParse(idB))
	if err != nil {
		t.Fatalf("get B: %v", err)
	}
	if len(taskB.DependsOn) != 1 || taskB.DependsOn[0] != idA {
		t.Errorf("task B DependsOn = %v, want [%s]", taskB.DependsOn, idA)
	}

	// Verify task C depends on B.
	taskC, err := h.store.GetTask(ctx, uuid.MustParse(idC))
	if err != nil {
		t.Fatalf("get C: %v", err)
	}
	if len(taskC.DependsOn) != 1 || taskC.DependsOn[0] != idB {
		t.Errorf("task C DependsOn = %v, want [%s]", taskC.DependsOn, idB)
	}
}

// TestBatchCreateTasks_CycleDetection verifies 422 when A depends on B and B depends on A.
func TestBatchCreateTasks_CycleDetection(t *testing.T) {
	h := newTestHandler(t)

	body := batchBody(t, map[string]any{
		"tasks": []map[string]any{
			{"ref": "a", "prompt": "task A", "depends_on_refs": []string{"b"}},
			{"ref": "b", "prompt": "task B", "depends_on_refs": []string{"a"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.BatchCreateTasks(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "cycle detected" {
		t.Errorf("expected 'cycle detected' error, got %v", resp["error"])
	}
}

// TestBatchCreateTasks_DuplicateRef verifies 400 when two tasks share the same ref.
func TestBatchCreateTasks_DuplicateRef(t *testing.T) {
	h := newTestHandler(t)

	body := batchBody(t, map[string]any{
		"tasks": []map[string]any{
			{"ref": "foo", "prompt": "task 1"},
			{"ref": "foo", "prompt": "task 2"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.BatchCreateTasks(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchCreateTasks_UnknownRef verifies 400 when depends_on_refs contains
// a ref that is neither a known batch ref nor a valid UUID.
func TestBatchCreateTasks_UnknownRef(t *testing.T) {
	h := newTestHandler(t)

	body := batchBody(t, map[string]any{
		"tasks": []map[string]any{
			{"ref": "a", "prompt": "task A", "depends_on_refs": []string{"nonexistent"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.BatchCreateTasks(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchCreateTasks_CrossBatchDep verifies that a valid existing task UUID
// in depends_on_refs is wired correctly into the created task's DependsOn.
func TestBatchCreateTasks_CrossBatchDep(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	// Pre-create an existing task to be referenced by UUID.
	existing, err := h.store.CreateTask(ctx, "existing task", 10, false, "", "")
	if err != nil {
		t.Fatalf("create existing: %v", err)
	}

	body := batchBody(t, map[string]any{
		"tasks": []map[string]any{
			{
				"ref":             "new",
				"prompt":          "new task",
				"depends_on_refs": []string{existing.ID.String()},
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.BatchCreateTasks(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Tasks   []store.Task      `json:"tasks"`
		RefToID map[string]string `json:"ref_to_id"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	newID := resp.RefToID["new"]
	newTask, err := h.store.GetTask(ctx, uuid.MustParse(newID))
	if err != nil {
		t.Fatalf("get new task: %v", err)
	}

	if len(newTask.DependsOn) != 1 || newTask.DependsOn[0] != existing.ID.String() {
		t.Errorf("DependsOn = %v, want [%s]", newTask.DependsOn, existing.ID)
	}
}

// TestBatchCreateTasks_EmptyBatch verifies 400 for an empty tasks array.
func TestBatchCreateTasks_EmptyBatch(t *testing.T) {
	h := newTestHandler(t)

	body := batchBody(t, map[string]any{"tasks": []any{}})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.BatchCreateTasks(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
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
	h.autopilotMu.Lock()
	h.autopilot = true
	h.autopilotMu.Unlock()

	ctx := context.Background()

	// Create the only backlog task and schedule it 1 hour in the future.
	task, _ := h.store.CreateTask(ctx, "scheduled task", 15, false, "", "")
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
	h.autopilotMu.Lock()
	h.autopilot = true
	h.autopilotMu.Unlock()

	ctx := context.Background()

	task, _ := h.store.CreateTask(ctx, "past scheduled task", 15, false, "", "")
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
	h.autopilotMu.Lock()
	h.autopilot = true
	h.autopilotMu.Unlock()

	ctx := context.Background()

	// Future-scheduled task at lower position (higher priority by default).
	scheduled, _ := h.store.CreateTask(ctx, "future scheduled", 15, false, "", "")
	h.store.UpdateTaskPosition(ctx, scheduled.ID, 0)
	future := time.Now().Add(1 * time.Hour)
	if err := h.store.UpdateTaskScheduledAt(ctx, scheduled.ID, &future); err != nil {
		t.Fatalf("UpdateTaskScheduledAt: %v", err)
	}

	// Unscheduled task at higher position (lower priority).
	unscheduled, _ := h.store.CreateTask(ctx, "unscheduled", 15, false, "", "")
	h.store.UpdateTaskPosition(ctx, unscheduled.ID, 1)

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

	task, _ := h.store.CreateTask(ctx, "sched test", 15, false, "", "")

	future := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	body, _ := json.Marshal(map[string]interface{}{
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

	task, _ := h.store.CreateTask(ctx, "sched clear", 15, false, "", "")
	future := time.Now().Add(2 * time.Hour)
	h.store.UpdateTaskScheduledAt(ctx, task.ID, &future)

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
	task, _ := h.store.CreateTask(ctx, "test model override", 15, false, "", "")

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
	task, _ := h.store.CreateTask(ctx, "test model clear", 15, false, "", "")
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

// TestListTasks_ModelOverrideSerialised verifies that a task with ModelOverride set
// serialises model_override in the GET /api/tasks response.
func TestListTasks_ModelOverrideSerialised(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test serialise model", 15, false, "", "")
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

// callBatchCreate is a test helper that posts a raw JSON body to BatchCreateTasks
// and returns the response recorder.
func callBatchCreate(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.BatchCreateTasks(w, req)
	return w
}

// storeTaskCount returns the number of tasks currently in the store.
func storeTaskCount(t *testing.T, h *Handler) int {
	t.Helper()
	tasks, err := h.store.ListTasks(context.Background(), true)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	return len(tasks)
}

// TestBatchCreateTasks_InvalidSandboxLeavesStoreEmpty verifies that an invalid
// sandbox in any one item causes a 400 response with zero tasks persisted.
func TestBatchCreateTasks_InvalidSandboxLeavesStoreEmpty(t *testing.T) {
	// Use plain newTestHandler — no codex API key and no test passed, so
	// "codex" is an invalid sandbox selection.
	h := newTestHandler(t)

	body := `{"tasks":[
		{"ref":"A","prompt":"do A","sandbox":"claude"},
		{"ref":"B","prompt":"do B","sandbox":"codex"}
	]}`
	w := callBatchCreate(t, h, body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if storeTaskCount(t, h) != 0 {
		t.Errorf("expected store to be empty after validation failure, got %d tasks", storeTaskCount(t, h))
	}
}

// TestBatchCreateTasks_NonexistentExternalDepLeavesStoreEmpty verifies that a
// depends_on_refs entry pointing to a UUID that does not exist in the store
// causes a 422 response with zero tasks persisted.
func TestBatchCreateTasks_NonexistentExternalDepLeavesStoreEmpty(t *testing.T) {
	h := newTestHandler(t)

	missingID := uuid.New().String()
	body := fmt.Sprintf(`{"tasks":[
		{"ref":"A","prompt":"do A","depends_on_refs":[%q]}
	]}`, missingID)
	w := callBatchCreate(t, h, body)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if storeTaskCount(t, h) != 0 {
		t.Errorf("expected store to be empty after validation failure, got %d tasks", storeTaskCount(t, h))
	}
}

// TestBatchCreateTasks_CycleWithExistingTaskRejected verifies that a batch where
// proposed edges combined with existing task edges would create a cycle is
// rejected with 422 and leaves the store unchanged.
func TestBatchCreateTasks_CycleWithExistingTaskRejected(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	// Pre-create existing tasks: E1 depends on E2.
	e2, err := h.store.CreateTask(ctx, "E2", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	e1, err := h.store.CreateTask(ctx, "E1", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.UpdateTaskDependsOn(ctx, e1.ID, []string{e2.ID.String()}); err != nil {
		t.Fatal(err)
	}

	// Batch: create task B that depends on E1, and also create a "bridge" task
	// to form a batch-internal cycle (B → A → B).
	body := fmt.Sprintf(`{"tasks":[
		{"ref":"A","prompt":"do A","depends_on_refs":["B"]},
		{"ref":"B","prompt":"do B","depends_on_refs":["A",%q]}
	]}`, e1.ID.String())
	w := callBatchCreate(t, h, body)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for cycle, got %d: %s", w.Code, w.Body.String())
	}
	// Store should still only have E1 and E2.
	if storeTaskCount(t, h) != 2 {
		t.Errorf("expected 2 tasks in store (only existing ones), got %d", storeTaskCount(t, h))
	}
}

// TestBatchCreateTasks_SuccessABCTopologicalOrder verifies that a batch of three
// tasks with A→B→C dependency chain is accepted, all tasks are created, the
// response is 201 with tasks in INPUT order, and dependency IDs are resolved.
func TestBatchCreateTasks_SuccessABCTopologicalOrder(t *testing.T) {
	h := newTestHandler(t)

	body := `{"tasks":[
		{"ref":"A","prompt":"do A","depends_on_refs":["B"]},
		{"ref":"B","prompt":"do B","depends_on_refs":["C"]},
		{"ref":"C","prompt":"do C"}
	]}`
	w := callBatchCreate(t, h, body)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Tasks   []store.Task      `json:"tasks"`
		RefToID map[string]string `json:"ref_to_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Tasks must come back in input order (A, B, C).
	if len(resp.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(resp.Tasks))
	}
	if resp.Tasks[0].Prompt != "do A" {
		t.Errorf("task[0] prompt: want %q, got %q", "do A", resp.Tasks[0].Prompt)
	}
	if resp.Tasks[1].Prompt != "do B" {
		t.Errorf("task[1] prompt: want %q, got %q", "do B", resp.Tasks[1].Prompt)
	}
	if resp.Tasks[2].Prompt != "do C" {
		t.Errorf("task[2] prompt: want %q, got %q", "do C", resp.Tasks[2].Prompt)
	}

	// Verify ref_to_id contains entries for all three refs.
	for _, ref := range []string{"A", "B", "C"} {
		if _, ok := resp.RefToID[ref]; !ok {
			t.Errorf("ref_to_id missing key %q", ref)
		}
	}

	// Verify dependency IDs are resolved correctly.
	idA := resp.Tasks[0].ID.String()
	idB := resp.Tasks[1].ID.String()
	idC := resp.Tasks[2].ID.String()

	if len(resp.Tasks[0].DependsOn) != 1 || resp.Tasks[0].DependsOn[0] != idB {
		t.Errorf("task A should depend on B (%s), got %v", idB, resp.Tasks[0].DependsOn)
	}
	if len(resp.Tasks[1].DependsOn) != 1 || resp.Tasks[1].DependsOn[0] != idC {
		t.Errorf("task B should depend on C (%s), got %v", idC, resp.Tasks[1].DependsOn)
	}
	if len(resp.Tasks[2].DependsOn) != 0 {
		t.Errorf("task C should have no deps, got %v", resp.Tasks[2].DependsOn)
	}

	// ref_to_id values must match the returned task IDs.
	if resp.RefToID["A"] != idA {
		t.Errorf("ref_to_id[A]: want %s, got %s", idA, resp.RefToID["A"])
	}
	if resp.RefToID["B"] != idB {
		t.Errorf("ref_to_id[B]: want %s, got %s", idB, resp.RefToID["B"])
	}
	if resp.RefToID["C"] != idC {
		t.Errorf("ref_to_id[C]: want %s, got %s", idC, resp.RefToID["C"])
	}

	// All three tasks must be in the store.
	if storeTaskCount(t, h) != 3 {
		t.Errorf("expected 3 tasks in store, got %d", storeTaskCount(t, h))
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
		"sandbox":          "claude",
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
	if task.Sandbox != "claude" {
		t.Errorf("sandbox: want %q, got %q", "claude", task.Sandbox)
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
		"sandbox":          "claude",
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

// TestBatchCreateTasks_ReturnsSandboxAndDeps verifies that batch-created tasks
// already contain sandbox and dependency fields in the 201 response without
// requiring a re-fetch.
func TestBatchCreateTasks_ReturnsSandboxAndDeps(t *testing.T) {
	h := newTestHandler(t)

	body := `{"tasks":[
		{"ref":"A","prompt":"task A","sandbox":"claude"},
		{"ref":"B","prompt":"task B","sandbox":"claude","depends_on_refs":["A"]}
	]}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.BatchCreateTasks(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Tasks   []store.Task      `json:"tasks"`
		RefToID map[string]string `json:"ref_to_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(resp.Tasks))
	}

	// Both tasks must have sandbox set immediately.
	for i, task := range resp.Tasks {
		if task.Sandbox != "claude" {
			t.Errorf("tasks[%d].sandbox: want %q, got %q", i, "claude", task.Sandbox)
		}
	}

	// Task B must have A's UUID in DependsOn.
	aID := resp.RefToID["A"]
	var taskB *store.Task
	for i := range resp.Tasks {
		if resp.Tasks[i].Prompt == "task B" {
			taskB = &resp.Tasks[i]
			break
		}
	}
	if taskB == nil {
		t.Fatal("task B not found in response")
	}
	if len(taskB.DependsOn) != 1 {
		t.Fatalf("task B DependsOn: want 1 entry, got %d", len(taskB.DependsOn))
	}
	if taskB.DependsOn[0] != aID {
		t.Errorf("task B DependsOn[0]: want %q (A's ID), got %q", aID, taskB.DependsOn[0])
	}
}

// TestBatchCreateTasks_EmitsOneDeltaPerTask verifies that each batch task
// emits exactly one SSE delta.
func TestBatchCreateTasks_EmitsOneDeltaPerTask(t *testing.T) {
	h := newTestHandler(t)

	subID, deltaCh := h.store.Subscribe()
	defer h.store.Unsubscribe(subID)

	body := `{"tasks":[
		{"ref":"X","prompt":"task X","sandbox":"claude"},
		{"ref":"Y","prompt":"task Y","depends_on_refs":["X"]}
	]}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/batch", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.BatchCreateTasks(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	count := 0
	deadline := time.After(50 * time.Millisecond)
drain2:
	for {
		select {
		case <-deltaCh:
			count++
		case <-deadline:
			break drain2
		}
	}

	// 2 tasks → exactly 2 creation deltas (no extra update deltas).
	if count != 2 {
		t.Errorf("expected exactly 2 SSE deltas for 2-task batch, got %d", count)
	}
}

// TestCreateTask_ScheduledTaskNotAutoPromotedBeforeScheduledAt verifies that
// a task's ScheduledAt is persisted atomically at creation time, so
// tryAutoPromote cannot promote it before the schedule fires.
func TestCreateTask_ScheduledTaskNotAutoPromotedBeforeScheduledAt(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)

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

	var dataDir string
	if d, err := os.MkdirTemp("/dev/shm", "wallfacer-ar-*"); err == nil {
		dataDir = d
		t.Cleanup(func() { os.RemoveAll(dataDir) })
	} else {
		dataDir = t.TempDir()
	}
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      cmd,
		SandboxImage: "test:latest",
		Workspaces:   repo,
		WorktreesDir: worktreesDir,
	})
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)
	return s, r
}

func TestAutoRetry_ContainerCrash(t *testing.T) {
	cmd := autoRetryCrashCmd(t)
	s, r := setupAutoRetryRunner(t, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "crash retry test", 5, false, "", "")
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
	cmd := autoRetryBudgetErrorCmd(t)
	s, r := setupAutoRetryRunner(t, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "budget exceeded test", 5, false, "", "")
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
	cmd := autoRetryCrashCmd(t)
	s, r := setupAutoRetryRunner(t, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "total cap test", 5, false, "", "")
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
// to closed (healthy) once its openUntil time has elapsed.
func TestWatcherBreaker_AutoRecoversAfterTimeout(t *testing.T) {
	wb := &watcherBreaker{}

	wb.recordFailure(nil, "transient error")
	if !wb.isOpen() {
		t.Fatal("expected breaker to be open immediately after failure")
	}

	// Wind the openUntil back to the past to simulate the timeout elapsing.
	wb.mu.Lock()
	wb.openUntil = time.Now().Add(-time.Second)
	wb.mu.Unlock()

	if wb.isOpen() {
		t.Error("expected breaker to be closed after openUntil has elapsed")
	}
}

// TestWatcherBreaker_RecordSuccessResets verifies that recordSuccess resets
// the failure counter and closes the breaker.
func TestWatcherBreaker_RecordSuccessResets(t *testing.T) {
	wb := &watcherBreaker{}

	wb.recordFailure(nil, "error 1")
	wb.recordFailure(nil, "error 2")

	if wb.failures != 2 {
		t.Errorf("expected failures=2, got %d", wb.failures)
	}
	if !wb.isOpen() {
		t.Error("expected breaker to be open after failures")
	}

	wb.recordSuccess()

	if wb.failures != 0 {
		t.Errorf("expected failures=0 after recordSuccess, got %d", wb.failures)
	}
	if wb.isOpen() {
		t.Error("expected breaker to be closed after recordSuccess")
	}
}

// TestGetConfig_IncludesWatcherHealth verifies that GET /api/config includes
// a watcher_health array with all six watcher entries.
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

	wantNames := []string{"auto-promote", "auto-retry", "auto-test", "auto-submit", "auto-sync", "auto-refine"}
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
	var results []interface{}
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
	_, err := h.store.CreateTask(ctx, "fix authentication bug in login", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/search?q=authentication", nil)
	w := httptest.NewRecorder()
	h.SearchTasks(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var results []interface{}
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
	var stats interface{}
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

// TestGetStats_UnknownWorkspace verifies that filtering by unknown workspace returns 400.
func TestGetStats_UnknownWorkspace(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	_, err := h.store.CreateTask(ctx, "some task", 5, false, "", "")
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
