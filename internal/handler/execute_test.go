package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// createWaitingTask creates a task in the store and moves it to waiting status.
func createWaitingTask(t *testing.T, h *Handler, prompt string) uuid.UUID {
	t.Helper()
	task, err := h.store.CreateTask(context.Background(), prompt, 15, false, "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := h.store.UpdateTaskStatus(context.Background(), task.ID, "waiting"); err != nil {
		t.Fatalf("set waiting: %v", err)
	}
	return task.ID
}

func TestTestTask_RejectsNonWaiting(t *testing.T) {
	h := newTestHandler(t)

	task, err := h.store.CreateTask(context.Background(), "build a widget", 15, false, "")
	if err != nil {
		t.Fatal(err)
	}
	// task is in "backlog" — TestTask should reject it.
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/test", bytes.NewBufferString(`{}`))
	req.SetPathValue("id", task.ID.String())
	w := httptest.NewRecorder()

	h.TestTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-waiting task, got %d", w.Code)
	}
}

func TestTestTask_CreatesTestTaskWithDefaultPrompt(t *testing.T) {
	h := newTestHandler(t)

	parentPrompt := "implement a login form with email and password fields"
	parentID := createWaitingTask(t, h, parentPrompt)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+parentID.String()+"/test", bytes.NewBufferString(`{}`))
	req.SetPathValue("id", parentID.String())
	w := httptest.NewRecorder()

	h.TestTask(w, req, parentID)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	testTaskIDStr := resp["test_task_id"]
	if testTaskIDStr == "" {
		t.Fatal("response missing test_task_id")
	}
	if resp["status"] != "created" {
		t.Errorf("expected status=created, got %q", resp["status"])
	}

	testTaskID, err := uuid.Parse(testTaskIDStr)
	if err != nil {
		t.Fatalf("invalid test_task_id %q: %v", testTaskIDStr, err)
	}

	testTask, err := h.store.GetTask(context.Background(), testTaskID)
	if err != nil {
		t.Fatalf("get test task: %v", err)
	}
	if !testTask.MountWorktrees {
		t.Error("test task should have mount_worktrees=true")
	}
	if testTask.Status != "backlog" {
		t.Errorf("expected test task in backlog, got %q", testTask.Status)
	}
	if !strings.Contains(testTask.Prompt, parentPrompt) {
		t.Error("test task prompt should contain the original task prompt")
	}

	// A system event should have been added to the parent task.
	events, err := h.store.GetEvents(context.Background(), parentID)
	if err != nil {
		t.Fatalf("get parent events: %v", err)
	}
	var foundSystem bool
	for _, ev := range events {
		if ev.EventType == store.EventTypeSystem {
			foundSystem = true
			break
		}
	}
	if !foundSystem {
		t.Error("expected system event on parent task after test agent launch")
	}
}

func TestTestTask_IncludesCriteriaInPrompt(t *testing.T) {
	h := newTestHandler(t)

	parentID := createWaitingTask(t, h, "build a widget")

	criteria := "the widget must render in under 100ms"
	body, _ := json.Marshal(map[string]string{"criteria": criteria})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+parentID.String()+"/test", bytes.NewReader(body))
	req.SetPathValue("id", parentID.String())
	w := httptest.NewRecorder()

	h.TestTask(w, req, parentID)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)

	testTaskID, _ := uuid.Parse(resp["test_task_id"])
	testTask, err := h.store.GetTask(context.Background(), testTaskID)
	if err != nil {
		t.Fatalf("get test task: %v", err)
	}
	if !strings.Contains(testTask.Prompt, criteria) {
		t.Errorf("test task prompt should contain acceptance criteria, got:\n%s", testTask.Prompt)
	}
}

func TestBuildTestPrompt(t *testing.T) {
	t.Run("without criteria", func(t *testing.T) {
		p := buildTestPrompt("build a widget", "")
		if !strings.Contains(p, "build a widget") {
			t.Error("prompt should contain original task text")
		}
		if strings.Contains(p, "Acceptance Criteria") {
			t.Error("prompt should not contain Acceptance Criteria section when criteria is empty")
		}
	})

	t.Run("with criteria", func(t *testing.T) {
		p := buildTestPrompt("build a widget", "must render in 100ms")
		if !strings.Contains(p, "build a widget") {
			t.Error("prompt should contain original task text")
		}
		if !strings.Contains(p, "must render in 100ms") {
			t.Error("prompt should contain acceptance criteria")
		}
		if !strings.Contains(p, "Acceptance Criteria") {
			t.Error("prompt should contain Acceptance Criteria section header")
		}
	})

	t.Run("whitespace-only criteria treated as empty", func(t *testing.T) {
		p := buildTestPrompt("build a widget", "   \n\t  ")
		if strings.Contains(p, "Acceptance Criteria") {
			t.Error("whitespace-only criteria should not produce Acceptance Criteria section")
		}
	})
}
