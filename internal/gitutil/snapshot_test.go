package gitutil

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitLocalRepo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "hello\n")

	if err := InitLocalRepo(dir, "wf@local", "Wallfacer", "wf: init"); err != nil {
		t.Fatalf("InitLocalRepo: %v", err)
	}

	if got := gitRun(t, dir, "config", "user.email"); got != "wf@local" {
		t.Errorf("user.email = %q, want wf@local", got)
	}
	if got := gitRun(t, dir, "config", "user.name"); got != "Wallfacer" {
		t.Errorf("user.name = %q, want Wallfacer", got)
	}
	if got := gitRun(t, dir, "log", "-1", "--format=%s"); got != "wf: init" {
		t.Errorf("commit subject = %q", got)
	}
	// The staged file should be part of the initial commit.
	if got := gitRun(t, dir, "ls-files"); !strings.Contains(got, "a.txt") {
		t.Errorf("ls-files = %q, want to contain a.txt", got)
	}
}

func TestInitLocalRepoEmptyDir(t *testing.T) {
	// --allow-empty lets the initial commit succeed with no files staged.
	dir := t.TempDir()
	if err := InitLocalRepo(dir, "a@b", "A", "empty"); err != nil {
		t.Fatalf("InitLocalRepo empty: %v", err)
	}
	if got := gitRun(t, dir, "log", "-1", "--format=%s"); got != "empty" {
		t.Errorf("commit subject = %q", got)
	}
}

func TestSnapshotDiff_ModifiedAndUntracked(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "tracked.txt"), "original\n")
	if err := InitLocalRepo(dir, "a@b", "A", "init"); err != nil {
		t.Fatalf("InitLocalRepo: %v", err)
	}
	// Second commit on top of initial so HEAD~1 exists and is the snapshot commit.
	writeFile(t, filepath.Join(dir, "tracked.txt"), "modified\n")
	writeFile(t, filepath.Join(dir, "new.txt"), "brand new\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "changes")
	// Plus a later untracked file.
	writeFile(t, filepath.Join(dir, "untracked.txt"), "still untracked\n")

	diff := SnapshotDiff(context.Background(), dir)
	if !strings.Contains(diff, "modified") {
		t.Errorf("diff missing modified content: %s", diff)
	}
	if !strings.Contains(diff, "brand new") {
		t.Errorf("diff missing new tracked file content: %s", diff)
	}
	if !strings.Contains(diff, "still untracked") {
		t.Errorf("diff missing untracked content: %s", diff)
	}
}

func TestSnapshotDiff_SingleCommit_UncommittedOnly(t *testing.T) {
	// When only the initial snapshot commit exists (no HEAD~1), SnapshotDiff
	// should return only uncommitted changes — the initial snapshot itself is
	// the baseline, not a change to report.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "f.txt"), "baseline\n")
	if err := InitLocalRepo(dir, "a@b", "A", "sole"); err != nil {
		t.Fatalf("InitLocalRepo: %v", err)
	}

	// Nothing changed after the initial commit — diff should be empty.
	if diff := SnapshotDiff(context.Background(), dir); diff != "" {
		t.Errorf("expected empty diff when only initial snapshot exists, got: %s", diff)
	}

	// Modify the file without committing — the uncommitted change must appear.
	writeFile(t, filepath.Join(dir, "f.txt"), "updated\n")
	diff := SnapshotDiff(context.Background(), dir)
	if !strings.Contains(diff, "updated") {
		t.Errorf("diff missing uncommitted change: %s", diff)
	}
}

func TestHasChanges(t *testing.T) {
	dir := setupRepo(t)
	ctx := context.Background()

	dirty, err := HasChanges(ctx, dir)
	if err != nil {
		t.Fatalf("HasChanges clean: %v", err)
	}
	if dirty {
		t.Errorf("clean repo reported as dirty")
	}

	writeFile(t, filepath.Join(dir, "file.txt"), "changed\n")
	dirty, err = HasChanges(ctx, dir)
	if err != nil {
		t.Fatalf("HasChanges after edit: %v", err)
	}
	if !dirty {
		t.Errorf("modified repo reported as clean")
	}
}

func TestHasChanges_UntrackedFile(t *testing.T) {
	dir := setupRepo(t)
	writeFile(t, filepath.Join(dir, "new.txt"), "untracked\n")
	dirty, err := HasChanges(context.Background(), dir)
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if !dirty {
		t.Errorf("untracked file not detected as change")
	}
}

func TestGlobalIdentityOverrides(t *testing.T) {
	// Redirect global git config to a temp HOME so the test doesn't depend on
	// (or mutate) the developer's real ~/.gitconfig.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	// Git on some platforms reads GIT_CONFIG_GLOBAL first; set it explicitly.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"),
		[]byte("[user]\n\tname = Alice\n\temail = alice@example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := GlobalIdentityOverrides(context.Background())
	want := []string{"-c", "user.name=Alice", "-c", "user.email=alice@example.com"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("GlobalIdentityOverrides = %v, want %v", got, want)
	}
}

func TestGlobalIdentityOverrides_Missing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))

	// No gitconfig written — both reads should fail and produce no overrides.
	got := GlobalIdentityOverrides(context.Background())
	if len(got) != 0 {
		t.Errorf("expected no overrides when global identity unset, got %v", got)
	}
}
