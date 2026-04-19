package flow

import "changkun.de/x/wallfacer/internal/store"

// builtins is the embedded catalog seeded on first boot. Each Flow
// references agents by Role.Slug from internal/agents — the flow
// engine resolves those slugs at execute time.
//
// Adding a new built-in: append a Flow value here and the registry
// picks it up without further wiring. The Builtin field is set via
// NewBuiltinRegistry so callers can't forget it.
var builtins = []Flow{
	{
		Slug:        "implement",
		Name:        "Implement",
		Description: "Implement, test, then commit with a generated message and oversight.",
		Steps: []Step{
			{AgentSlug: "impl"},
			{AgentSlug: "test"},
			{AgentSlug: "commit-msg", RunInParallelWith: []string{"title", "oversight"}},
			{AgentSlug: "title", RunInParallelWith: []string{"commit-msg", "oversight"}},
			{AgentSlug: "oversight", RunInParallelWith: []string{"commit-msg", "title"}},
		},
	},
	{
		Slug:        "brainstorm",
		Name:        "Brainstorm",
		Description: "Scan the workspace and propose up to three high-impact task ideas.",
		SpawnKind:   store.TaskKindIdeaAgent,
		Steps:       []Step{{AgentSlug: "ideate"}},
	},
	{
		Slug:        "test-only",
		Name:        "Test only",
		Description: "Run the test-verification agent against the current worktree state.",
		Steps:       []Step{{AgentSlug: "test"}},
	},
}
