package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
)

// newTestHandlerWithEnv creates a Handler backed by a temp-dir store and a
// real env file so that UpdateEnvConfig can write to it.
func newTestHandlerWithEnv(t *testing.T) (*Handler, string) {
	t.Helper()
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), ".env")
	// Create an empty env file so envconfig.Update can write to it.
	if err := os.WriteFile(envPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{EnvFile: envPath})
	t.Cleanup(r.WaitBackground)
	h := NewHandler(s, r, t.TempDir(), nil)
	return h, envPath
}

// TestUpdateEnvConfig_TriggersAutoPromote verifies that updating
// max_parallel_tasks immediately triggers auto-promotion when autopilot is
// enabled and there are backlog tasks waiting.
func TestUpdateEnvConfig_TriggersAutoPromote(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	// Enable autopilot so tryAutoPromote will act.
	h.autopilotMu.Lock()
	h.autopilot = true
	h.autopilotMu.Unlock()

	// Create a backlog task.
	ctx := context.Background()
	_, err := h.store.CreateTask(ctx, "backlog task", 15, false, "")
	if err != nil {
		t.Fatal(err)
	}

	// Update max_parallel_tasks to 1 via the HTTP handler.
	body := `{"max_parallel_tasks": 1}`
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Poll briefly for the backlog task to leave backlog status.
	// The promotion happens in a background goroutine; the task moves to
	// in_progress (and may then move to failed if the runner has no command),
	// but either outcome proves tryAutoPromote was triggered.
	promoted := false
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		tasks, err := h.store.ListTasks(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) > 0 && tasks[0].Status != "backlog" {
			promoted = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !promoted {
		tasks, _ := h.store.ListTasks(ctx, false)
		status := "unknown"
		if len(tasks) > 0 {
			status = tasks[0].Status
		}
		t.Errorf("expected backlog task to be promoted (leave backlog status), got %s", status)
	}
}

// TestUpdateEnvConfig_NoAutoPromoteWhenAutopilotOff verifies that no
// auto-promotion happens when autopilot is disabled.
func TestUpdateEnvConfig_NoAutoPromoteWhenAutopilotOff(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	// autopilot is false by default.

	ctx := context.Background()
	_, err := h.store.CreateTask(ctx, "backlog task", 15, false, "")
	if err != nil {
		t.Fatal(err)
	}

	body := `{"max_parallel_tasks": 1}`
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Wait long enough that a goroutine would have run.
	time.Sleep(100 * time.Millisecond)

	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) > 0 && tasks[0].Status == "in_progress" {
		t.Errorf("expected task to remain in backlog when autopilot is off, got in_progress")
	}
}
