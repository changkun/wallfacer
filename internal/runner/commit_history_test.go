package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/store"
)

func TestCommitPipelinePersistsCommitHistoryBeforeCleanup(t *testing.T) {
	repo := setupTestRepo(t)
	s, r := setupTestRunner(t, []string{repo})
	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Two feedback changes", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	paths, branch, err := r.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, paths, branch); err != nil {
		t.Fatal(err)
	}
	wt := paths[repo]
	for turn, name := range []string{"first", "second"} {
		if err := os.WriteFile(filepath.Join(wt, name+".txt"), []byte(name+" change\n"), 0644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, wt, "add", ".")
		gitRun(t, wt, "commit", "-m", name+" feedback change")
		if err := r.captureTaskCommits(ctx, task.ID, turn+1, paths); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCommitting); err != nil {
		t.Fatal(err)
	}
	if err := r.commit(ctx, task.ID, "", 2, paths, branch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("commit did not clean up worktree")
	}
	raw, err := os.ReadFile(filepath.Join(s.DataDir(), task.ID.String(), "commit-history.json"))
	if err != nil {
		t.Fatalf("task lost its commit history after cleanup: %v", err)
	}
	var history []store.TaskCommit
	if err := json.Unmarshal(raw, &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || !strings.Contains(history[0].Patch, "+first change") || !strings.Contains(history[1].Patch, "+second change") {
		t.Fatal("commit history did not retain both patches")
	}
	if history[0].Turn != 1 || history[1].Turn != 2 {
		t.Fatal("feedback turn attribution lost")
	}
	reopened, err := store.NewFileStore(s.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := TaskCommits(ctx, reopened, task.ID)
	if err != nil || len(persisted) != 2 {
		t.Fatalf("history unreadable after restart: %v", err)
	}

}

func TestTaskCommitsAmendRetryAndMultipleRepositories(t *testing.T) {
	a, b := setupTestRepo(t), setupTestRepo(t)
	s, r := setupTestRunner(t, []string{a, b})
	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "history"})
	if err != nil {
		t.Fatal(err)
	}
	paths, branch, err := r.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, paths, branch); err != nil {
		t.Fatal(err)
	}
	if commits, err := TaskCommits(ctx, s, task.ID); err != nil || len(commits) != 0 {
		t.Fatal("unchanged worktree included base commits")
	}
	writeCommit := func(repo, content string, amend bool) {
		t.Helper()
		wt := paths[repo]
		if err := os.WriteFile(filepath.Join(wt, "feature.txt"), []byte(content+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, wt, "add", ".")
		args := []string{"commit", "-m", content}
		if amend {
			args = append(args, "--amend")
		}
		gitRun(t, wt, args...)
	}
	writeCommit(a, "original", false)
	if err := r.captureTaskCommits(ctx, task.ID, 1, paths); err != nil {
		t.Fatal(err)
	}
	writeCommit(a, "amended", true)
	writeCommit(b, "other repository", false)
	live, err := TaskCommits(ctx, s, task.ID)
	if err != nil || len(live) != 3 {
		t.Fatalf("live discovery omitted amended or other-repo commit: %v", err)
	}
	saved, _ := s.GetTaskCommits(task.ID)
	if len(saved) != 1 {
		t.Fatal("GET discovery mutated saved history")
	}
	if err := r.captureTaskCommits(ctx, task.ID, 2, paths); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetTaskForRetry(ctx, task.ID, "retry", true); err != nil {
		t.Fatal(err)
	}
	writeCommit(a, "retry change", false)
	if err := r.captureTaskCommits(ctx, task.ID, 1, paths); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTaskCommits(task.ID)
	if err != nil || len(got) != 4 {
		t.Fatalf("history after retry: %v", err)
	}
	if got[0].Turn != 1 || got[0].Attempt != 1 || !strings.Contains(got[0].Patch, "+original") {
		t.Fatal("amend erased original observation")
	}
	last := got[len(got)-1]
	if last.Attempt != 2 || last.Turn != 1 || !strings.Contains(last.Patch, "+retry change") {
		t.Fatal("retry attribution incorrect")
	}
}

func TestCommitHistorySnapshotAndEmptyRepositoryE2E(t *testing.T) {
	for _, emptyGit := range []bool{false, true} {
		name := "plain-directory"
		if emptyGit {
			name = "empty-git-repository"
		}
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			if emptyGit {
				gitRun(t, repo, "init", "-b", "main")
			}
			s, r := setupTestRunner(t, []string{repo})
			ctx := context.Background()
			task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "first implementation"})
			if err != nil {
				t.Fatal(err)
			}
			paths, branch, err := r.setupWorktrees(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.UpdateTaskWorktrees(ctx, task.ID, paths, branch); err != nil {
				t.Fatal(err)
			}
			wt := paths[repo]
			if err := os.WriteFile(filepath.Join(wt, "first.txt"), []byte("first implementation\n"), 0644); err != nil {
				t.Fatal(err)
			}
			gitRun(t, wt, "add", ".")
			gitRun(t, wt, "commit", "-m", "first implementation")
			if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCommitting); err != nil {
				t.Fatal(err)
			}
			if err := r.commit(ctx, task.ID, "", 1, paths, branch); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(repo, "first.txt"))
			if err != nil || string(content) != "first implementation\n" {
				t.Fatalf("work did not reach original workspace: %v", err)
			}
			commits, err := TaskCommits(ctx, s, task.ID)
			if err != nil || len(commits) != 1 || !strings.Contains(commits[0].Patch, "+first implementation") {
				t.Fatalf("snapshot history lost or included synthetic root: %v", err)
			}
			_, gitErr := os.Stat(filepath.Join(repo, ".git"))
			if emptyGit && gitErr != nil {
				t.Fatal("original repository metadata removed")
			}
			if !emptyGit && !os.IsNotExist(gitErr) {
				t.Fatal("synthetic repository leaked into workspace")
			}
		})
	}
}

func TestCommitHistoryFailureKeepsWorktree(t *testing.T) {
	repo := setupTestRepo(t)
	s, r := setupTestRunner(t, []string{repo})
	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "history"})
	if err != nil {
		t.Fatal(err)
	}
	paths, branch, err := r.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, paths, branch); err != nil {
		t.Fatal(err)
	}
	wt := paths[repo]
	gitRun(t, wt, "commit", "--allow-empty", "-m", "preserve this")
	if err := os.Mkdir(filepath.Join(s.DataDir(), task.ID.String(), "commit-history.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCommitting); err != nil {
		t.Fatal(err)
	}
	if err := r.commit(ctx, task.ID, "", 1, paths, branch); err == nil {
		t.Fatal("history persistence failure ignored")
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatal("worktree deleted despite history failure")
	}
	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusFailed || updated.Result == nil || !strings.Contains(*updated.Result, "worktrees were kept") {
		t.Fatal("history failure not visible")
	}
}

func TestCommitPatchLimitIsExplicit(t *testing.T) {
	w := &boundedPatch{}
	data := []byte(strings.Repeat("x", maxCommitPatchBytes+20))
	if n, err := w.Write(data); err != nil || n != len(data) {
		t.Fatal("git output not fully drained")
	}
	if len(w.data) != maxCommitPatchBytes || !w.truncated {
		t.Fatal("large patch not bounded and marked")
	}
}
