package handler

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"latere.ai/x/pkg/httpjson"
)

// traceNode is one agent in an agentic run's trace graph. It is the thin,
// frontend-facing projection of the topos trace node persisted opaquely on the
// task (see internal/agentgraph and Task.Trace). Status is "running", "done",
// or "failed".
type traceNode struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Role    string   `json:"role"`
	Status  string   `json:"status"`
	Grants  []string `json:"grants,omitempty"`
	Sandbox string   `json:"sandbox,omitempty"`
}

// traceEdge is one handoff between agents. Kind is "delegate", "deliver", or
// "next".
type traceEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// taskTraceResp is the GET /api/tasks/{id}/trace body: the agent-graph of a
// single agentic run. Nodes and edges are always non-nil so the frontend can
// render an empty graph without nil checks; a task with no trace yields both
// empty.
type taskTraceResp struct {
	Nodes []traceNode `json:"nodes"`
	Edges []traceEdge `json:"edges"`
}

// TaskTrace returns the trace sub-graph of an agentic-flow run for a task.
// The stored trace is an opaque JSON string written by the runner from the
// topos result (capitalised keys, no json tags); this handler reparses it into
// the thin lowercase-keyed shape the UI consumes. A task with no trace (every
// non-agentic task, or one whose run has not produced a graph yet) returns
// empty nodes and edges with 200, so the client renders nothing without special
// casing. json.Unmarshal matches keys case-insensitively, so the capitalised
// stored keys bind to the lowercase-tagged fields directly.
func (h *Handler) TaskTrace(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	resp := taskTraceResp{Nodes: []traceNode{}, Edges: []traceEdge{}}
	if task.Trace != nil && *task.Trace != "" {
		if err := json.Unmarshal([]byte(*task.Trace), &resp); err != nil {
			http.Error(w, "trace parse error", http.StatusInternalServerError)
			return
		}
		if resp.Nodes == nil {
			resp.Nodes = []traceNode{}
		}
		if resp.Edges == nil {
			resp.Edges = []traceEdge{}
		}
	}
	httpjson.Write(w, http.StatusOK, resp)
}
