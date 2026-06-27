package handler

import (
	"net/http"

	"latere.ai/x/wallfacer/internal/graph"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
)

// GetGraph returns the unified spec+task dependency graph the Map renders and
// drives. It composes the same principal-scoped sources the spec tree and task
// list already use — collectSpecTree (workspace-visibility scoped) and
// TasksForPrincipal (org scoped in cloud mode) — then defers all node/edge
// derivation to the pure internal/graph builder.
//
// Query param: archived=1 (or true) includes archived specs and tasks,
// matching the Map's "Show archived" toggle. Default excludes them.
func (h *Handler) GetGraph(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	archived := r.URL.Query().Get("archived")
	includeArchived := archived == "1" || archived == "true"

	tree := h.collectSpecTree(r.Context())
	tasks := s.TasksForPrincipal(r.Context(), principalFromRequest(r), includeArchived)

	g := graph.Build(tree.Nodes, tasks, includeArchived)
	httpjson.Write(w, http.StatusOK, g)
}
