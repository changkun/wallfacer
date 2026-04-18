package flow

import "changkun.de/x/wallfacer/internal/store"

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
}

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
