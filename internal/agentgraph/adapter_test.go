package agentgraph_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/topos"
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
	// A non-dynamic flow stays the deterministic pinned chain.
	if region.Autonomy != topos.Pinned {
		t.Errorf("autonomy = %q, want pinned", region.Autonomy)
	}
}

// TestFromFlow_DynamicTopology covers the M3 dynamic/mesh mapping: a dynamic
// flow yields Autonomy: Dynamic with the topology resolved from the flow
// (mesh stays mesh; empty materializes the orchestrator-worker default), while
// a non-dynamic flow stays Pinned. The test names topos types because the
// agentgraph seam is the place that may.
func TestFromFlow_DynamicTopology(t *testing.T) {
	reg, base := twoAgentFixture()

	tests := []struct {
		name     string
		dynamic  bool
		topology flow.Topology
		wantAuto topos.Autonomy
		wantTopo topos.Topology
	}{
		{"dynamic mesh", true, flow.TopologyMesh, topos.Dynamic, topos.Mesh},
		{"dynamic explicit orchestrator-worker", true, flow.TopologyOrchestratorWorker, topos.Dynamic, topos.OrchestratorWorker},
		{"dynamic empty topology defaults to orchestrator-worker", true, "", topos.Dynamic, topos.OrchestratorWorker},
		{"non-dynamic stays pinned", false, flow.TopologyMesh, topos.Pinned, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			f.Dynamic = tc.dynamic
			f.Topology = tc.topology
			region, err := agentgraph.FromFlow(f, reg)
			if err != nil {
				t.Fatalf("FromFlow: %v", err)
			}
			if region.Autonomy != tc.wantAuto {
				t.Errorf("autonomy = %q, want %q", region.Autonomy, tc.wantAuto)
			}
			if region.Topology != tc.wantTopo {
				t.Errorf("topology = %q, want %q", region.Topology, tc.wantTopo)
			}
			// Entry/peers shape is unchanged across autonomy modes.
			if region.Entry.Name != "planner" || len(region.Peers) != 1 || region.Peers[0].Name != "builder" {
				t.Errorf("region shape = entry %q peers %+v, want planner + [builder]", region.Entry.Name, region.Peers)
			}
		})
	}
}

