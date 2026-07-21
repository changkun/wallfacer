package handler

import (
	"os/exec"
	"strings"
	"testing"
	"unicode/utf8"

	"latere.ai/x/wallfacer/internal/store"
)

// TestPRTitleForTask_MultibyteTruncation guards against slicing a multi-byte
// (CJK) prompt at a raw byte boundary, which would cut a rune in half and yield
// an invalid-UTF-8 PR title. The leading "a" shifts the 72-byte boundary into
// the middle of a 3-byte rune; without rune-aware truncation the result is not
// valid UTF-8.
func TestPRTitleForTask_MultibyteTruncation(t *testing.T) {
	task := &store.Task{Prompt: "a" + strings.Repeat("世", 100)}
	got := prTitleForTask(task)
	if !utf8.ValidString(got) {
		t.Fatalf("PR title is not valid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 72 {
		t.Errorf("title rune count = %d, want 72", n)
	}
}

// TestPRTitleForTask_ShortPromptUnchanged confirms sub-limit prompts pass
// through untouched (first line only).
func TestPRTitleForTask_ShortPromptUnchanged(t *testing.T) {
	task := &store.Task{Prompt: "  fix the parser\nmore detail  "}
	if got := prTitleForTask(task); got != "fix the parser" {
		t.Errorf("title = %q, want %q", got, "fix the parser")
	}
}

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

// TestTaskRepoRef_MultiRepoDeterministic verifies that a task spanning two
// github.com repos always resolves to the same repo (the path-sorted first),
// rather than a nondeterministic one from map iteration order.
func TestTaskRepoRef_MultiRepoDeterministic(t *testing.T) {
	dirA := gitRepoWithOrigin(t, "https://github.com/org/repo-a.git")
	dirB := gitRepoWithOrigin(t, "https://github.com/org/repo-b.git")
	task := &store.Task{
		BranchName:    "task/x",
		WorktreePaths: map[string]string{dirA: dirA, dirB: dirB},
	}
	expectedName := "repo-a"
	if dirB < dirA {
		expectedName = "repo-b" // resolution follows sorted worktree path
	}
	for i := range 50 {
		_, name, _, _, ok := taskRepoRef(task)
		if !ok {
			t.Fatalf("iteration %d: expected ok", i)
		}
		if name != expectedName {
			t.Fatalf("iteration %d: name = %q, want %q (nondeterministic repo selection)", i, name, expectedName)
		}
	}
}

// TestBuildRevertSubject_MultibyteTruncation guards against slicing a multi-byte
// summary at a raw byte boundary, which would cut a rune in half.
func TestBuildRevertSubject_MultibyteTruncation(t *testing.T) {
	// The leading "a" shifts the 80-byte boundary into the middle of a 3-byte
	// rune; a raw byte slice would yield invalid UTF-8.
	got := buildRevertSubject("a"+strings.Repeat("世", 100), 3)
	if !utf8.ValidString(got) {
		t.Fatalf("revert subject is not valid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n > 80 {
		t.Errorf("subject rune count = %d, want <= 80", n)
	}
}
