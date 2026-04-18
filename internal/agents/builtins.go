package agents

// BuiltinAgents is the ordered catalog of the seven built-in sub-agent
// roles wallfacer ships today. The order here is the order the Agents
// tab renders and the order NewBuiltinRegistry registers, so it is
// deliberately grouped by tier: headless (fastest, no mounts) first,
// then inspector (read-only workspace), then heavyweight (read-write
// worktrees).
//
// Adding a new role: define the descriptor in the tier file that
// matches its MountMode, append it here, and the Agents tab + registry
// pick it up without further wiring.
var BuiltinAgents = []Role{
	// Headless tier — no workspace mounts.
	Title,
	Oversight,
	CommitMessage,

	// Inspector tier — read-only workspace.
	Refinement,
	IdeaAgent,

	// Heavyweight tier — read-write worktrees + board context.
	Implementation,
	Testing,
}
