package runner

import (
	"context"
	"encoding/json"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
)

// TestRunEmitsSystemEventForNonEmptyStderr verifies that when a task turn
// produces non-empty stderr output, a system event with a "stderr_file" key
// pointing to the persisted stderr file is inserted into the task's event trail.
func TestRunEmitsSystemEventForNonEmptyStderr(t *testing.T) {
	repo := setupTestRepo(t)
	stderrMsg := "rate limit warning: 90% of quota used\nAuthentication notice"
	cmd := fakeCmdScriptWithStderr(t, endTurnOutput, stderrMsg, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "stderr event test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, ev := range events {
		if ev.EventType != store.EventTypeSystem {
			continue
		}
		var data map[string]string
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		if data["stderr_file"] == "turn-0001.stderr.txt" {
			found = true
			if data["turn"] != "1" {
				t.Errorf("expected turn=1, got %q", data["turn"])
			}
			break
		}
	}
	if !found {
		t.Fatal("expected a system event with stderr_file='turn-0001.stderr.txt'")
	}
}

// TestRunNoSystemEventWhenStderrEmpty verifies that no stderr system event is
// emitted when the container produces no stderr output.
func TestRunNoSystemEventWhenStderrEmpty(t *testing.T) {
	repo := setupTestRepo(t)
	// Use fakeCmdScript (no stderr) to ensure clean baseline.
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "no stderr event test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, ev := range events {
		if ev.EventType != store.EventTypeSystem {
			continue
		}
		var data map[string]string
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		if _, ok := data["stderr_file"]; ok {
			t.Errorf("unexpected stderr system event when stderr is empty: %v", data)
		}
	}
}
