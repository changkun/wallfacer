package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// seedPlanningCommit stages all changes under specs/ and creates a
// kanban-style planning commit carrying the Plan-Round trailer. Used by the
// undo tests to hand-craft a history that matches what commitPlanningRound
// produces without going through the whole planning pipeline.
func seedPlanningCommit(t *testing.T, ws string, n int, summary string) {
	t.Helper()
	runGit(t, ws, "add", "specs/")
	msg := fmt.Sprintf("specs(plan): %s\n\nseeded by test\n\nPlan-Round: %d", summary, n)
	runGit(t, ws, "commit", "-m", msg)
}

func TestUndoPlanningRound_Success(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "foo.md", "# foo\n")
	seedPlanningCommit(t, ws, 1, "drafted foo")

	h := newStaticWorkspaceHandler(t, []string{ws})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/undo", nil)
	h.UndoPlanningRound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nbody: %s", rec.Code, rec.Body.String())
	}

	var resp undoResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp.Round != 1 {
		t.Errorf("round = %d, want 1", resp.Round)
	}
	if resp.Summary != "drafted foo" {
		t.Errorf("summary = %q, want %q", resp.Summary, "drafted foo")
	}
	if resp.Workspace != ws {
		t.Errorf("workspace = %q, want %q", resp.Workspace, ws)
	}
	if len(resp.FilesReverted) != 1 || resp.FilesReverted[0] != "specs/foo.md" {
		t.Errorf("files_reverted = %v, want [specs/foo.md]", resp.FilesReverted)
	}

	// With the git-revert design, the original planning commit stays in
	// history and a forward revert commit sits on top of it. Verify both
	// are present and that the spec file is gone from the working tree.
	subjects := gitLogSubjects(t, ws)
	var sawOriginal, sawRevert bool
	for _, s := range subjects {
		if strings.Contains(s, "(plan): drafted foo") {
			sawOriginal = true
		}
		if strings.Contains(s, "revert round 1") {
			sawRevert = true
		}
	}
	if !sawOriginal {
		t.Errorf("original planning commit missing from history; subjects=%v", subjects)
	}
	if !sawRevert {
		t.Errorf("revert commit not found in history; subjects=%v", subjects)
	}
	if _, err := os.Stat(filepath.Join(ws, "specs", "foo.md")); !os.IsNotExist(err) {
		t.Errorf("specs/foo.md still exists after undo: err=%v", err)
	}
}

func TestUndoPlanningRound_NoPlanningCommits(t *testing.T) {
	ws := initPlanningTestRepo(t)

	h := newStaticWorkspaceHandler(t, []string{ws})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/undo", nil)
	h.UndoPlanningRound(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no planning commits to undo") {
		t.Errorf("body = %q, want error about no planning commits to undo", rec.Body.String())
	}
}

func TestUndoPlanningRound_WithDirtyWorkingTree(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "foo.md", "# foo initial\n")
	seedPlanningCommit(t, ws, 1, "drafted foo")

	// User hand-edits a different spec after the planning commit — NOT committed.
	writeSpec(t, ws, "bar.md", "# bar user edit\n")

	h := newStaticWorkspaceHandler(t, []string{ws})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/undo", nil)
	h.UndoPlanningRound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nbody: %s", rec.Code, rec.Body.String())
	}

	// foo.md (from the reverted commit) should be gone.
	if _, err := os.Stat(filepath.Join(ws, "specs", "foo.md")); !os.IsNotExist(err) {
		t.Errorf("specs/foo.md still exists: err=%v", err)
	}

	// bar.md (user's dirty edit) should survive the stash/pop.
	got, err := os.ReadFile(filepath.Join(ws, "specs", "bar.md"))
	if err != nil {
		t.Fatalf("read bar.md after undo: %v", err)
	}
	if string(got) != "# bar user edit\n" {
		t.Errorf("bar.md content = %q, want preserved user edit", string(got))
	}
}

