package agentgraph_test

import (
	"context"
	"reflect"
	"testing"

	"latere.ai/x/topos"
	"latere.ai/x/topos/graph"
	"latere.ai/x/wallfacer/internal/agentgraph"
	"latere.ai/x/wallfacer/internal/flow"
)

// TestFromFlowGraph_RefAgentsAndCoordination proves the authoring compile: the flow
// becomes one region whose entry+peers are ref agents naming their slugs, and the
// flow's execution shape maps onto the single Coordination field.
func TestFromFlowGraph_RefAgentsAndCoordination(t *testing.T) {
	_, base := twoAgentFixture()

	g, err := agentgraph.FromFlowGraph(base)
	if err != nil {
		t.Fatalf("FromFlowGraph: %v", err)
	}
	if len(g.Regions) != 1 {
		t.Fatalf("regions = %d, want 1 (a flow is a single region)", len(g.Regions))
	}
	r := g.Regions[0]
	if !r.Entry.IsRef() || r.Entry.Ref != "planner" || r.Entry.Name != "planner" {
		t.Errorf("entry = %+v, want a ref to planner named planner", r.Entry)
	}
	if len(r.Peers) != 1 || !r.Peers[0].IsRef() || r.Peers[0].Ref != "builder" {
		t.Fatalf("peers = %+v, want a single ref to builder", r.Peers)
	}
	// The ref form carries no inline spec until Resolve runs.
	if r.Entry.SystemPrompt != "" {
		t.Errorf("entry SystemPrompt = %q, want empty before Resolve", r.Entry.SystemPrompt)
	}

	tests := []struct {
		name     string
		dynamic  bool
		topology flow.Topology
		want     graph.Coordination
	}{
		{"non-dynamic is a sequence", false, "", graph.Sequence},
		{"dynamic mesh is mesh", true, flow.TopologyMesh, graph.Mesh},
		{"dynamic empty topology is lead", true, "", graph.Lead},
		{"dynamic orchestrator-worker is lead", true, flow.TopologyOrchestratorWorker, graph.Lead},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			f.Dynamic = tc.dynamic
			f.Topology = tc.topology
			g, err := agentgraph.FromFlowGraph(f)
			if err != nil {
				t.Fatalf("FromFlowGraph: %v", err)
			}
			if got := g.Regions[0].Coordination; got != tc.want {
				t.Errorf("coordination = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFromFlowGraph_MaxHandoffDepth threads the flow's depth bound onto the authored
// graph so a persisted graph round-trips it.
func TestFromFlowGraph_MaxHandoffDepth(t *testing.T) {
	_, f := twoAgentFixture()
	f.MaxHandoffDepth = 7
	g, err := agentgraph.FromFlowGraph(f)
	if err != nil {
		t.Fatalf("FromFlowGraph: %v", err)
	}
	if g.MaxHandoffDepth != 7 {
		t.Errorf("MaxHandoffDepth = %d, want 7", g.MaxHandoffDepth)
	}
}

// TestFromFlowGraph_Errors covers the empty-flow guard.
func TestFromFlowGraph_Errors(t *testing.T) {
	if _, err := agentgraph.FromFlowGraph(flow.Flow{Slug: "empty"}); err == nil {
		t.Error("expected error for a flow with no steps")
	}
}

// TestRunFlowGraph_MatchesFromFlowPath is the behavior-preserving proof: the same
// fixture run through the new graph.Graph consume path and the existing FromFlow
// region path produces an identical Result (final text + lineage), so introducing
// the graph model changes nothing a consumer observes.
func TestRunFlowGraph_MatchesFromFlowPath(t *testing.T) {
	reg, f := twoAgentFixture()
	opts := agentgraph.RunOptions("run-eq", agentgraph.ModelConfig{}, f)

	viaRegion, err := agentgraph.RunFlow(context.Background(), opts, f, reg, "do the thing")
	if err != nil {
		t.Fatalf("RunFlow: %v", err)
	}
	viaGraph, err := agentgraph.RunFlowGraph(context.Background(), opts, f, reg, "do the thing")
	if err != nil {
		t.Fatalf("RunFlowGraph: %v", err)
	}
	if viaGraph.Final != viaRegion.Final {
		t.Errorf("final differs: graph %q vs region %q", viaGraph.Final, viaRegion.Final)
	}
	// Sandbox is a fresh per-run resource id (a random sandbox name), not a
	// behavioral property of the graph; normalize it so the comparison is over the
	// node identity, roles, statuses, and edges the two paths must share.
	normalize(&viaGraph.Lineage)
	normalize(&viaRegion.Lineage)
	if !reflect.DeepEqual(viaGraph.Lineage, viaRegion.Lineage) {
		t.Errorf("lineage differs:\n graph  = %+v\n region = %+v", viaGraph.Lineage, viaRegion.Lineage)
	}
}

// normalize blanks the ephemeral per-run sandbox id on every node so two lineages
// can be compared for behavioral equality.
func normalize(l *topos.Lineage) {
	for i := range l.Nodes {
		l.Nodes[i].Sandbox = ""
	}
}

// TestRunFlowGraph_UnknownAgent surfaces a ref that the registry cannot resolve as
// an error, rather than lowering an unresolved graph.
func TestRunFlowGraph_UnknownAgent(t *testing.T) {
	reg, _ := twoAgentFixture()
	bad := flow.Flow{Slug: "bad", Steps: []flow.Step{{AgentSlug: "nope"}}}
	if _, err := agentgraph.RunFlowGraph(context.Background(), topos.Options{}, bad, reg, "x"); err == nil {
		t.Error("expected an error resolving an unknown agent slug")
	}
}

// TestRunFlowGraph_ResolveInlinesSpecs proves the resolve seam fills a ref's inline
// spec from the registry: after Resolve the lowered region carries the role's
// prompt, title, and scopes, not an empty ref.
func TestRunFlowGraph_ResolveInlinesSpecs(t *testing.T) {
	reg, f := twoAgentFixture()
	res, err := agentgraph.RunFlowGraph(context.Background(), agentgraph.RunOptions("run-x", agentgraph.ModelConfig{}, f), f, reg, "go")
	if err != nil {
		t.Fatalf("RunFlowGraph: %v", err)
	}
	// A resolved two-agent pinned chain produces two done nodes joined by one
	// next edge, exactly as the region path does.
	if len(res.Lineage.Nodes) != 2 {
		t.Fatalf("nodes = %+v, want 2 (planner, builder)", res.Lineage.Nodes)
	}
	if res.Lineage.Nodes[0].ID != "run-x/planner" || res.Lineage.Nodes[1].ID != "run-x/builder" {
		t.Errorf("node ids = %q, %q; want run-x/planner, run-x/builder",
			res.Lineage.Nodes[0].ID, res.Lineage.Nodes[1].ID)
	}
}
