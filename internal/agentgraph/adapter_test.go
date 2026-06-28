package agentgraph_test

import (
	"context"
	"testing"

	"latere.ai/x/wallfacer/internal/agentgraph"
	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/flow"
)

// twoAgentFixture builds a registry + a two-step agentic flow used across the
// adapter tests.
func twoAgentFixture() (*agents.Registry, flow.Flow) {
	reg := agents.NewRegistry(
		agents.Role{Slug: "planner", Title: "Planner", Description: "plans the work", PromptTmpl: "you plan", Capabilities: []string{"workspace.read"}},
		agents.Role{Slug: "builder", Title: "Builder", Description: "builds the work", PromptTmpl: "you build", Capabilities: []string{"workspace.write"}},
	)
	f := flow.Flow{
		Slug:    "agentic-pair",
		Name:    "Agentic Pair",
		Agentic: true,
		Steps:   []flow.Step{{AgentSlug: "planner"}, {AgentSlug: "builder"}},
	}
	return reg, f
}

func TestFromFlow_EntryAndPeers(t *testing.T) {
	reg, f := twoAgentFixture()
	region, err := agentgraph.FromFlow(f, reg)
	if err != nil {
		t.Fatalf("FromFlow: %v", err)
	}
	if region.Entry.Name != "planner" {
		t.Errorf("entry = %q, want planner", region.Entry.Name)
	}
	if region.Entry.SystemPrompt != "you plan" || region.Entry.Role != "Planner" {
		t.Errorf("entry spec = %+v, want prompt/role mapped from the role", region.Entry)
	}
	if len(region.Peers) != 1 || region.Peers[0].Name != "builder" {
		t.Fatalf("peers = %+v, want [builder]", region.Peers)
	}
	if len(region.Peers[0].Scopes) != 1 || region.Peers[0].Scopes[0] != "workspace.write" {
		t.Errorf("peer scopes = %v, want capabilities mapped to scopes", region.Peers[0].Scopes)
	}
}

func TestFromFlow_Errors(t *testing.T) {
	reg, _ := twoAgentFixture()
	if _, err := agentgraph.FromFlow(flow.Flow{Slug: "empty"}, reg); err == nil {
		t.Error("expected error for a flow with no steps")
	}
	bad := flow.Flow{Slug: "bad", Steps: []flow.Step{{AgentSlug: "nope"}}}
	if _, err := agentgraph.FromFlow(bad, reg); err == nil {
		t.Error("expected error for an unknown agent slug")
	}
}

// TestRunFlowFake exercises the full headless path with the deterministic fake
// model: a two-agent pinned chain produces a lineage with two nodes joined by a
// single "next" edge, and a non-empty final text.
func TestRunFlowFake(t *testing.T) {
	reg, f := twoAgentFixture()
	res, err := agentgraph.RunFlowFake(context.Background(), "run-x", f, reg, "do the thing")
	if err != nil {
		t.Fatalf("RunFlowFake: %v", err)
	}
	if res.Final == "" {
		t.Error("final text is empty")
	}
	if len(res.Lineage.Nodes) != 2 {
		t.Fatalf("nodes = %+v, want 2", res.Lineage.Nodes)
	}
	if res.Lineage.Nodes[0].ID != "run-x/planner" || res.Lineage.Nodes[1].ID != "run-x/builder" {
		t.Errorf("node ids = %q, %q; want run-x/planner, run-x/builder",
			res.Lineage.Nodes[0].ID, res.Lineage.Nodes[1].ID)
	}
	for _, n := range res.Lineage.Nodes {
		if n.Status != "done" {
			t.Errorf("node %s status = %q, want done", n.ID, n.Status)
		}
	}
	if len(res.Lineage.Edges) != 1 || res.Lineage.Edges[0].Kind != "next" {
		t.Fatalf("edges = %+v, want one next edge", res.Lineage.Edges)
	}
	if res.Lineage.Edges[0].From != "run-x/planner" || res.Lineage.Edges[0].To != "run-x/builder" {
		t.Errorf("edge = %+v, want planner -> builder", res.Lineage.Edges[0])
	}
}
