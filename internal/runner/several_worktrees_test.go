package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/store"
)

// severalWorktreesEvents returns the data of every several-worktrees system
// event on the task's timeline.
func severalWorktreesEvents(t *testing.T, s *store.Store, taskID uuid.UUID) []map[string]string {
	t.Helper()
	events, err := s.GetEvents(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	var out []map[string]string
	for _, ev := range events {
		if ev.EventType != store.EventTypeSystem {
			continue
		}
		var data map[string]string
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue // system events with non-string payloads are not ours
		}
		if data["kind"] == severalWorktreesEventKind {
			out = append(out, data)
		}
	}
	return out
}

// TestRun_SeveralWorktreesRunsWithOneNotice covers a task stored with
// worktrees in two folders, from before a workspace became one folder, run
// after its workspace was re-pointed to one of them. The run completes as
// before and keeps both worktrees, and the timeline carries one notice naming
// the folder the agent works in. A resumed run adds no second notice.
func TestRun_SeveralWorktreesRunsWithOneNotice(t *testing.T) {
	repoA, repoB := setupTestRepo(t), setupTestRepo(t)
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repoB}, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "work across two repos", Timeout: 5})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	paths, branch, err := r.EnsureTaskWorktrees(task.ID, map[string]string{repoA: "", repoB: ""}, "")
	if err != nil {
		t.Fatalf("EnsureTaskWorktrees: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("worktrees = %v, want one per folder", paths)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, paths, branch); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	r.Run(task.ID, task.Prompt, "", false)

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.Status != store.TaskStatusWaiting {
		t.Fatalf("status = %q, want waiting", updated.Status)
	}
	if len(updated.WorktreePaths) != 2 {
		t.Fatalf("worktree_paths = %v, want both folders kept", updated.WorktreePaths)
	}

	notices := severalWorktreesEvents(t, s, task.ID)
	if len(notices) != 1 {
		t.Fatalf("several-worktrees events = %v, want exactly one", notices)
	}
	if notices[0]["agent_folder"] != repoB {
		t.Errorf("notice agent_folder = %q, want the workspace folder %q", notices[0]["agent_folder"], repoB)
	}
	if res := notices[0]["result"]; !strings.Contains(res, repoA) || !strings.Contains(res, repoB) {
		t.Errorf("notice result %q does not name both folders", res)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus (resume): %v", err)
	}
	r.Run(task.ID, "continue", "sess1", true)
	if got := severalWorktreesEvents(t, s, task.ID); len(got) != 1 {
		t.Fatalf("several-worktrees events after a resumed run = %v, want still exactly one", got)
	}
}

// TestRun_OneFolderTaskGetsOneWorktreeAndNoNotice asserts a new task in a
// one-folder workspace gets one worktree and no several-worktrees notice.
func TestRun_OneFolderTaskGetsOneWorktreeAndNoNotice(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "ordinary work", Timeout: 5})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	r.Run(task.ID, task.Prompt, "", false)

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(updated.WorktreePaths) != 1 || updated.WorktreePaths[repo] == "" {
		t.Fatalf("worktree_paths = %v, want one worktree for %s", updated.WorktreePaths, repo)
	}
	if got := severalWorktreesEvents(t, s, task.ID); len(got) != 0 {
		t.Errorf("several-worktrees events = %v, want none", got)
	}
}
