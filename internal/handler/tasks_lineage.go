package handler

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/pkg/httpjson"
)

// lineageNode is one agent in an agentic run's lineage graph. It is the thin,
// frontend-facing projection of the topos lineage node persisted opaquely on the
// task (see internal/agentgraph and Task.Lineage). Status is "running", "done",
// or "failed".
type lineageNode struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Role    string   `json:"role"`
	Status  string   `json:"status"`
	Grants  []string `json:"grants,omitempty"`
	Sandbox string   `json:"sandbox,omitempty"`
}

// lineageEdge is one handoff between agents. Kind is "delegate", "deliver", or
// "next".
type lineageEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// taskLineageResp is the GET /api/tasks/{id}/lineage body: the agent-graph of a
// single agentic run. Nodes and edges are always non-nil so the frontend can
// render an empty graph without nil checks; a task with no lineage yields both
// empty.
type taskLineageResp struct {
	Nodes []lineageNode `json:"nodes"`
	Edges []lineageEdge `json:"edges"`
}

// TaskLineage returns the lineage sub-graph of an agentic-flow run for a task.
// The stored lineage is an opaque JSON string written by the runner from the
// topos result (capitalised keys, no json tags); this handler reparses it into
// the thin lowercase-keyed shape the UI consumes. A task with no lineage (every
// non-agentic task, or one whose run has not produced a graph yet) returns
// empty nodes and edges with 200, so the client renders nothing without special
// casing. json.Unmarshal matches keys case-insensitively, so the capitalised
// stored keys bind to the lowercase-tagged fields directly.
func (h *Handler) TaskLineage(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	resp := taskLineageResp{Nodes: []lineageNode{}, Edges: []lineageEdge{}}
	if task.Lineage != nil && *task.Lineage != "" {
		if err := json.Unmarshal([]byte(*task.Lineage), &resp); err != nil {
			http.Error(w, "lineage parse error", http.StatusInternalServerError)
			return
		}
		if resp.Nodes == nil {
			resp.Nodes = []lineageNode{}
		}
		if resp.Edges == nil {
			resp.Edges = []lineageEdge{}
		}
	}
	httpjson.Write(w, http.StatusOK, resp)
}
