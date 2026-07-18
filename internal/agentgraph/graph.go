package agentgraph

import (
	"context"
	"fmt"

	"latere.ai/x/topos"
	"latere.ai/x/topos/graph"
	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/flow"
)

// FromFlowGraph compiles a wallfacer flow into the canonical authored graph
// ([latere.ai/x/topos/graph.Graph]) in its agent-reference form: the flow becomes a
// single region whose entry is the first step and whose peers are the rest, each
// step contributed as a ref Agent naming its AgentSlug. No registry is read here,
// so the returned graph still holds refs; a consumer swaps them for inline specs
// via [graph.Graph.Resolve] (see registryResolver) before running.
//
// The flow's coordination (pinned chain, orchestrator-worker, or mesh) maps onto
// the graph's single first-class Coordination field: a non-dynamic flow is a
// sequence, a dynamic flow is lead or mesh per its topology. MaxHandoffDepth rides
// on the authored graph so it round-trips; ToRuntime does not consume it (the
// caller routes it into topos.Options.MaxHandoffDepth via runOptions).
func FromFlowGraph(f flow.Flow) (graph.Graph, error) {
	if len(f.Steps) == 0 {
		return graph.Graph{}, fmt.Errorf("agentgraph: flow %q has no steps", f.Slug)
	}
	refs := make([]graph.Agent, 0, len(f.Steps))
	for _, step := range f.Steps {
		// Name mirrors the ref slug so the in-graph identity (and lineage node id)
		// stays <session>/<slug>, matching the FromFlow region path. Resolve
		// preserves this authored Name.
		refs = append(refs, graph.Agent{Ref: step.AgentSlug, Name: step.AgentSlug})
	}
	id := f.Slug
	if id == "" {
		id = "flow"
	}
	return graph.Graph{
		Regions: []graph.Region{{
			ID:           id,
			Coordination: coordination(f),
			Entry:        refs[0],
			Peers:        refs[1:],
		}},
		MaxHandoffDepth: f.MaxHandoffDepth,
	}, nil
}

// coordination maps a wallfacer flow's execution shape onto the authored graph's
// single Coordination field. A non-dynamic flow is a deterministic sequence; a
// dynamic flow is mesh when its topology is mesh, else lead (orchestrator-worker,
// the default). It is the authoring-model twin of the FromFlow autonomy switch.
func coordination(f flow.Flow) graph.Coordination {
	if !f.Dynamic {
		return graph.Sequence
	}
	if f.Topology == flow.TopologyMesh {
		return graph.Mesh
	}
	return graph.Lead
}

// registryResolver is the [graph.Graph.Resolve] hook backed by wallfacer's agents
// registry: it swaps a ref slug for the inline agent spec the registry holds
// (the same slug->AgentSpec mapping FromFlow performs). topos-lib never reads a
// registry, so this consumer-side resolver is the only place a ref becomes inline.
func registryResolver(reg *agents.Registry) func(ref string) (graph.Agent, error) {
	return func(ref string) (graph.Agent, error) {
		role, ok := reg.Get(ref)
		if !ok {
			return graph.Agent{}, fmt.Errorf("agentgraph: flow references unknown agent %q", ref)
		}
		return graph.Agent{
			Name:         role.Slug,
			Role:         role.Title,
			Description:  role.Description,
			SystemPrompt: role.PromptTmpl,
			Scopes:       role.Capabilities,
		}, nil
	}
}

// RunFlowGraph runs a flow through the canonical graph model: it compiles the flow
// to an authored graph, resolves its ref agents against reg, lowers it to the
// runtime graph, and runs it. A wallfacer flow is a single region, so it executes
// via Runner.Run on that region (not RunGraph): RunGraph namespaces node ids
// <session>/<regionID>/<agent> for multi-region graphs, whereas the single-region
// Run keeps them <session>/<agent>, preserving the lineage shape the FromFlow path
// produces.
func RunFlowGraph(ctx context.Context, opts topos.Options, f flow.Flow, reg *agents.Registry, prompt string) (topos.RunResult, error) {
	authored, err := FromFlowGraph(f)
	if err != nil {
		return topos.RunResult{}, err
	}
	resolved, err := authored.Resolve(registryResolver(reg))
	if err != nil {
		return topos.RunResult{}, err
	}
	rt, err := resolved.ToRuntime()
	if err != nil {
		return topos.RunResult{}, err
	}
	runner, err := NewRunner(opts)
	if err != nil {
		return topos.RunResult{}, err
	}
	return runner.Run(ctx, rt.Regions[0].Region, prompt)
}
