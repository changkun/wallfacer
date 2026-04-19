package agents

// BuiltinAgents is the ordered catalog of built-in sub-agent roles.
// The order determines Agents-tab rendering and registry iteration.
var BuiltinAgents = []Role{
	Title,
	Oversight,
	CommitMessage,
	IdeaAgent,
	Implementation,
	Testing,
}
