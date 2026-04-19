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

	"changkun.de/x/wallfacer/internal/planner"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// seedPlanningCommit stages all changes under specs/ and creates a
// scope-prefixed planning commit carrying the Plan-Round trailer. Used by the
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

// --- Task-mode undo tests ---

// seedPromptRound simulates what UpdateTaskPromptTool does: updates the task
// prompt and writes a prompt_round event. Returns the previous prompt.
func seedPromptRound(t *testing.T, h *Handler, taskID uuid.UUID, threadID string, round int, newPrompt string) string {
	t.Helper()
	ctx := context.Background()
	prev, _, err := h.store.UpdateTaskPromptDirect(ctx, taskID, newPrompt)
	if err != nil {
		t.Fatalf("UpdateTaskPromptDirect: %v", err)
	}
	payload := store.NewPromptRoundEvent(threadID, round, prev, newPrompt, false)
	_ = h.store.InsertEvent(ctx, taskID, store.EventTypePromptRound, payload)
	return prev
}

func TestUndo_TaskMode_RewindsLastRound(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	_ = p.Start(context.Background())
	h.planner = p

	ctx := context.Background()
	tsk, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "original", Timeout: 60})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}

	threadID := p.Threads().ActiveID()
	cs, _ := p.Threads().Store(threadID)
	_ = cs.SaveSession(planner.SessionInfo{FocusedTask: tsk.ID.String()})

	// Simulate two prompt rounds.
	seedPromptRound(t, h, tsk.ID, threadID, 1, "v1")
	seedPromptRound(t, h, tsk.ID, threadID, 2, "v2")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/undo?thread="+threadID, nil)
	h.UndoPlanningRound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if got := resp["reverted_round"]; got != float64(2) {
		t.Errorf("reverted_round = %v, want 2", got)
	}
	if got := resp["mode"]; got != "task" {
		t.Errorf("mode = %v, want task", got)
	}
	if got := resp["thread_id"]; got != threadID {
		t.Errorf("thread_id = %v, want %q", got, threadID)
	}

	// Task prompt should be restored to round-2's prev (= "v1").
	updated, err := h.store.GetTask(ctx, tsk.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.Prompt != "v1" {
		t.Errorf("task.Prompt = %q, want %q", updated.Prompt, "v1")
	}

	// A prompt_round_revert event for round 2 must exist.
	events, _ := h.store.GetEvents(ctx, tsk.ID)
	var foundRevert bool
	for _, ev := range events {
		if ev.EventType != store.EventTypePromptRoundRevert {
			continue
		}
		var data store.PromptRoundRevertData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		if data.ThreadID == threadID && data.RevertedRound == 2 {
			foundRevert = true
		}
	}
	if !foundRevert {
		t.Error("prompt_round_revert event for round 2 not found")
	}
}

func TestUndo_TaskMode_RepeatedUndo(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	_ = p.Start(context.Background())
	h.planner = p

	ctx := context.Background()
	tsk, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "original", Timeout: 60})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}

	threadID := p.Threads().ActiveID()
	cs, _ := p.Threads().Store(threadID)
	_ = cs.SaveSession(planner.SessionInfo{FocusedTask: tsk.ID.String()})

	// Two prompt rounds.
	seedPromptRound(t, h, tsk.ID, threadID, 1, "v1")
	seedPromptRound(t, h, tsk.ID, threadID, 2, "v2")

	// First undo: reverts round 2.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/undo?thread="+threadID, nil)
	h.UndoPlanningRound(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("undo1 status = %d: %s", rec.Code, rec.Body.String())
	}

	// Second undo: reverts round 1.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/planning/undo?thread="+threadID, nil)
	h.UndoPlanningRound(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("undo2 status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if got := resp["reverted_round"]; got != float64(1) {
		t.Errorf("reverted_round = %v, want 1", got)
	}

	// Prompt should be back to original.
	updated, _ := h.store.GetTask(ctx, tsk.ID)
	if updated.Prompt != "original" {
		t.Errorf("task.Prompt = %q, want %q", updated.Prompt, "original")
	}

	// Two prompt_round_revert events must exist.
	events, _ := h.store.GetEvents(ctx, tsk.ID)
	var revertCount int
	for _, ev := range events {
		if ev.EventType == store.EventTypePromptRoundRevert {
			revertCount++
		}
	}
	if revertCount != 2 {
		t.Errorf("prompt_round_revert count = %d, want 2", revertCount)
	}
}

