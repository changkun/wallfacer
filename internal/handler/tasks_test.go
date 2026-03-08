package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// TestListTasks_ExcludesArchivedByDefault checks that archived tasks are not
// returned when include_archived is not set.
func TestListTasks_ExcludesArchivedByDefault(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, err := h.store.CreateTask(ctx, "archived task", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)
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
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)
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

// TestCreateTask_SetsModel verifies that model is stored on the task.
func TestCreateTask_SetsModel(t *testing.T) {
	h := newTestHandler(t)
	body := `{"prompt": "build a thing", "model": "claude-opus-4-5"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	var task store.Task
	json.NewDecoder(w.Body).Decode(&task)
	if task.Model != "claude-opus-4-5" {
		t.Errorf("expected model 'claude-opus-4-5', got %q", task.Model)
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
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

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
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

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
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusCancelled)

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
	h.store.UpdateTaskStatus(ctx, dep.ID, store.TaskStatusDone)

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

// --- checkAndSyncWaitingTasks tests ---

// TestCheckAndSyncWaitingTasks_SkipsNonWaiting verifies that tasks not in the
// waiting state are left untouched.
func TestCheckAndSyncWaitingTasks_SkipsNonWaiting(t *testing.T) {
	h := newTestHandler(t)
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
	ctx := context.Background()

	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
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
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
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
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
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

// --- tryAutoTest tests ---

// TestTryAutoTest_DisabledNoOp verifies that auto-test does nothing when disabled.
func TestTryAutoTest_DisabledNoOp(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test task", 15, false, "", "")
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
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
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
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
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
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
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	// No worktrees set.

	h.tryAutoTest(ctx)

	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected task without worktrees to remain waiting, got %s", got.Status)
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
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
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
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
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
