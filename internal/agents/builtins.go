package agents

// BuiltinAgents is the table of built-in sub-agent roles: the only roles
// wallfacer runs. The runner's binding table carries one entry per slug
// listed here, and a runner test checks that every role in this table has
// one.
var BuiltinAgents = []Role{
	Title,
	Oversight,
	CommitMessage,
	Implementation,
	Testing,
}
