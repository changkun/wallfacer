package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/spec"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

const testSpecValidated = `---
title: Test Spec
status: validated
depends_on: []
affects:
  - internal/test/
effort: small
created: 2026-01-01
updated: 2026-01-01
author: test
dispatched_task_id: null
---

# Test Spec

Implement something useful.
`

const testSpecDrafted = `---
title: Drafted Spec
status: drafted
depends_on: []
affects: []
effort: small
created: 2026-01-01
updated: 2026-01-01
author: test
dispatched_task_id: null
---

# Drafted Spec

Not ready yet.
`

func writeTestSpec(t *testing.T, ws, relPath, content string) {
	t.Helper()
	abs := filepath.Join(ws, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func newDispatchTestHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	h, ws := newTestHandlerWithWorkspaces(t)
	return h, ws
}

type dispatchResponse struct {
	Dispatched []dispatchResult `json:"dispatched"`
	Errors     []dispatchError  `json:"errors"`
}

func doDispatch(t *testing.T, h *Handler, paths []string, run bool) (*httptest.ResponseRecorder, dispatchResponse) {
	t.Helper()
	body := map[string]any{"paths": paths, "run": run}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/specs/dispatch", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.DispatchSpecs(w, req)
	var resp dispatchResponse
	if w.Code == http.StatusCreated {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return w, resp
}

func TestDispatchSpecs_SingleSpec(t *testing.T) {
	h, ws := newDispatchTestHandler(t)
	writeTestSpec(t, ws, "specs/local/test.md", testSpecValidated)

	w, resp := doDispatch(t, h, []string{"specs/local/test.md"}, false)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if len(resp.Dispatched) != 1 {
		t.Fatalf("dispatched count = %d, want 1", len(resp.Dispatched))
	}
	if resp.Dispatched[0].SpecPath != "specs/local/test.md" {
		t.Errorf("spec_path = %q, want %q", resp.Dispatched[0].SpecPath, "specs/local/test.md")
	}
	if resp.Dispatched[0].TaskID == "" {
		t.Error("task_id is empty")
	}

	// Verify task was created with correct prompt.
	tasks, _ := h.store.ListTasks(context.Background(), false)
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}
	if !strings.Contains(tasks[0].Prompt, "Implement something useful.") {
		t.Errorf("task prompt = %q, should contain spec body", tasks[0].Prompt)
	}

	// Verify dispatched_task_id was written back to spec file.
	s, err := spec.ParseFile(filepath.Join(ws, "specs/local/test.md"))
	if err != nil {
		t.Fatalf("parse spec after dispatch: %v", err)
	}
	if s.DispatchedTaskID == nil {
		t.Fatal("dispatched_task_id is nil after dispatch")
	}
	if *s.DispatchedTaskID != resp.Dispatched[0].TaskID {
		t.Errorf("dispatched_task_id = %q, want %q", *s.DispatchedTaskID, resp.Dispatched[0].TaskID)
	}
}

func TestDispatchSpecs_BatchWithDependencies(t *testing.T) {
	h, ws := newDispatchTestHandler(t)

	specA := `---
title: Spec A
status: validated
depends_on: []
affects: []
effort: small
created: 2026-01-01
updated: 2026-01-01
author: test
dispatched_task_id: null
---

# Spec A

Foundation work.
`
	specB := `---
title: Spec B
status: validated
depends_on:
  - specs/local/a.md
affects: []
effort: small
created: 2026-01-01
updated: 2026-01-01
author: test
dispatched_task_id: null
---

# Spec B

Depends on A.
`
	writeTestSpec(t, ws, "specs/local/a.md", specA)
	writeTestSpec(t, ws, "specs/local/b.md", specB)

	w, resp := doDispatch(t, h, []string{"specs/local/a.md", "specs/local/b.md"}, false)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if len(resp.Dispatched) != 2 {
		t.Fatalf("dispatched count = %d, want 2", len(resp.Dispatched))
	}

	// Find task B and verify it depends on task A.
	taskAID := resp.Dispatched[0].TaskID
	tasks, _ := h.store.ListTasks(context.Background(), false)
	var taskB *struct {
		DependsOn []string
	}
	for _, task := range tasks {
		if task.ID.String() == resp.Dispatched[1].TaskID {
			taskB = &struct{ DependsOn []string }{DependsOn: task.DependsOn}
			break
		}
	}
	if taskB == nil {
		t.Fatal("task B not found")
	}
	if len(taskB.DependsOn) != 1 || taskB.DependsOn[0] != taskAID {
		t.Errorf("task B depends_on = %v, want [%s]", taskB.DependsOn, taskAID)
	}
}

func TestDispatchSpecs_RejectsNonValidated(t *testing.T) {
	h, ws := newDispatchTestHandler(t)
	writeTestSpec(t, ws, "specs/local/draft.md", testSpecDrafted)

	w, resp := doDispatch(t, h, []string{"specs/local/draft.md"}, false)

	// Should return 400 because all specs failed validation.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Errors) != 1 {
		t.Fatalf("errors count = %d, want 1", len(resp.Errors))
	}
	if !strings.Contains(resp.Errors[0].Error, "drafted") {
		t.Errorf("error message = %q, should mention status", resp.Errors[0].Error)
	}
}

