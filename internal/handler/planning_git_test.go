package handler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initPlanningTestRepo creates a temp git repo with one initial commit so
// HEAD exists and `git log` returns without error.
func initPlanningTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "planning-test@example.com")
	runGit(t, dir, "config", "user.name", "Planning Test")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// writeSpec creates a spec file under specs/ relative to the workspace.
func writeSpec(t *testing.T, dir, name, body string) {
	t.Helper()
	specsDir := filepath.Join(dir, "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specsDir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitLogSubjects returns commit subjects reachable from HEAD, newest first.
func gitLogSubjects(t *testing.T, dir string) []string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "log", "--format=%s")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func TestCommitPlanningRound_DirtySpecs(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "foo.md", "# Foo\n")

	if err := commitPlanningRound(context.Background(), ws, "drafted foo"); err != nil {
		t.Fatalf("commitPlanningRound: %v", err)
	}

	subjects := gitLogSubjects(t, ws)
	if len(subjects) != 2 {
		t.Fatalf("expected 2 commits, got %d: %v", len(subjects), subjects)
	}
	want := "plan: round 1 — drafted foo"
	if subjects[0] != want {
		t.Errorf("top commit subject = %q, want %q", subjects[0], want)
	}

	// Verify only specs/ landed in the commit.
	cmd := exec.Command("git", "-C", ws, "show", "--name-only", "--format=", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git show: %v", err)
	}
	files := strings.Fields(strings.TrimSpace(string(out)))
	for _, f := range files {
		if !strings.HasPrefix(f, "specs/") {
			t.Errorf("unexpected file in commit: %q", f)
		}
	}
	if len(files) != 1 || files[0] != "specs/foo.md" {
		t.Errorf("commit files = %v, want [specs/foo.md]", files)
	}
}

func TestCommitPlanningRound_NoOp(t *testing.T) {
	ws := initPlanningTestRepo(t)
	before := gitLogSubjects(t, ws)

	if err := commitPlanningRound(context.Background(), ws, "nothing changed"); err != nil {
		t.Fatalf("commitPlanningRound: %v", err)
	}

	after := gitLogSubjects(t, ws)
	if len(after) != len(before) {
		t.Errorf("commit count changed: before=%d after=%d", len(before), len(after))
	}
}

func TestCommitPlanningRound_RoundNumbering(t *testing.T) {
	ws := initPlanningTestRepo(t)

	// Seed two rounds.
	writeSpec(t, ws, "a.md", "a\n")
	if err := commitPlanningRound(context.Background(), ws, "first"); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, ws, "b.md", "b\n")
	if err := commitPlanningRound(context.Background(), ws, "second"); err != nil {
		t.Fatal(err)
	}

	// Third round should be round 3.
	writeSpec(t, ws, "c.md", "c\n")
	if err := commitPlanningRound(context.Background(), ws, "third"); err != nil {
		t.Fatal(err)
	}

	subjects := gitLogSubjects(t, ws)
	if len(subjects) < 1 {
		t.Fatalf("no commits")
	}
	want := "plan: round 3 — third"
	if subjects[0] != want {
		t.Errorf("top subject = %q, want %q\nfull log: %v", subjects[0], want, subjects)
	}
}

func TestCommitPlanningRound_SummaryTruncation(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "foo.md", "foo\n")

	// 120-char summary; should truncate to 80.
	long := strings.Repeat("x", 120)
	if err := commitPlanningRound(context.Background(), ws, long); err != nil {
		t.Fatalf("commitPlanningRound: %v", err)
	}

	subjects := gitLogSubjects(t, ws)
	if len(subjects) == 0 {
		t.Fatal("no commits")
	}
	top := subjects[0]
	prefix := "plan: round 1 — "
	if !strings.HasPrefix(top, prefix) {
		t.Fatalf("unexpected subject: %q", top)
	}
	body := strings.TrimPrefix(top, prefix)
	if len(body) != commitPlanningRoundSummaryMax {
		t.Errorf("summary length = %d, want %d (subject=%q)", len(body), commitPlanningRoundSummaryMax, top)
	}
	if body != strings.Repeat("x", commitPlanningRoundSummaryMax) {
		t.Errorf("summary content mismatch")
	}
}
