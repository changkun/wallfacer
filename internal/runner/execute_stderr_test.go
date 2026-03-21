package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
)

// fakeCmdScriptWithStderr creates a temporary executable shell script that
// writes stdout to stdout and stderr to stderr, then exits with exitCode.
// Container lifecycle calls ("rm", "kill") are silently skipped so they do
// not emit spurious output or stderr.
func fakeCmdScriptWithStderr(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()

	stdoutPath := filepath.Join(dir, "stdout.txt")
	if err := os.WriteFile(stdoutPath, []byte(stdout), 0644); err != nil {
		t.Fatal(err)
	}
	stderrPath := filepath.Join(dir, "stderr.txt")
	if err := os.WriteFile(stderrPath, []byte(stderr), 0644); err != nil {
		t.Fatal(err)
	}

	scriptPath := filepath.Join(dir, "fake-cmd-stderr")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  rm|kill) exit 0 ;;
esac
cat %s
cat %s >&2
exit %d
`, stdoutPath, stderrPath, exitCode)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return scriptPath
}

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