func TestDispatchSpecs_RejectsAlreadyDispatched(t *testing.T) {
	h, ws := newDispatchTestHandler(t)

	alreadyDispatched := `---
title: Already Dispatched
status: validated
depends_on: []
affects: []
effort: small
created: 2026-01-01
updated: 2026-01-01
author: test
dispatched_task_id: 550e8400-e29b-41d4-a716-446655440000
---

# Already Dispatched
`
	writeTestSpec(t, ws, "specs/local/dispatched.md", alreadyDispatched)

	w, resp := doDispatch(t, h, []string{"specs/local/dispatched.md"}, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Errors) != 1 {
		t.Fatalf("errors count = %d, want 1", len(resp.Errors))
	}
	if !strings.Contains(resp.Errors[0].Error, "already dispatched") {
		t.Errorf("error message = %q, should mention already dispatched", resp.Errors[0].Error)
	}
}

func TestDispatchSpecs_RejectsNonLeaf(t *testing.T) {
	h, ws := newDispatchTestHandler(t)
	// Create a non-leaf spec: parent.md with a child directory parent/ containing a child spec.
	writeTestSpec(t, ws, "specs/local/parent.md", testSpecValidated)
	writeTestSpec(t, ws, "specs/local/parent/child.md", testSpecValidated)

	w, resp := doDispatch(t, h, []string{"specs/local/parent.md"}, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Errors) != 1 {
		t.Fatalf("errors count = %d, want 1", len(resp.Errors))
	}
	if !strings.Contains(resp.Errors[0].Error, "non-leaf") {
		t.Errorf("error message = %q, should mention non-leaf", resp.Errors[0].Error)
	}
}

func TestDispatchSpecs_SpecSourcePath(t *testing.T) {
	h, ws := newDispatchTestHandler(t)
	writeTestSpec(t, ws, "specs/local/source.md", testSpecValidated)

	w, resp := doDispatch(t, h, []string{"specs/local/source.md"}, false)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	tasks, _ := h.store.ListTasks(context.Background(), false)
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}
	if tasks[0].SpecSourcePath != "specs/local/source.md" {
		t.Errorf("SpecSourcePath = %q, want %q", tasks[0].SpecSourcePath, "specs/local/source.md")
	}
	_ = resp
}

