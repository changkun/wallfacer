package agents

// Implementation runs each turn of the task-execution loop with
// read-write access to the task's worktree + board context.
var Implementation = Role{
	Slug:         "impl",
	Title:        "Implementation",
	Description:  "Executes the task prompt and produces commits on the task's worktree.",
	Capabilities: []string{CapWorkspaceWrite, CapBoardContext},
	Multiturn:    true,
}

// Testing runs the task's test suite after Implementation and
// classifies the verdict.
var Testing = Role{
	Slug:         "test",
	Title:        "Testing",
	Description:  "Runs the task's test suite and classifies the verdict.",
	Capabilities: []string{CapWorkspaceWrite, CapBoardContext},
	Multiturn:    true,
}