func TestUndo_TaskMode_NothingToUndo(t *testing.T) {
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	_ = p.Start(context.Background())
	h.planner = p

	ctx := context.Background()
	tsk, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "original", Timeout: 60})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}

	threadID := p.Threads().ActiveID()
	cs, _ := p.Threads().Store(threadID)
	_ = cs.SaveSession(planner.SessionInfo{FocusedTask: tsk.ID.String()})
	_ = ctx

	// No prompt_round events written — undo should return 409.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/undo?thread="+threadID, nil)
	h.UndoPlanningRound(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409\nbody: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no planning rounds to undo") {
		t.Errorf("body = %q, want error about no planning rounds to undo", rec.Body.String())
	}
}

func TestUndo_FileMode_Unchanged(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "foo.md", "# foo\n")
	// Seed a planning commit with an explicit thread trailer so the lookup
	// can match it when a thread ID is passed via ?thread=.
	runGit(t, ws, "add", "specs/")
	msg := "specs(plan): drafted foo\n\nPlan-Round: 1\nPlan-Thread: spec-thread-1"
	runGit(t, ws, "commit", "-m", msg)

	h := newStaticWorkspaceHandler(t, []string{ws})
	// Attach a planner whose active thread is in spec-mode (no FocusedTask).
	p := newPlannerWithStore(t)
	_ = p.Start(context.Background())
	h.planner = p

	specThreadID := p.Threads().ActiveID()
	// Do NOT set FocusedTask — this keeps the thread in spec-mode.

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/undo?thread="+specThreadID, nil)
	h.UndoPlanningRound(rec, req)

	// Should fall through to the git-revert path. Because the planning
	// commit was written with a different Plan-Thread, the handler won't
	// find a matching commit and returns 409 "no planning commits to undo".
	// The important invariant is that it did NOT call undoTaskModeRound
	// (which would 503 on a missing store or return a task-mode payload).
	if rec.Code == http.StatusServiceUnavailable {
		t.Fatalf("handler entered task-mode path unexpectedly (503)")
	}

	// Verify the response is not a task-mode payload.
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["mode"] == "task" {
		t.Errorf("spec-mode thread returned mode=task; git-revert path was bypassed")
	}
}

func TestUndo_TaskMode_DoesNotTouchGit(t *testing.T) {
	ws := initPlanningTestRepo(t)
	// Seed a planning commit in the workspace so there is commit history.
	writeSpec(t, ws, "existing.md", "# existing\n")
	seedPlanningCommit(t, ws, 1, "existing spec")
	commitsBefore := gitLogSubjects(t, ws)

	h := newTestHandler(t)
	h.workspaces = []string{ws}

	p := newPlannerWithStore(t)
	_ = p.Start(context.Background())
	h.planner = p

	ctx := context.Background()
	tsk, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "original", Timeout: 60})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}

	threadID := p.Threads().ActiveID()
	cs, _ := p.Threads().Store(threadID)
	_ = cs.SaveSession(planner.SessionInfo{FocusedTask: tsk.ID.String()})

	seedPromptRound(t, h, tsk.ID, threadID, 1, "v1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/undo?thread="+threadID, nil)
	h.UndoPlanningRound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nbody: %s", rec.Code, rec.Body.String())
	}

	// Git log must be unchanged — task-mode undo must not create any commits.
	commitsAfter := gitLogSubjects(t, ws)
	if len(commitsAfter) != len(commitsBefore) {
		t.Errorf("git commit count changed from %d to %d; task-mode undo must not write commits",
			len(commitsBefore), len(commitsAfter))
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