// TestRunOptions_MaxHandoffDepth asserts the flow's MaxHandoffDepth is threaded
// into the built topos.Options, and a zero flow depth passes 0 (so topos applies
// its own default). The mapping is read structurally; only the seam names topos.
func TestRunOptions_MaxHandoffDepth(t *testing.T) {
	_, f := twoAgentFixture()
	f.MaxHandoffDepth = 5
	opts := agentgraph.RunOptions("run-x", agentgraph.ModelConfig{}, f)
	if opts.MaxHandoffDepth != 5 {
		t.Errorf("MaxHandoffDepth = %d, want 5", opts.MaxHandoffDepth)
	}
	if opts.SessionID != "run-x" {
		t.Errorf("SessionID = %q, want run-x", opts.SessionID)
	}

	f.MaxHandoffDepth = 0
	if got := agentgraph.RunOptions("run-x", agentgraph.ModelConfig{}, f).MaxHandoffDepth; got != 0 {
		t.Errorf("zero-depth flow MaxHandoffDepth = %d, want 0 (topos default applies)", got)
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

// TestRunFlowWithModel_ObserverReceivesEvents proves the observer seam: a run
// delivers live trace events (lifecycle, per-turn assistant text, tool use) to
// the host callback, and each event's Node joins to a lineage node.
func TestRunFlowWithModel_ObserverReceivesEvents(t *testing.T) {
	reg, f := twoAgentFixture()
	var got []agentgraph.TraceEvent
	res, err := agentgraph.RunFlowWithModel(
		context.Background(), "run-obs", agentgraph.ModelConfig{}, f, reg, "do the thing",
		func(ev agentgraph.TraceEvent) { got = append(got, ev) },
	)
	if err != nil {
		t.Fatalf("RunFlowWithModel: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("observer received no events")
	}

	names := map[string]bool{}
	nodes := map[string]bool{}
	for _, ev := range got {
		names[ev.Name] = true
		nodes[ev.Node] = true
	}
	for _, want := range []string{"SessionStart", "AssistantMessage", "PostToolUse", "SessionEnd"} {
		if !names[want] {
			t.Errorf("missing event %q; got %v", want, names)
		}
	}
	// Every event's Node must be a real lineage node id (the join key).
	lineageIDs := map[string]bool{}
	for _, n := range res.Lineage.Nodes {
		lineageIDs[n.ID] = true
	}
	for n := range nodes {
		if n != "" && !lineageIDs[n] {
			t.Errorf("event Node %q is not a lineage node id %v", n, res.Lineage.Nodes)
		}
	}
}

// TestRunAgent_SingleNode exercises the native-harness entry point: a single
// agent runs as a one-node pinned region with the deterministic fake model,
// producing a non-empty final text, exactly one lineage node (<session>/<name>,
// status done), no edges, and live observer events that join to that node.
func TestRunAgent_SingleNode(t *testing.T) {
	var got []agentgraph.TraceEvent
	res, err := agentgraph.RunAgent(
		context.Background(), "run-native", agentgraph.ModelConfig{}, "implement", "you implement", "do the thing", "",
		func(ev agentgraph.TraceEvent) { got = append(got, ev) },
	)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res.Final == "" {
		t.Error("final text is empty")
	}
	if len(res.Lineage.Nodes) != 1 {
		t.Fatalf("nodes = %+v, want exactly 1", res.Lineage.Nodes)
	}
	n := res.Lineage.Nodes[0]
	if n.ID != "run-native/implement" {
		t.Errorf("node id = %q, want run-native/implement", n.ID)
	}
	if n.Status != "done" {
		t.Errorf("node status = %q, want done", n.Status)
	}
	if len(res.Lineage.Edges) != 0 {
		t.Errorf("edges = %+v, want none (single agent, no delegation)", res.Lineage.Edges)
	}

	names := map[string]bool{}
	for _, ev := range got {
		names[ev.Name] = true
		if ev.Node != "" && ev.Node != n.ID {
			t.Errorf("event Node %q is not the single lineage node %q", ev.Node, n.ID)
		}
	}
	for _, want := range []string{"SessionStart", "AssistantMessage", "SessionEnd"} {
		if !names[want] {
			t.Errorf("missing event %q; got %v", want, names)
		}
	}
}

// TestRunAgent_WithWorktreeExecutesInRepo proves end-to-end worktree execution:
// with a worktree set, the run's tools execute in that directory. The fake model
// runs `echo <prompt>` in the sandbox, so a prompt that redirects to a file lands
// that file in the worktree — demonstrating the native harness edits the real repo
// (via topos Options.Workdir).
func TestRunAgent_WithWorktreeExecutesInRepo(t *testing.T) {
	worktree := t.TempDir()
	_, err := agentgraph.RunAgent(
		context.Background(), "run-wt", agentgraph.ModelConfig{}, "implement", "", "hi > marker.txt", worktree, nil,
	)
	if err != nil {
		t.Fatalf("RunAgent with worktree: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(worktree, "marker.txt"))
	if err != nil {
		t.Fatalf("marker.txt not created in the worktree: %v", err)
	}
	if !strings.Contains(string(got), "hi") {
		t.Errorf("marker.txt = %q, want it to contain the echoed prompt", got)
	}
}

// TestRunAgent_DefaultName falls back to a stable node name when none is given,
// and works with a nil observer.
func TestRunAgent_DefaultName(t *testing.T) {
	res, err := agentgraph.RunAgent(context.Background(), "run-x", agentgraph.ModelConfig{}, "", "", "hi", "", nil)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if len(res.Lineage.Nodes) != 1 || res.Lineage.Nodes[0].ID != "run-x/agent" {
		t.Fatalf("nodes = %+v, want one node run-x/agent", res.Lineage.Nodes)
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
