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
	task, err := h.store.CreateTask(context.Background(), prompt, 15, false, "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(context.Background(), task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("set waiting: %v", err)
	}
	return task.ID
}

func TestTestTask_RejectsNonWaiting(t *testing.T) {
	h := newTestHandler(t)

	task, err := h.store.CreateTask(context.Background(), "build a widget", 15, false, "", "")
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

func TestTestTask_StartsTestRunOnSameTask(t *testing.T) {
	h := newTestHandler(t)

	parentPrompt := "implement a login form with email and password fields"
	parentID := createWaitingTask(t, h, parentPrompt)

	// Count tasks before — should be exactly 1.
	tasksBefore, err := h.store.ListTasks(context.Background(), false)
	if err != nil {
		t.Fatalf("list tasks before: %v", err)
	}
	if len(tasksBefore) != 1 {
		t.Fatalf("expected 1 task before test, got %d", len(tasksBefore))
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+parentID.String()+"/test", bytes.NewBufferString(`{}`))
	req.SetPathValue("id", parentID.String())
	w := httptest.NewRecorder()

	h.TestTask(w, req, parentID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "testing" {
		t.Errorf("expected status=testing, got %q", resp["status"])
	}

	// No new task should have been created.
	tasksAfter, err := h.store.ListTasks(context.Background(), false)
	if err != nil {
		t.Fatalf("list tasks after: %v", err)
	}
	if len(tasksAfter) != 1 {
		t.Errorf("expected still 1 task after test start, got %d", len(tasksAfter))
	}

	// A state_change event (waiting → in_progress) and system event should exist on the same task.
	events, err := h.store.GetEvents(context.Background(), parentID)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	var foundStateChange, foundSystem bool
	for _, ev := range events {
		if ev.EventType == store.EventTypeStateChange {
			var data map[string]string
			if jsonErr := json.Unmarshal(ev.Data, &data); jsonErr == nil {
				if data["from"] == "waiting" && data["to"] == "in_progress" {
					foundStateChange = true
				}
			}
		}
		if ev.EventType == store.EventTypeSystem {
			var data map[string]string
			if jsonErr := json.Unmarshal(ev.Data, &data); jsonErr == nil {
				if strings.Contains(data["result"], "Test verification started") {
					foundSystem = true
				}
			}
		}
	}
	if !foundStateChange {
		t.Error("expected state_change event from waiting to in_progress on same task")
	}
	if !foundSystem {
		t.Error("expected system event noting test verification started on same task")
	}
}

func TestTestTask_IncludesCriteriaInTestPrompt(t *testing.T) {
	h := newTestHandler(t)

	parentID := createWaitingTask(t, h, "build a widget")

	criteria := "the widget must render in under 100ms"
	body, _ := json.Marshal(map[string]string{"criteria": criteria})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+parentID.String()+"/test", bytes.NewReader(body))
	req.SetPathValue("id", parentID.String())
	w := httptest.NewRecorder()

	h.TestTask(w, req, parentID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The test prompt (including criteria) should be recorded in a system event.
	events, err := h.store.GetEvents(context.Background(), parentID)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	var foundCriteria bool
	for _, ev := range events {
		if ev.EventType == store.EventTypeSystem {
			var data map[string]string
			if jsonErr := json.Unmarshal(ev.Data, &data); jsonErr == nil {
				if strings.Contains(data["test_prompt"], criteria) {
					foundCriteria = true
				}
			}
		}
	}
	if !foundCriteria {
		t.Error("expected system event test_prompt to contain acceptance criteria")
	}
}

func TestBuildTestPrompt(t *testing.T) {
	t.Run("without criteria", func(t *testing.T) {
		p := buildTestPrompt("build a widget", "", "", "")
		if !strings.Contains(p, "build a widget") {
			t.Error("prompt should contain original task text")
		}
		if strings.Contains(p, "Acceptance Criteria") {
			t.Error("prompt should not contain Acceptance Criteria section when criteria is empty")
		}
	})

	t.Run("with criteria", func(t *testing.T) {
		p := buildTestPrompt("build a widget", "must render in 100ms", "", "")
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
		p := buildTestPrompt("build a widget", "   \n\t  ", "", "")
		if strings.Contains(p, "Acceptance Criteria") {
			t.Error("whitespace-only criteria should not produce Acceptance Criteria section")
		}
	})

	t.Run("prompt instructs not to modify code", func(t *testing.T) {
		p := buildTestPrompt("build a widget", "", "", "")
		if !strings.Contains(p, "Do not modify") {
			t.Error("prompt should instruct test agent not to modify code")
		}
	})

	t.Run("with implementation result", func(t *testing.T) {
		p := buildTestPrompt("build a widget", "", "I added widget.go with a Widget struct", "")
		if !strings.Contains(p, "Implementation Summary") {
			t.Error("prompt should contain Implementation Summary section when implResult is set")
		}
		if !strings.Contains(p, "I added widget.go with a Widget struct") {
			t.Error("prompt should contain the implementation result text")
		}
	})

	t.Run("whitespace-only implResult treated as empty", func(t *testing.T) {
		p := buildTestPrompt("build a widget", "", "   \n  ", "")
		if strings.Contains(p, "Implementation Summary") {
			t.Error("whitespace-only implResult should not produce Implementation Summary section")
		}
	})

	t.Run("with diff", func(t *testing.T) {
		fakeDiff := "+func Widget() {}\n-old code"
		p := buildTestPrompt("build a widget", "", "", fakeDiff)
		if !strings.Contains(p, "Changes Made") {
			t.Error("prompt should contain Changes Made section when diff is set")
		}
		if !strings.Contains(p, fakeDiff) {
			t.Error("prompt should contain the diff text")
		}
		if !strings.Contains(p, "focus your verification on those files") {
			t.Error("prompt should tell agent to focus on the diff when diff is present")
		}
	})

	t.Run("without diff uses generic examine instruction", func(t *testing.T) {
		p := buildTestPrompt("build a widget", "", "", "")
		if strings.Contains(p, "Changes Made") {
			t.Error("prompt should not contain Changes Made section when diff is empty")
		}
		if !strings.Contains(p, "Examine the code") {
			t.Error("prompt should fall back to generic examine instruction when no diff")
		}
	})
}
