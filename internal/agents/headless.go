package agents

// Title is the descriptor for the title-generation sub-agent.
// Produces a 2–5 word summary of a task's prompt; no workspace access.
var Title = Role{
	Slug:               "title",
	Title:              "Title",
	Description:        "Generates a short 2–5 word summary of a task's goal.",
	PromptTemplateName: "title",
}

// Oversight is the descriptor for the oversight-summary sub-agent.
// Parses the post-run event timeline into a structured phase list.
var Oversight = Role{
	Slug:               "oversight",
	Title:              "Oversight",
	Description:        "Summarises an agent run's activity into a structured phase list.",
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
