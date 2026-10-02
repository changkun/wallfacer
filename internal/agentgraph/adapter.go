package agentgraph

import (
	"context"
	"encoding/json"
	"time"

	"latere.ai/x/topos"
)

// RunAgent runs a single agent in-process as a one-node topos region with no
// delegation: the run that backs the native Topos harness. name is the agent's
// trace identity (node ids are <session>/<name>); systemPrompt is its system
// prompt; onEvent may be nil. A config without a credential returns
// ErrNoModelCredential before anything runs.
func RunAgent(ctx context.Context, sessionID string, c ModelConfig, name, systemPrompt, prompt, worktree string, onEvent func(Event)) (Result, error) {
	if name == "" {
		name = "agent"
	}
	region := topos.Region{
		Entry:    topos.AgentSpec{Name: name, SystemPrompt: systemPrompt},
		Autonomy: topos.Pinned,
	}
	opts, err := runOptions(sessionID, c)
	if err != nil {
		return Result{}, err
	}
	if worktree != "" {
		// Run the agent's tools in the task's git worktree (the real repo) rather
		// than a temp dir, so a native run reads and writes actual files. Workdir
		// is a plain root-level topos.Options field, so this stays within the
		// embeddable boundary (no topos/sandbox subpackage import). Empty worktree
		// keeps the default temp-dir sandbox.
		opts.Workdir = worktree
	}
	if onEvent != nil {
		// Bridge topos's observer to a topos-free Event so only this seam names
		// a topos type. The callback runs synchronously on the run's
		// goroutine(s); the host's onEvent must be non-blocking.
		opts.Observer = func(e topos.Event) { onEvent(toEvent(e)) }
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

// Result is the host-facing outcome of an in-process run. It mirrors
// topos.RunResult with topos-free types so a wallfacer package (e.g. the runner)
// can consume a run without importing topos and crossing the seam.
type Result struct {
	Final string
	Trace Trace
}

// Trace is the topos-free mirror of topos.Trace: the renderable run graph of
// nodes (agents) and edges (delegate / deliver / next). It marshals to the same
// JSON shape, so a host can persist it opaquely and a consumer can unmarshal it.
type Trace struct {
	Nodes []Node
	Edges []Edge
}

// Node mirrors topos.TraceNode.
type Node struct {
	ID      string
	Name    string
	Role    string
	Status  string
	Grants  []string
	Sandbox string
}

// Edge mirrors topos.TraceEdge (Kind is "delegate", "deliver", or "next").
type Edge struct {
	From string
	To   string
	Kind string
}

// Event is the topos-free mirror of topos.Event: one observation emitted
// during a run (lifecycle, tool use, delegation, per-turn assistant text). Node
// is the trace node id the event came from (it equals the emitting agent's
// topos session id), so a consumer can join a live event to a Trace node.
// PayloadJSON is the full event payload, opaque to the seam.
type Event struct {
	Name        string
	Node        string
	AgentID     string
	At          time.Time
	PayloadJSON json.RawMessage
}

// toResult converts a topos.RunResult into the topos-free host Result.
func toResult(in topos.RunResult) Result {
	out := Result{Final: in.Final}
	out.Trace.Nodes = make([]Node, 0, len(in.Trace.Nodes))
	for _, n := range in.Trace.Nodes {
		out.Trace.Nodes = append(out.Trace.Nodes, Node{
			ID: n.ID, Name: n.Name, Role: n.Role, Status: string(n.Status),
			Grants: n.Grants, Sandbox: n.Sandbox,
		})
	}
	out.Trace.Edges = make([]Edge, 0, len(in.Trace.Edges))
	for _, e := range in.Trace.Edges {
		out.Trace.Edges = append(out.Trace.Edges, Edge{From: e.From, To: e.To, Kind: string(e.Kind)})
	}
	return out
}
