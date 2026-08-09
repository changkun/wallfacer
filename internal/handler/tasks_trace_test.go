package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"latere.ai/x/wallfacer/internal/store"
)

// storedTraceJSON mirrors the opaque string the runner persists from a topos
// run (capitalised keys, no json tags; see internal/agentgraph.Trace).
const storedTraceJSON = `{"Nodes":[` +
	`{"ID":"run-x/planner","Name":"planner","Role":"Planner","Status":"done","Grants":["read"],"Sandbox":"local"},` +
	`{"ID":"run-x/builder","Name":"builder","Role":"Builder","Status":"running","Grants":[],"Sandbox":""}],` +
	`"Edges":[{"From":"run-x/planner","To":"run-x/builder","Kind":"next"}]}`

// TestTaskTrace_WithTrace verifies the handler reparses the opaque stored
// trace into the thin lowercase-keyed nodes/edges the UI consumes.
func TestTaskTrace_WithTrace(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "agentic task", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.UpdateTaskTrace(ctx, task.ID, storedTraceJSON); err != nil {
		t.Fatalf("UpdateTaskTrace: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/trace", nil)
	w := httptest.NewRecorder()
	h.TaskTrace(w, req, task.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp taskTraceResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("nodes = %+v, want 2", resp.Nodes)
	}
	if resp.Nodes[0].ID != "run-x/planner" || resp.Nodes[0].Name != "planner" ||
		resp.Nodes[0].Role != "Planner" || resp.Nodes[0].Status != "done" {
		t.Errorf("node[0] = %+v, want planner/Planner/done", resp.Nodes[0])
	}
	if len(resp.Nodes[0].Grants) != 1 || resp.Nodes[0].Grants[0] != "read" {
		t.Errorf("node[0] grants = %v, want [read]", resp.Nodes[0].Grants)
	}
	if resp.Nodes[1].Status != "running" {
		t.Errorf("node[1] status = %q, want running", resp.Nodes[1].Status)
	}
	if len(resp.Edges) != 1 || resp.Edges[0].From != "run-x/planner" ||
		resp.Edges[0].To != "run-x/builder" || resp.Edges[0].Kind != "next" {
		t.Errorf("edges = %+v, want one planner->builder next edge", resp.Edges)
	}
}

// TestTaskTrace_NoTrace verifies a non-agentic task (nil Trace) returns
// 200 with empty, non-nil nodes and edges arrays.
func TestTaskTrace_NoTrace(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "plain task", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/trace", nil)
	w := httptest.NewRecorder()
	h.TaskTrace(w, req, task.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The JSON must carry empty arrays, not null, so the client renders nothing
	// without nil handling.
	if got := w.Body.String(); got != "{\"nodes\":[],\"edges\":[]}\n" && got != "{\"nodes\":[],\"edges\":[]}" {
		t.Errorf("body = %q, want empty nodes/edges arrays", got)
	}

	var resp taskTraceResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 0 || len(resp.Edges) != 0 {
		t.Errorf("resp = %+v, want empty nodes and edges", resp)
	}
}
