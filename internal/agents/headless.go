package agents

// Oversight is the descriptor for the oversight-summary sub-agent.
// Parses the post-run event timeline into a structured phase list.
var Oversight = Role{
	Slug:               "oversight",
	Title:              "Oversight",
	Description:        "Summarizes an agent run's activity into a structured phase list.",
	PromptTemplateName: "oversight",
}

// CommitMessage is the descriptor for the commit-message generation
// sub-agent.
var CommitMessage = Role{
	Slug:               "commit-msg",
	Title:              "Commit message",
	Description:        "Produces a descriptive git commit message from the task prompt and diff.",
	PromptTemplateName: "commit_message",
}