func TestUndoPlanningRound_DispatchAware(t *testing.T) {
	ws := initPlanningTestRepo(t)

	h := newStaticWorkspaceHandler(t, []string{ws})

	// Seed a task in the store that will be "dispatched" by the planning commit.
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt:  "dispatched-by-planning",
		Timeout: 60,
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}

	// Baseline commit with empty frontmatter (no dispatched_task_id).
	writeSpec(t, ws, "foo.md", "---\ntitle: foo\n---\n")
	runGit(t, ws, "add", "specs/")
	runGit(t, ws, "commit", "-m", "seed foo spec")

	// Planning round adds dispatched_task_id — this is the line the undo
	// must detect and act on.
	writeSpec(t, ws, "foo.md", "---\ntitle: foo\ndispatched_task_id: "+task.ID.String()+"\n---\n")
	seedPlanningCommit(t, ws, 1, "dispatch foo")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/undo", nil)
	h.UndoPlanningRound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nbody: %s", rec.Code, rec.Body.String())
	}

	// The task should now be cancelled.
	got, err := h.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after undo: %v", err)
	}
	if got.Status != store.TaskStatusCancelled {
		t.Errorf("task status = %q, want %q", got.Status, store.TaskStatusCancelled)
	}
}

func TestExtractDispatchedTaskIDs(t *testing.T) {
	a := uuid.New().String()
	b := uuid.New().String()
	diff := "" +
		"diff --git a/specs/x.md b/specs/x.md\n" +
		"+++ b/specs/x.md\n" +
		"@@\n" +
		"-dispatched_task_id: 00000000-0000-0000-0000-000000000000\n" +
		"+dispatched_task_id: " + a + "\n" +
		"+title: foo\n" +
		"+dispatched_task_id: " + b + "\n" +
		"+dispatched_task_id: not-a-uuid\n" +
		"+dispatched_task_id: " + a + "\n" // duplicate

	got := extractDispatchedTaskIDs(diff)
	if len(got) != 2 {
		t.Fatalf("extracted %d ids, want 2: %v", len(got), got)
	}
	if got[0].String() != a {
		t.Errorf("ids[0] = %s, want %s", got[0], a)
	}
	if got[1].String() != b {
		t.Errorf("ids[1] = %s, want %s", got[1], b)
	}

	// Empty diff → no ids.
	if len(extractDispatchedTaskIDs("")) != 0 {
		t.Errorf("empty diff should yield 0 ids")
	}

	// Diff-header lines like "+++ b/specs/..." must not match.
	headerOnly := "+++ b/specs/foo.md\n--- a/specs/foo.md\n"
	if len(extractDispatchedTaskIDs(headerOnly)) != 0 {
		t.Errorf("diff-header-only should yield 0 ids")
	}
}

func TestParsePlanMessage(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		body    string
		round   int
		summary string
	}{
		{
			name:    "canonical",
			subject: "specs/local/auth(plan): add OAuth breakdown",
			body:    "body text\n\nPlan-Round: 3\n",
			round:   3,
			summary: "add OAuth breakdown",
		},
		{
			name:    "deep path",
			subject: "specs/local/spec-coordination/foo(plan): draft proposal",
			body:    "Plan-Round: 42",
			round:   42,
			summary: "draft proposal",
		},
		{
			name:    "empty summary",
			subject: "specs(plan):",
			body:    "Plan-Round: 7",
			round:   7,
			summary: "",
		},
		{
			name:    "missing trailer",
			subject: "specs(plan): did a thing",
			body:    "no trailer here",
			round:   0,
			summary: "did a thing",
		},
		{
			name:    "non-planning subject",
			subject: "internal/runner: unrelated",
			body:    "Plan-Round: 1",
			round:   1,
			summary: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, s := parsePlanMessage(c.subject, c.body)
			if n != c.round || s != c.summary {
				t.Errorf("parsePlanMessage(%q, %q) = (%d, %q), want (%d, %q)",
					c.subject, c.body, n, s, c.round, c.summary)
			}
		})
	}
}
