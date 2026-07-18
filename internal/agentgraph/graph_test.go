package agentgraph_test

import (
	"testing"

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

// TestFromFlowGraph_ResolveInlinesSpecs proves the resolve seam fills a ref's inline
// spec from the registry: after FromFlowGraph produces a ref graph, resolving it
// against the registry replaces each ref with the role's prompt, title, and scopes,
// and the result no longer holds a ref.
func TestFromFlowGraph_ResolveInlinesSpecs(t *testing.T) {
	reg, f := twoAgentFixture()
	region, err := agentgraph.FromFlow(f, reg)
	if err != nil {
		t.Fatalf("FromFlow: %v", err)
	}
	if region.Entry.Name != "planner" || region.Entry.SystemPrompt != "you plan" || region.Entry.Role != "Planner" {
		t.Errorf("entry = %+v, want planner inlined with prompt/role from the registry", region.Entry)
	}
	if len(region.Peers) != 1 || region.Peers[0].Name != "builder" || len(region.Peers[0].Scopes) != 1 || region.Peers[0].Scopes[0] != "workspace.write" {
		t.Errorf("peers = %+v, want builder inlined with its scopes", region.Peers)
	}
}