func TestDispatchSpecs_EmptyPaths(t *testing.T) {
	h, _ := newDispatchTestHandler(t)

	w, _ := doDispatch(t, h, []string{}, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDispatchSpecs_SpecNotFound(t *testing.T) {
	h, _ := newDispatchTestHandler(t)

	w, resp := doDispatch(t, h, []string{"specs/nonexistent.md"}, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Errors) != 1 {
		t.Fatalf("errors count = %d, want 1", len(resp.Errors))
	}
	if !strings.Contains(resp.Errors[0].Error, "not found") {
		t.Errorf("error message = %q, should mention not found", resp.Errors[0].Error)
	}
}

func TestDispatchSpecs_Tags(t *testing.T) {
	h, ws := newDispatchTestHandler(t)
	writeTestSpec(t, ws, "specs/local/tagged.md", testSpecValidated)

	w, _ := doDispatch(t, h, []string{"specs/local/tagged.md"}, false)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	tasks, _ := h.store.ListTasks(context.Background(), false)
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}
	foundSpecDispatched := false
	for _, tag := range tasks[0].Tags {
		if tag == "spec-dispatched" {
			foundSpecDispatched = true
		}
	}
	if !foundSpecDispatched {
		t.Errorf("tags = %v, should contain 'spec-dispatched'", tasks[0].Tags)
	}
}

// --- Undispatch tests ---

type undispatchResponse struct {
	Undispatched []undispatchResult `json:"undispatched"`
	Errors       []dispatchError    `json:"errors"`
}

func doUndispatch(t *testing.T, h *Handler, paths []string) (*httptest.ResponseRecorder, undispatchResponse) {
	t.Helper()
	body := map[string]any{"paths": paths}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/specs/undispatch", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.UndispatchSpecs(w, req)
	var resp undispatchResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

func TestUndispatchSpecs_CancelsBacklogTask(t *testing.T) {
	h, ws := newDispatchTestHandler(t)
	writeTestSpec(t, ws, "specs/local/cancel.md", testSpecValidated)

	// First dispatch.
	dw, dresp := doDispatch(t, h, []string{"specs/local/cancel.md"}, false)
	if dw.Code != http.StatusCreated {
		t.Fatalf("dispatch failed: %d %s", dw.Code, dw.Body.String())
	}

	// Task should be in backlog.
	taskID := dresp.Dispatched[0].TaskID

	// Now undispatch.
	uw, uresp := doUndispatch(t, h, []string{"specs/local/cancel.md"})
	if uw.Code != http.StatusOK {
		t.Fatalf("undispatch status = %d, want %d; body: %s", uw.Code, http.StatusOK, uw.Body.String())
	}
	if len(uresp.Undispatched) != 1 {
		t.Fatalf("undispatched count = %d, want 1", len(uresp.Undispatched))
	}
	if uresp.Undispatched[0].TaskID != taskID {
		t.Errorf("task_id = %q, want %q", uresp.Undispatched[0].TaskID, taskID)
	}

	// Task should be cancelled.
	task, err := h.store.GetTask(context.Background(), uuid.MustParse(taskID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != "cancelled" {
		t.Errorf("task status = %q, want %q", task.Status, "cancelled")
	}

	// Spec should have dispatched_task_id cleared.
	s, err := spec.ParseFile(filepath.Join(ws, "specs/local/cancel.md"))
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if s.DispatchedTaskID != nil {
		t.Errorf("dispatched_task_id = %v, want nil", s.DispatchedTaskID)
	}
}

func TestUndispatchSpecs_DoneTask(t *testing.T) {
	h, ws := newDispatchTestHandler(t)
	writeTestSpec(t, ws, "specs/local/done.md", testSpecValidated)

	// Dispatch, then force task to done.
	dw, dresp := doDispatch(t, h, []string{"specs/local/done.md"}, false)
	if dw.Code != http.StatusCreated {
		t.Fatalf("dispatch failed: %d %s", dw.Code, dw.Body.String())
	}
	taskID := uuid.MustParse(dresp.Dispatched[0].TaskID)
	_ = h.store.ForceUpdateTaskStatus(context.Background(), taskID, "done")

	// Undispatch — should clear spec but NOT cancel task.
	uw, uresp := doUndispatch(t, h, []string{"specs/local/done.md"})
	if uw.Code != http.StatusOK {
		t.Fatalf("undispatch status = %d, want %d; body: %s", uw.Code, http.StatusOK, uw.Body.String())
	}
	if len(uresp.Undispatched) != 1 {
		t.Fatalf("undispatched count = %d, want 1", len(uresp.Undispatched))
	}

	// Task should still be done.
	task, err := h.store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != "done" {
		t.Errorf("task status = %q, want %q (should not be cancelled)", task.Status, "done")
	}

	// Spec frontmatter should be cleared.
	s, err := spec.ParseFile(filepath.Join(ws, "specs/local/done.md"))
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if s.DispatchedTaskID != nil {
		t.Errorf("dispatched_task_id = %v, want nil", s.DispatchedTaskID)
	}
}

func TestUndispatchSpecs_NotDispatched(t *testing.T) {
	h, ws := newDispatchTestHandler(t)
	writeTestSpec(t, ws, "specs/local/notdispatched.md", testSpecValidated)

	uw, uresp := doUndispatch(t, h, []string{"specs/local/notdispatched.md"})
	if uw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", uw.Code, http.StatusBadRequest, uw.Body.String())
	}
	if len(uresp.Errors) != 1 {
		t.Fatalf("errors count = %d, want 1", len(uresp.Errors))
	}
	if !strings.Contains(uresp.Errors[0].Error, "not dispatched") {
		t.Errorf("error = %q, should mention not dispatched", uresp.Errors[0].Error)
	}
}

func TestUndispatchSpecs_SpecReturnsToValidated(t *testing.T) {
	h, ws := newDispatchTestHandler(t)
	writeTestSpec(t, ws, "specs/local/revalidate.md", testSpecValidated)

	// Dispatch first.
	dw, _ := doDispatch(t, h, []string{"specs/local/revalidate.md"}, false)
	if dw.Code != http.StatusCreated {
		t.Fatalf("dispatch failed: %d %s", dw.Code, dw.Body.String())
	}

	// Verify spec status changed (dispatch doesn't change status, but dispatched_task_id is set).
	// Now undispatch.
	uw, _ := doUndispatch(t, h, []string{"specs/local/revalidate.md"})
	if uw.Code != http.StatusOK {
		t.Fatalf("undispatch status = %d, want %d; body: %s", uw.Code, http.StatusOK, uw.Body.String())
	}

	s, err := spec.ParseFile(filepath.Join(ws, "specs/local/revalidate.md"))
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if s.Status != spec.StatusValidated {
		t.Errorf("status = %q, want %q", s.Status, spec.StatusValidated)
	}
	if s.DispatchedTaskID != nil {
		t.Errorf("dispatched_task_id = %v, want nil", s.DispatchedTaskID)
	}
}

// --- SpecCompletionHook tests ---

func TestCompletionHook_UpdatesSpecStatus(t *testing.T) {
	_, ws := newDispatchTestHandler(t)
	writeTestSpec(t, ws, "specs/local/hooktest.md", testSpecValidated)

	hook := SpecCompletionHook(func() []string { return []string{ws} })
	hook(store.Task{
		SpecSourcePath: "specs/local/hooktest.md",
	})

	s, err := spec.ParseFile(filepath.Join(ws, "specs/local/hooktest.md"))
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if s.Status != spec.StatusComplete {
		t.Errorf("status = %q, want %q", s.Status, spec.StatusComplete)
	}
}

func TestCompletionHook_NoSpecPath(t *testing.T) {
	hook := SpecCompletionHook(func() []string { return []string{t.TempDir()} })
	hook(store.Task{}) // no-op, no crash
}

func TestCompletionHook_SpecFileNotFound(t *testing.T) {
	hook := SpecCompletionHook(func() []string { return []string{t.TempDir()} })
	hook(store.Task{
		SpecSourcePath: "specs/nonexistent.md",
	}) // logs warning, no crash
}

func TestDispatch_ArchivedSpecRejectedWithMessage(t *testing.T) {
	h, ws := newDispatchTestHandler(t)
	archivedSpec := strings.Replace(testSpecValidated, "status: validated", "status: archived", 1)
	writeTestSpec(t, ws, "specs/local/archived.md", archivedSpec)

	w, resp := doDispatch(t, h, []string{"specs/local/archived.md"}, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Errors) != 1 {
		t.Fatalf("errors count = %d, want 1", len(resp.Errors))
	}
	if !strings.Contains(resp.Errors[0].Error, "unarchive") {
		t.Errorf("error message = %q, should mention unarchive", resp.Errors[0].Error)
	}
}

func TestDispatch_ArchivedDependencyTreatedAsSatisfied(t *testing.T) {
	h, ws := newDispatchTestHandler(t)

	archivedDep := strings.Replace(testSpecValidated, "status: validated", "status: archived", 1)
	writeTestSpec(t, ws, "specs/local/archived-dep.md", archivedDep)

	specWithArchivedDep := `---
title: Spec With Archived Dep
status: validated
depends_on:
  - specs/local/archived-dep.md
affects: []
effort: small
created: 2026-01-01
updated: 2026-01-01
author: test
dispatched_task_id: null
---

# Spec With Archived Dep

Depends on an archived spec, should still dispatch without blocker edge.
`
	writeTestSpec(t, ws, "specs/local/live.md", specWithArchivedDep)

	w, resp := doDispatch(t, h, []string{"specs/local/live.md"}, false)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if len(resp.Dispatched) != 1 {
		t.Fatalf("dispatched count = %d, want 1", len(resp.Dispatched))
	}

	// Verify no blocker edge was added for the archived dep.
	tasks, _ := h.store.ListTasks(context.Background(), false)
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}
	if len(tasks[0].DependsOn) != 0 {
		t.Errorf("archived dep should not contribute blocker edge, got DependsOn = %v", tasks[0].DependsOn)
	}
}

func TestDispatch_ArchivedDependencyInBatch(t *testing.T) {
	h, ws := newDispatchTestHandler(t)

	archivedSpec := strings.Replace(testSpecValidated, "status: validated", "status: archived", 1)
	writeTestSpec(t, ws, "specs/local/archived.md", archivedSpec)
	writeTestSpec(t, ws, "specs/local/valid.md", testSpecValidated)

	w, resp := doDispatch(t, h, []string{"specs/local/archived.md", "specs/local/valid.md"}, false)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if len(resp.Dispatched) != 1 {
		t.Fatalf("dispatched count = %d, want 1 (valid only)", len(resp.Dispatched))
	}
	if resp.Dispatched[0].SpecPath != "specs/local/valid.md" {
		t.Errorf("dispatched spec = %q, want specs/local/valid.md", resp.Dispatched[0].SpecPath)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("errors count = %d, want 1 (archived rejected)", len(resp.Errors))
	}
	if resp.Errors[0].SpecPath != "specs/local/archived.md" {
		t.Errorf("error spec = %q, want specs/local/archived.md", resp.Errors[0].SpecPath)
	}
	if !strings.Contains(resp.Errors[0].Error, "unarchive") {
		t.Errorf("error message = %q, should mention unarchive", resp.Errors[0].Error)
	}
}

func TestCompletionHook_AlreadyComplete(t *testing.T) {
	_, ws := newDispatchTestHandler(t)
	completeSpec := strings.Replace(testSpecValidated, "status: validated", "status: complete", 1)
	writeTestSpec(t, ws, "specs/local/already.md", completeSpec)

	hook := SpecCompletionHook(func() []string { return []string{ws} })
	hook(store.Task{
		SpecSourcePath: "specs/local/already.md",
	})

	s, err := spec.ParseFile(filepath.Join(ws, "specs/local/already.md"))
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if s.Status != spec.StatusComplete {
		t.Errorf("status = %q, want %q", s.Status, spec.StatusComplete)
	}
}
