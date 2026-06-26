package handler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitHeadFiles returns the file paths changed by the commit at HEAD.
func gitHeadFiles(t *testing.T, dir string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("git diff-tree: %v", err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// gitStaged returns the paths currently staged in the index.
func gitStaged(t *testing.T, dir string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "diff", "--cached", "--name-only").Output()
	if err != nil {
		t.Fatalf("git diff --cached: %v", err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// writeExplorerFile writes content to relPath under ws, creating parent dirs.
func writeExplorerFile(t *testing.T, ws, relPath, content string) {
	t.Helper()
	full := filepath.Join(ws, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommitExplorerEdit_NewFile(t *testing.T) {
	ws := initGitTestRepo(t)
	writeExplorerFile(t, ws, "specs/foo.md", "# Foo\n")

	if err := commitExplorerEdit(context.Background(), ws, "specs/foo.md"); err != nil {
		t.Fatalf("commitExplorerEdit: %v", err)
	}

	subjects := gitLogSubjects(t, ws)
	if len(subjects) == 0 || subjects[0] != "specs/foo.md(edit): update foo.md" {
		t.Fatalf("unexpected HEAD subject %q", subjects)
	}
	msg := gitHeadMessage(t, ws)
	if !strings.Contains(msg, "\nEdit-Source: explorer") {
		t.Fatalf("missing Edit-Source trailer in:\n%s", msg)
	}
	if files := gitHeadFiles(t, ws); len(files) != 1 || files[0] != "specs/foo.md" {
		t.Fatalf("expected only specs/foo.md committed, got %v", files)
	}
}

// The Edit-Source trailer must be grep-queryable, the durable contract that
// makes an explorer edit revertible by the same mechanism the planning path
// uses (git log --grep + git revert).
func TestCommitExplorerEdit_TrailerGrepQueryable(t *testing.T) {
	ws := initGitTestRepo(t)
	writeExplorerFile(t, ws, "specs/foo.md", "# Foo\n")
	if err := commitExplorerEdit(context.Background(), ws, "specs/foo.md"); err != nil {
		t.Fatalf("commitExplorerEdit: %v", err)
	}

	out, err := exec.Command("git", "-C", ws, "log", "--format=%H", "--grep=^Edit-Source: ").Output()
	if err != nil {
		t.Fatalf("git log --grep: %v", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("Edit-Source commit not found by git log --grep")
	}
}

// The whole reason for the partial-commit pathspec is isolation: an explorer
// save must commit only the saved file, never sweeping in unrelated staged or
// working-tree changes from a concurrent task or planning round.
func TestCommitExplorerEdit_IsolatesUnrelatedChanges(t *testing.T) {
	ws := initGitTestRepo(t)

	// An unrelated change staged in the index, plus an unrelated dirty file.
	writeExplorerFile(t, ws, "specs/staged.md", "staged\n")
	runGit(t, ws, "add", "specs/staged.md")
	writeExplorerFile(t, ws, "specs/dirty.md", "dirty\n")

	// The explorer save.
	writeExplorerFile(t, ws, "specs/saved.md", "saved\n")
	if err := commitExplorerEdit(context.Background(), ws, "specs/saved.md"); err != nil {
		t.Fatalf("commitExplorerEdit: %v", err)
	}

	if files := gitHeadFiles(t, ws); len(files) != 1 || files[0] != "specs/saved.md" {
		t.Fatalf("commit must contain only specs/saved.md, got %v", files)
	}
	// The unrelated staged file remains staged, uncommitted.
	if staged := gitStaged(t, ws); len(staged) != 1 || staged[0] != "specs/staged.md" {
		t.Fatalf("expected specs/staged.md still staged, got %v", staged)
	}
	// The unrelated dirty file remains uncommitted in the working tree.
	out, _ := exec.Command("git", "-C", ws, "status", "--porcelain", "--", "specs/dirty.md").Output()
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("expected specs/dirty.md to remain uncommitted")
	}
}

func TestCommitExplorerEdit_NonGitWorkspace(t *testing.T) {
	ws := t.TempDir()
	writeExplorerFile(t, ws, "specs/foo.md", "# Foo\n")

	if err := commitExplorerEdit(context.Background(), ws, "specs/foo.md"); err != nil {
		t.Fatalf("non-git workspace must not error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".git")); !os.IsNotExist(err) {
		t.Fatal("non-git workspace must not gain a .git directory")
	}
}

func TestCommitExplorerEdit_IdenticalContentNoOp(t *testing.T) {
	ws := initGitTestRepo(t)
	writeExplorerFile(t, ws, "specs/foo.md", "# Foo\n")
	if err := commitExplorerEdit(context.Background(), ws, "specs/foo.md"); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	before := len(gitLogSubjects(t, ws))

	// Re-save identical content: no change, so no new commit.
	if err := commitExplorerEdit(context.Background(), ws, "specs/foo.md"); err != nil {
		t.Fatalf("no-op commit: %v", err)
	}
	if after := len(gitLogSubjects(t, ws)); after != before {
		t.Fatalf("identical re-save produced a commit: before=%d after=%d", before, after)
	}
}

func TestBuildExplorerCommitMessage(t *testing.T) {
	got := buildExplorerCommitMessage("specs/intent/intent-commits.md")
	want := "specs/intent/intent-commits.md(edit): update intent-commits.md\n\nEdit-Source: explorer"
	if got != want {
		t.Fatalf("buildExplorerCommitMessage:\n got %q\nwant %q", got, want)
	}
}
