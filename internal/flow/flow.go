package flow

import "latere.ai/x/wallfacer/internal/store"

// Flow is an ordered composition of sub-agent roles that a task
// executes against. v1 scope is linear with parallel-sibling steps; a
// full DAG model lands in a follow-up and replaces Steps with Graph.
type Flow struct {
	// Slug is the kebab-case identifier used in API URLs, task
	// references, and flow-to-flow lookups. Required and unique
	// within a registry.
	Slug string

	// Name is the human-readable label shown in UI.
	Name string

	// Description is the one-line summary the composer renders.
	Description string

	// Steps runs in declared order. Steps that share a
	// RunInParallelWith entry execute concurrently (the flow
	// engine owns that semantics). Empty is legal but uncommon;
	// v1 built-ins always have at least one step.
	Steps []Step

	// SpawnKind preserves the legacy TaskKind that tasks of this
	// flow run as. "" for normal tasks, "idea-agent" for
	// brainstorm. The field lives on the flow rather than the
	// agent because a flow's first step drives the task's
	// execution mode, not the individual agent.
	SpawnKind store.TaskKind

	// Builtin is true for flows shipped in the embedded catalog.
	// User-authored flows (future) set this to false so the UI
	// can mark them differently.
	Builtin bool

	// Agentic marks a flow that executes through the in-process topos
	// agent-graph runtime (internal/agentgraph) instead of the legacy
	// flow engine. The runner dispatch builds a topos.Region from the
	// flow's steps + the agents registry, runs it, and persists the
	// resulting lineage on the task. Default false keeps every existing
	// flow on its current execution path. See
	// specs/local/topos-runtime-integration.md (M2).
	Agentic bool

	// Dynamic, on an agentic flow, opts the region into model-driven
	// delegation (topos Autonomy: Dynamic) instead of the deterministic
	// pinned chain: the entry agent gets a delegate tool over the peers
	// directory and the model decides whom to hand off to. Default false
	// keeps the pinned chain (Autonomy: Pinned). Ignored for non-agentic
	// flows. See specs/local/topos-runtime-integration.md (M3).
	Dynamic bool

	// Topology, on a dynamic flow, decides whom an agent may delegate to:
	// orchestrator-worker (the default) lets only the entry agent delegate;
	// mesh lets any agent delegate recursively (bounded by MaxHandoffDepth).
	// Empty maps to orchestrator-worker. Ignored when Dynamic is false.
	Topology Topology

	// MaxHandoffDepth, on a mesh flow, bounds the recursive delegation
	// depth. Zero leaves the topos default (3). Ignored outside a mesh
	// region.
	MaxHandoffDepth int
}

// Topology selects whom an agent in a dynamic agentic flow may delegate
// to. It is wallfacer's own enum; the agentgraph seam maps it onto the
// topos topology constants so only that package names a topos type.
type Topology string

const (
	// TopologyOrchestratorWorker (the zero value) lets only the entry
	// agent delegate to peers; a delegated peer runs without a delegate
	// tool. The safe default.
	TopologyOrchestratorWorker Topology = "orchestrator-worker"

	// TopologyMesh lets any agent delegate to a peer recursively, bounded
	// by Flow.MaxHandoffDepth. Opt-in.
	TopologyMesh Topology = "mesh"
)

// Step is a single node in a Flow. AgentSlug references a role in
// internal/agents by slug; resolution happens at engine execute time
// so flows don't hold direct agent pointers. Optional and
// RunInParallelWith give the engine enough hints for v1 composition.
type Step struct {
	// AgentSlug references agents.Role.Slug. The flow engine
	// resolves it via the agents registry at execute time.
	AgentSlug string

	// Optional steps can be skipped by the composer (e.g.
	// refinement before implement). The engine treats skipped
	// steps as no-ops.
	Optional bool

	// InputFrom names a previous step whose parsed result feeds
	// this step's prompt. Empty means the engine uses the task's
	// prompt verbatim.
	InputFrom string

	// RunInParallelWith names sibling step AgentSlugs that
	// execute concurrently with this one. All steps in a parallel
	// group must list each other so the engine can group them
	// deterministically.
	RunInParallelWith []string
}
