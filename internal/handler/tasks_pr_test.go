package handler

import (
	"os/exec"
	"testing"

	"latere.ai/x/wallfacer/internal/store"
)

// gitRepoWithOrigin creates a temp git repo with the given origin remote and a
// committed default branch, returning its path.
func gitRepoWithOrigin(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("remote", "add", "origin", origin)
	run("commit", "--allow-empty", "-m", "init")
	return dir
}

func TestTaskRepoRef_GitHubOrigin(t *testing.T) {
	dir := gitRepoWithOrigin(t, "https://github.com/latere/wallfacer.git")
	task := &store.Task{BranchName: "task/abc123", WorktreePaths: map[string]string{dir: dir}}

	owner, name, base, head, ok := taskRepoRef(task)
	if !ok {
		t.Fatal("expected ok for a github origin + branch")
	}
	if owner != "latere" || name != "wallfacer" {
		t.Errorf("owner/name = %q/%q, want latere/wallfacer", owner, name)
	}
	if head != "task/abc123" {
		t.Errorf("head = %q", head)
	}
	if base == "" {
		t.Error("base should be resolved (default branch or fallback)")
	}
}

// scp-style git@github.com:owner/repo.git origins must resolve too.
func TestTaskRepoRef_SCPOrigin(t *testing.T) {
	dir := gitRepoWithOrigin(t, "git@github.com:latere/wallfacer.git")
	task := &store.Task{BranchName: "task/x", WorktreePaths: map[string]string{dir: dir}}
	owner, name, _, _, ok := taskRepoRef(task)
	if !ok || owner != "latere" || name != "wallfacer" {
		t.Errorf("scp origin ref = %q/%q ok=%v", owner, name, ok)
	}
}

func TestTaskRepoRef_NoBranch(t *testing.T) {
	dir := gitRepoWithOrigin(t, "https://github.com/o/r.git")
	task := &store.Task{BranchName: "", WorktreePaths: map[string]string{dir: dir}}
	if _, _, _, _, ok := taskRepoRef(task); ok {
		t.Error("expected not ok when the task has no branch")
	}
}

// A non-github origin is not usable by the GitHub write API.
func TestTaskRepoRef_NonGitHubOrigin(t *testing.T) {
	dir := gitRepoWithOrigin(t, "https://gitlab.com/o/r.git")
	task := &store.Task{BranchName: "task/x", WorktreePaths: map[string]string{dir: dir}}
	if _, _, _, _, ok := taskRepoRef(task); ok {
		t.Error("expected not ok for a non-github origin")
	}
}

func TestTaskRepoRef_NoWorktrees(t *testing.T) {
	task := &store.Task{BranchName: "task/x", WorktreePaths: map[string]string{}}
	if _, _, _, _, ok := taskRepoRef(task); ok {
		t.Error("expected not ok with no worktrees")
	}
}
