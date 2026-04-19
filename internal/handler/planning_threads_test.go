package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/planner"
	"changkun.de/x/wallfacer/internal/store"
)

// newPlannerHandlerWithThreads returns a handler backed by a planner
// whose ThreadManager is rooted at a temp directory. Tests use this to
// exercise the thread CRUD routes without spinning up a real sandbox.
func newPlannerHandlerWithThreads(t *testing.T) *Handler {
	t.Helper()
	h := newTestHandler(t)
	p := newPlannerWithStore(t)
	_ = p.Start(context.Background())
	h.planner = p
	return h
}

func TestListPlanningThreads_DefaultChat1(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/threads", nil)
	h.ListPlanningThreads(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Threads  []map[string]any `json:"threads"`
		ActiveID string           `json:"active_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Threads) != 1 {
		t.Fatalf("threads = %d, want 1", len(resp.Threads))
	}
	if resp.Threads[0]["name"] != "Chat 1" {
		t.Errorf("name = %v, want Chat 1", resp.Threads[0]["name"])
	}
	if resp.ActiveID == "" {
		t.Error("active_id should be set")
	}
}

func TestCreateRenameArchivePlanningThread(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)

	// Create.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/threads",
		strings.NewReader(`{"name":"Auth"}`))
	h.CreatePlanningThread(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)
	if id == "" {
		t.Fatal("created id is empty")
	}

	// Rename.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/planning/threads/"+id,
		strings.NewReader(`{"name":"Auth refactor"}`))
	req.SetPathValue("id", id)
	h.RenamePlanningThread(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d: %s", rec.Code, rec.Body.String())
	}

	// Archive.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/planning/threads/"+id+"/archive", nil)
	req.SetPathValue("id", id)
	h.ArchivePlanningThread(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status = %d: %s", rec.Code, rec.Body.String())
	}

	// List without archived should no longer include it.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/planning/threads", nil)
	h.ListPlanningThreads(rec, req)
	if strings.Contains(rec.Body.String(), id) {
		t.Errorf("archived thread still in non-archived list: %s", rec.Body.String())
	}

	// List with archived includes it.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/planning/threads?includeArchived=true", nil)
	h.ListPlanningThreads(rec, req)
	if !strings.Contains(rec.Body.String(), id) {
		t.Errorf("archived thread missing from includeArchived list: %s", rec.Body.String())
	}

	// Unarchive brings it back.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/planning/threads/"+id+"/unarchive", nil)
	req.SetPathValue("id", id)
	h.UnarchivePlanningThread(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d", rec.Code)
	}
}

func TestArchivePlanningThread_RejectsInFlight(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)
	threads := h.planner.Threads().List(false)
	id := threads[0].ID
	// Simulate an exec in flight owned by this thread.
	h.planner.SetBusy(true, id)
	defer h.planner.SetBusy(false, "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/planning/threads/"+id+"/archive", nil)
	req.SetPathValue("id", id)
	h.ArchivePlanningThread(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (busy)", rec.Code)
	}
}

func TestUndoPlanningRound_ThreadScoped(t *testing.T) {
	ws := initPlanningTestRepo(t)
	h := newStaticWorkspaceHandler(t, []string{ws})

	// Thread A writes round 1.
	writeSpec(t, ws, "a.md", "# a\n")
	seedPlanningCommitWithThread(t, ws, 1, "drafted a", "thread-a")
	// Thread B writes round 2 on top.
	writeSpec(t, ws, "b.md", "# b\n")
	seedPlanningCommitWithThread(t, ws, 2, "drafted b", "thread-b")

	// Undo from thread A — should revert A's round 1 even though B's
	// round 2 sits on top, thanks to the git revert flow.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/planning/undo?thread=thread-a", nil)
	h.UndoPlanningRound(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("undo status = %d: %s", rec.Code, rec.Body.String())
	}

	// a.md should be gone (revert removed it) but b.md should remain
	// (thread B's round 2 is untouched).
	subjects := gitLogSubjects(t, ws)
	var sawRevert bool
	for _, s := range subjects {
		if strings.Contains(s, "revert round 1") {
			sawRevert = true
		}
	}
	if !sawRevert {
		t.Errorf("expected a revert commit for round 1; subjects = %v", subjects)
	}
}

func TestUndoPlanningRound_NoCommitsForThread(t *testing.T) {
	ws := initPlanningTestRepo(t)
	h := newStaticWorkspaceHandler(t, []string{ws})

	// A commit exists but it belongs to a different thread.
	writeSpec(t, ws, "x.md", "# x\n")
	seedPlanningCommitWithThread(t, ws, 1, "drafted x", "thread-other")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/planning/undo?thread=thread-missing", nil)
	h.UndoPlanningRound(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

// TestCreateThread_TaskMode verifies that creating a thread with a valid
// focused_task pins it to task-mode and that the list endpoint returns the
// correct mode and task_id fields.
func TestCreateThread_TaskMode(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)

	// Create a real task in the store so focused_task validation passes.
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt:  "test task for thread",
		Timeout: 15,
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	taskID := task.ID.String()

	// Create thread with focused_task.
	body := strings.NewReader(`{"name":"Task Thread","focused_task":"` + taskID + `"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/threads", body)
	h.CreatePlanningThread(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}

	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created["mode"] != "task" {
		t.Errorf("created mode = %v, want task", created["mode"])
	}
	if created["task_id"] != taskID {
		t.Errorf("created task_id = %v, want %s", created["task_id"], taskID)
	}
	createdID := created["id"].(string)

	// List threads and verify mode is preserved.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/planning/threads", nil)
	h.ListPlanningThreads(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", rec.Code, rec.Body.String())
	}

	var listResp struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var found bool
	for _, th := range listResp.Threads {
		if th["id"] == createdID {
			found = true
			if th["mode"] != "task" {
				t.Errorf("list mode = %v, want task", th["mode"])
			}
			if th["task_id"] != taskID {
				t.Errorf("list task_id = %v, want %s", th["task_id"], taskID)
			}
		}
	}
	if !found {
		t.Errorf("created thread %s not found in list", createdID)
	}
}

// TestCreateThread_TaskMode_UnknownTask verifies that creating a thread with
// a non-existent focused_task returns 404.
func TestCreateThread_TaskMode_UnknownTask(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)

	body := strings.NewReader(`{"name":"Orphan","focused_task":"00000000-0000-0000-0000-000000000001"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/planning/threads", body)
	h.CreatePlanningThread(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (unknown task)", rec.Code, http.StatusNotFound)
	}
}

// TestListPlanningThreads_DefaultMode verifies that the initial "Chat 1"
// thread has mode "spec" and no task_id.
func TestListPlanningThreads_DefaultMode(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/planning/threads", nil)
	h.ListPlanningThreads(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Threads) == 0 {
		t.Fatal("expected at least one thread")
	}
	th := resp.Threads[0]
	if th["mode"] != "spec" {
		t.Errorf("mode = %v, want spec", th["mode"])
	}
	if th["task_id"] != nil && th["task_id"] != "" {
		t.Errorf("task_id = %v, want empty for spec-mode thread", th["task_id"])
	}
}

// pinThreadToTask saves a task-mode session for a thread, making it look like
// the thread is actively planning for the given task.
func pinThreadToTask(t *testing.T, tm *planner.ThreadManager, threadID, taskID string) {
	t.Helper()
	cs, err := tm.Store(threadID)
	if err != nil {
		t.Fatalf("pinThreadToTask: Store(%q): %v", threadID, err)
	}
	if err := cs.SaveSession(planner.SessionInfo{FocusedTask: taskID}); err != nil {
		t.Fatalf("pinThreadToTask: SaveSession: %v", err)
	}
}

func TestCascade_ArchiveOnTaskLeavesBacklog(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)
	ctx := context.Background()

	// Create a backlog task.
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "cascade test"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Pin the planner thread to this task.
	tm := h.planner.Threads()
	threads := tm.List(false)
	if len(threads) == 0 {
		t.Fatal("expected at least one thread")
	}
	threadID := threads[0].ID
	pinThreadToTask(t, tm, threadID, task.ID.String())

	// Transition task backlog → in_progress via the PATCH handler.
	body := `{"status":"in_progress"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/api/tasks/"+task.ID.String(), strings.NewReader(body))
	req.SetPathValue("id", task.ID.String())
	h.UpdateTask(rec, req, task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("UpdateTask status = %d: %s", rec.Code, rec.Body.String())
	}

	// Thread must now be archived with the cascade flag set.
	meta, err := tm.Meta(threadID)
	if err != nil {
		t.Fatalf("tm.Meta: %v", err)
	}
	if !meta.Archived {
		t.Error("thread should be archived after task leaves backlog")
	}
	if !meta.AutoArchivedByTaskLifecycle {
		t.Error("AutoArchivedByTaskLifecycle should be true")
	}
}

func TestCascade_UnarchivesOnTaskUnarchive(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)
	ctx := context.Background()

	// Create a done task so we can archive it.
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "unarchive cascade"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	// Pin the planner thread to this task.
	tm := h.planner.Threads()
	threads := tm.List(false)
	if len(threads) == 0 {
		t.Fatal("expected at least one thread")
	}
	threadID := threads[0].ID
	pinThreadToTask(t, tm, threadID, task.ID.String())

	// Archive the task (this should cascade-archive the thread).
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/"+task.ID.String()+"/archive", nil)
	req.SetPathValue("id", task.ID.String())
	h.ArchiveTask(rec, req, task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("ArchiveTask status = %d: %s", rec.Code, rec.Body.String())
	}

	meta, err := tm.Meta(threadID)
	if err != nil {
		t.Fatalf("meta after archive: %v", err)
	}
	if !meta.Archived || !meta.AutoArchivedByTaskLifecycle {
		t.Fatal("thread should be cascade-archived after task archive")
	}

	// Unarchive the task — thread should be un-archived too.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/tasks/"+task.ID.String()+"/unarchive", nil)
	req.SetPathValue("id", task.ID.String())
	h.UnarchiveTask(rec, req, task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("UnarchiveTask status = %d: %s", rec.Code, rec.Body.String())
	}

	meta, err = tm.Meta(threadID)
	if err != nil {
		t.Fatalf("meta after unarchive: %v", err)
	}
	if meta.Archived {
		t.Error("thread should be unarchived after task unarchive")
	}
	if meta.AutoArchivedByTaskLifecycle {
		t.Error("AutoArchivedByTaskLifecycle should be cleared after unarchive")
	}
}

func TestCascade_ManuallyUnarchivedThreadStaysAfterTaskReArchive(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "re-archive test"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	tm := h.planner.Threads()
	threads := tm.List(false)
	threadID := threads[0].ID
	pinThreadToTask(t, tm, threadID, task.ID.String())

	// Archive task → cascade-archive thread.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/"+task.ID.String()+"/archive", nil)
	req.SetPathValue("id", task.ID.String())
	h.ArchiveTask(rec, req, task.ID)

	// Manually unarchive just the thread (simulates user action).
	// Under the new semantics, Unarchive() intentionally keeps
	// AutoArchivedByTaskLifecycle=true so CascadeArchiveForTask knows to skip
	// this thread in future.
	if err := tm.Unarchive(threadID); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	meta, _ := tm.Meta(threadID)
	if meta.Archived {
		t.Fatal("thread should be visible after manual unarchive")
	}
	if !meta.AutoArchivedByTaskLifecycle {
		t.Fatal("flag must be preserved after manual unarchive so future cascades skip this thread")
	}

	// Unarchive the task then archive it again — thread should NOT be
	// re-cascade-archived because flag=true signals user intent to keep it
	// visible.
	if err := h.store.SetTaskArchived(ctx, task.ID, false); err != nil {
		t.Fatalf("SetTaskArchived false: %v", err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/tasks/"+task.ID.String()+"/archive", nil)
	req.SetPathValue("id", task.ID.String())
	h.ArchiveTask(rec, req, task.ID)

	meta, _ = tm.Meta(threadID)
	if meta.Archived {
		t.Error("should not re-cascade after manual unarchive: user intent (flag=true, Archived=false) must be respected")
	}
}

func TestUpdateTaskPromptTool_FailsOnCascadeArchivedThread(t *testing.T) {
	h := newPlannerHandlerWithThreads(t)
	ctx := context.Background()

	// Create a backlog task.
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "prompt tool test"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Pin the thread to this task.
	tm := h.planner.Threads()
	threads := tm.List(false)
	threadID := threads[0].ID
	pinThreadToTask(t, tm, threadID, task.ID.String())

	// Cascade-archive the thread manually (simulates task leaving backlog).
	if _, err := tm.CascadeArchiveForTask(task.ID.String()); err != nil {
		t.Fatalf("CascadeArchiveForTask: %v", err)
	}

	// Now the tool should return an error.
	body := `{"task_id":"` + task.ID.String() + `","prompt":"new prompt","thread_id":"` + threadID + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/planning/tool/update_task_prompt", strings.NewReader(body))
	h.UpdateTaskPromptTool(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "moved past backlog") {
		t.Errorf("unexpected error body: %s", rec.Body.String())
	}

	// On the non-cascade-archived state it should work fine (restore first).
	if err := tm.Unarchive(threadID); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/planning/tool/update_task_prompt", strings.NewReader(body))
	h.UpdateTaskPromptTool(rec, req)
	// Should succeed (200) or fail for a different reason (not 422 cascade-archived).
	if rec.Code == http.StatusUnprocessableEntity && strings.Contains(rec.Body.String(), "moved past backlog") {
		t.Errorf("should not reject after manual unarchive; body = %s", rec.Body.String())
	}
}

// seedPlanningCommitWithThread creates a planning commit carrying both
// Plan-Round and Plan-Thread trailers. Mirrors the output of the
// production commitPlanningRound path once the revert change landed.
func seedPlanningCommitWithThread(t *testing.T, ws string, round int, summary, threadID string) {
	t.Helper()
	runGit(t, ws, "add", "specs/")
	msg := fmt.Sprintf("specs(plan): %s\n\nseeded by test\n\nPlan-Round: %d\nPlan-Thread: %s",
		summary, round, threadID)
	runGit(t, ws, "commit", "-m", msg)
}
