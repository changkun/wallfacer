package agentgraph

import (
	"context"
	"encoding/json"
	"time"

	"latere.ai/x/topos"
	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/flow"
)

// FromFlow compiles a wallfacer flow + agents registry into a topos.Region via the
// canonical authored graph model: FromFlowGraph builds the ref-form graph.Graph,
// registryResolver swaps each ref for the inline spec reg holds, and ToRuntime
// lowers it. A wallfacer flow is a single region, so the lowered graph's one region
// is returned.
//
// The flow's first step becomes the region entry; the remaining steps become the
// ordered peer chain. The resolver maps each agents.Role onto the spec: the slug is
// the stable identity (so lineage node ids are <session>/<slug>), Title/Description
// carry the role labels, PromptTmpl becomes the system prompt, and Capabilities
// become the permission scopes. A non-dynamic flow lowers to a deterministic pinned
// chain (Optional / RunInParallelWith hints are not expressed by the graph model); a
// dynamic flow lowers to Autonomy: Dynamic with the topology its coordination
// selects. Built-in roles leave PromptTmpl empty (they render through the prompts
// package); an empty system prompt is legal for the fake model and the headless path.
func FromFlow(f flow.Flow, reg *agents.Registry) (topos.Region, error) {
	authored, err := FromFlowGraph(f)
	if err != nil {
		return topos.Region{}, err
	}
	resolved, err := authored.Resolve(registryResolver(reg))
	if err != nil {
		return topos.Region{}, err
	}
	rt, err := resolved.ToRuntime()
	if err != nil {
		return topos.Region{}, err
	}
	return rt.Regions[0].Region, nil
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

// RunAgent runs a single agent in-process as a one-node topos region — the
// degenerate, non-delegating case that backs the native Topos harness. A plain
// task with no multi-agent flow executes as one agent, sharing the same engine,
// lineage, observer, and model selection the multi-agent path uses. name is the
// agent's lineage identity (node ids are <session>/<name>); systemPrompt is its
// system prompt; onEvent may be nil. Like RunFlowWithModel, an unconfigured
// ModelConfig transparently uses the deterministic fake model, so tests and
// no-credential dev keep working.
func RunAgent(ctx context.Context, sessionID string, c ModelConfig, name, systemPrompt, prompt, worktree string, onEvent func(TraceEvent)) (Result, error) {
	if name == "" {
		name = "agent"
	}
	region := topos.Region{
		Entry:    topos.AgentSpec{Name: name, SystemPrompt: systemPrompt},
		Autonomy: topos.Pinned,
	}
	opts := runOptions(sessionID, c, flow.Flow{})
	if worktree != "" {
		// Run the agent's tools in the task's git worktree (the real repo) rather
		// than a temp dir, so a native run reads and writes actual files. Workdir
		// is a plain root-level topos.Options field, so this stays within the
		// embeddable boundary (no topos/sandbox subpackage import). Empty worktree
		// keeps the default temp-dir sandbox.
		opts.Workdir = worktree
	}
	if onEvent != nil {
		// Same topos-free observer bridge as RunFlowWithModel; this seam is the
		// only place that names a topos type.
		opts.Observer = func(e topos.Event) { onEvent(toTraceEvent(e)) }
	}
	runner, err := NewRunner(opts)
	if err != nil {
		return Result{}, err
	}
	res, err := runner.Run(ctx, region, prompt)
	if err != nil {
		return Result{}, err
	}
	return toResult(res), nil
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

// TraceEvent is the topos-free mirror of topos.Event: one observation emitted
// during a run (lifecycle, tool use, delegation, per-turn assistant text). Node
// is the lineage node id the event came from (it equals the emitting agent's
// topos session id), so a consumer can join a live event to a Lineage node.
// PayloadJSON is the full event payload, opaque to the seam.
type TraceEvent struct {
	Name        string
	Node        string
	AgentID     string
	At          time.Time
	PayloadJSON json.RawMessage
}

// RunFlowFake runs a flow through the agent-graph runtime with the deterministic,
// network-free fake model, returning a topos-free Result. sessionID seeds the
// run id so lineage node ids (<session>/<agent>) are stable. It is the explicit
// fake entrypoint, equivalent to RunFlowWithModel with an unconfigured config.
func RunFlowFake(ctx context.Context, sessionID string, f flow.Flow, reg *agents.Registry, prompt string) (Result, error) {
	return RunFlowWithModel(ctx, sessionID, ModelConfig{}, f, reg, prompt, nil)
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
