package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/graph"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/workspace"
)

func getGraph(t *testing.T, h *Handler, req *http.Request) graph.Graph {
	t.Helper()
	w := httptest.NewRecorder()
	h.GetGraph(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var g graph.Graph
	if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode: %v; body: %s", err, w.Body.String())
	}
	return g
}

func nodeID(g graph.Graph, id string) bool {
	for _, n := range g.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

// TestGetGraph_Shape proves the endpoint composes the spec tree and task list
// into a unified graph: a dispatched spec and its task both appear, joined by a
// dispatch edge.
func TestGetGraph_Shape(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	taskID := seedTask(t, h.store, store.TaskStatusBacklog)
	writeDispatchedSpec(t, ws, "specs/local/dispatched.md", taskID)

	g := getGraph(t, h, httptest.NewRequest(http.MethodGet, "/api/graph", nil))

	if !nodeID(g, graph.SpecID("specs/local/dispatched.md")) {
		t.Errorf("spec node missing; nodes=%+v", g.Nodes)
	}
	if !nodeID(g, graph.TaskID(taskID)) {
		t.Errorf("task node missing; nodes=%+v", g.Nodes)
	}
	want := graph.Edge{
		From: graph.SpecID("specs/local/dispatched.md"),
		To:   graph.TaskID(taskID),
		Kind: graph.EdgeDispatch,
	}
	found := false
	for _, e := range g.Edges {
		if e == want {
			found = true
		}
	}
	if !found {
		t.Errorf("dispatch edge missing; edges=%+v", g.Edges)
	}
	// A ready backlog task offers the start action.
	for _, n := range g.Nodes {
		if n.ID == graph.TaskID(taskID) {
			if len(n.AvailableActions) != 1 || n.AvailableActions[0] != graph.ActionStart {
				t.Errorf("task actions = %v, want [start]", n.AvailableActions)
			}
		}
	}
}

// TestGetGraph_SpecDepEdgeFromRealTree proves spec_dep edges resolve against
// the real BuildTree path format — i.e. the NodeResponse.Path that the builder
// keys on equals the frontmatter depends_on string a user writes. A format
// mismatch here would silently drop every spec_dep edge in production while
// the unit fixtures (which pick the path format themselves) stay green.
func TestGetGraph_SpecDepEdgeFromRealTree(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	writeTestSpec(t, ws, "specs/local/base.md", testSpecValidated)
	dependent := strings.Replace(testSpecValidated, "depends_on: []",
		"depends_on:\n  - specs/local/base.md", 1)
	writeTestSpec(t, ws, "specs/local/dependent.md", dependent)

	g := getGraph(t, h, httptest.NewRequest(http.MethodGet, "/api/graph", nil))

	want := graph.Edge{
		From: graph.SpecID("specs/local/base.md"),
		To:   graph.SpecID("specs/local/dependent.md"),
		Kind: graph.EdgeSpecDep,
	}
	found := false
	for _, e := range g.Edges {
		if e == want {
			found = true
		}
	}
	if !found {
		t.Errorf("spec_dep edge missing; edges=%+v", g.Edges)
	}
}

// TestGetGraph_HiddenForMismatchedPrincipal mirrors the spec-tree leak guard
// (config_test.go:332): a caller who cannot see the org-stamped workspace gets
// no spec nodes, even though specs exist on disk.
func TestGetGraph_HiddenForMismatchedPrincipal(t *testing.T) {
	h, _, ws := newTestHandlerWithRealWorkspaceManager(t)
	h.SetCloudMode(true)
	writeTestSpec(t, ws, "specs/local/secret.md", testSpecValidated)
	if err := workspace.SaveGroups(h.configDir, []workspace.Group{
		{Workspaces: []string{ws}, CreatedBy: "owner", OrgID: "org-a"},
	}); err != nil {
		t.Fatal(err)
	}

	// Local caller (no claims) sees the spec node.
	local := getGraph(t, h, httptest.NewRequest(http.MethodGet, "/api/graph", nil))
	if !nodeID(local, graph.SpecID("specs/local/secret.md")) {
		t.Fatalf("local caller should see spec node; nodes=%+v", local.Nodes)
	}

	// Personal caller (mismatched org) sees an empty graph.
	req := httptest.NewRequest(http.MethodGet, "/api/graph", nil)
	req = req.WithContext(auth.WithIdentity(context.Background(), &auth.Identity{Sub: "u", OrgID: ""}))
	personal := getGraph(t, h, req)
	for _, n := range personal.Nodes {
		if n.Kind == graph.NodeSpec {
			t.Errorf("mismatched principal should see no spec nodes, got %s", n.ID)
		}
	}
}

// TestGetGraph_ArchivedToggle confirms the archived query param flows through
// to the builder for tasks.
func TestGetGraph_ArchivedToggle(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	taskID := seedTask(t, h.store, store.TaskStatusDone)
	if err := h.store.SetTaskArchived(context.Background(), uuid.MustParse(taskID), true); err != nil {
		t.Fatalf("archive task: %v", err)
	}

	def := getGraph(t, h, httptest.NewRequest(http.MethodGet, "/api/graph", nil))
	if nodeID(def, graph.TaskID(taskID)) {
		t.Error("archived task should be hidden by default")
	}
	inc := getGraph(t, h, httptest.NewRequest(http.MethodGet, "/api/graph?archived=1", nil))
	if !nodeID(inc, graph.TaskID(taskID)) {
		t.Error("archived task should appear with ?archived=1")
	}
}
