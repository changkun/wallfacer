package agentgraph

import (
	"context"
	"fmt"

	"latere.ai/x/topos"
	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/flow"
)

// FromFlow compiles a wallfacer flow + agents registry into a topos.Region.
//
// The flow's first step becomes the region entry; the remaining steps become
// the ordered peer chain. Each step's AgentSlug is resolved through reg into an
// agents.Role, mapped onto a topos.AgentSpec: the slug is the stable identity
// (so lineage node ids are <session>/<slug>), Title/Description carry the role
// labels, PromptTmpl becomes the system prompt, and Capabilities become the
// permission scopes.
//
// M2 scope is a deterministic chain, so the region is Pinned and a step's
// Optional / RunInParallelWith hints are ignored (the dynamic/mesh mapping is a
// later milestone). Built-in roles leave PromptTmpl empty (they render through
// the prompts package); FromFlow tolerates that — an empty system prompt is
// legal for the fake model and the M2 headless path.
func FromFlow(f flow.Flow, reg *agents.Registry) (topos.Region, error) {
	if len(f.Steps) == 0 {
		return topos.Region{}, fmt.Errorf("agentgraph: flow %q has no steps", f.Slug)
	}
	specs := make([]topos.AgentSpec, 0, len(f.Steps))
	for _, step := range f.Steps {
		role, ok := reg.Get(step.AgentSlug)
		if !ok {
			return topos.Region{}, fmt.Errorf("agentgraph: flow %q references unknown agent %q", f.Slug, step.AgentSlug)
		}
		specs = append(specs, topos.AgentSpec{
			Name:         role.Slug,
			Role:         role.Title,
			Description:  role.Description,
			SystemPrompt: role.PromptTmpl,
			Scopes:       role.Capabilities,
		})
	}
	return topos.Region{
		Autonomy: topos.Pinned,
		Entry:    specs[0],
		Peers:    specs[1:],
	}, nil
}

// RunFlow builds a topos runner from opts and runs the region compiled from the
// flow against prompt, returning the run result (final text + lineage graph).
func RunFlow(ctx context.Context, opts topos.Options, f flow.Flow, reg *agents.Registry, prompt string) (topos.RunResult, error) {
	region, err := FromFlow(f, reg)
	if err != nil {
		return topos.RunResult{}, err
	}
	runner, err := NewRunner(opts)
	if err != nil {
		return topos.RunResult{}, err
	}
	return runner.Run(ctx, region, prompt)
}

// Result is the host-facing outcome of an agent-graph run. It mirrors
// topos.RunResult with topos-free types so a wallfacer package (e.g. the runner)
// can consume a run without importing topos and crossing the seam.
type Result struct {
	Final   string
	Lineage Lineage
}

// Lineage is the topos-free mirror of topos.Lineage: the renderable run graph of
// nodes (agents) and edges (delegate / deliver / next). It marshals to the same
// JSON shape, so a host can persist it opaquely and a consumer can unmarshal it.
type Lineage struct {
	Nodes []Node
	Edges []Edge
}

// Node mirrors topos.LineageNode.
type Node struct {
	ID      string
	Name    string
	Role    string
	Status  string
	Grants  []string
	Sandbox string
}

// Edge mirrors topos.LineageEdge (Kind is "delegate", "deliver", or "next").
type Edge struct {
	From string
	To   string
	Kind string
}

// RunFlowFake runs a flow through the agent-graph runtime with the deterministic,
// network-free fake model, returning a topos-free Result. sessionID seeds the
// run id so lineage node ids (<session>/<agent>) are stable. It is the explicit
// fake entrypoint, equivalent to RunFlowWithModel with an unconfigured config.
func RunFlowFake(ctx context.Context, sessionID string, f flow.Flow, reg *agents.Registry, prompt string) (Result, error) {
	return RunFlowWithModel(ctx, sessionID, ModelConfig{}, f, reg, prompt)
}

// toResult converts a topos.RunResult into the topos-free host Result.
func toResult(in topos.RunResult) Result {
	out := Result{Final: in.Final}
	out.Lineage.Nodes = make([]Node, 0, len(in.Lineage.Nodes))
	for _, n := range in.Lineage.Nodes {
		out.Lineage.Nodes = append(out.Lineage.Nodes, Node{
			ID: n.ID, Name: n.Name, Role: n.Role, Status: string(n.Status),
			Grants: n.Grants, Sandbox: n.Sandbox,
		})
	}
	out.Lineage.Edges = make([]Edge, 0, len(in.Lineage.Edges))
	for _, e := range in.Lineage.Edges {
		out.Lineage.Edges = append(out.Lineage.Edges, Edge{From: e.From, To: e.To, Kind: string(e.Kind)})
	}
	return out
}
