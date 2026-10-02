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

// Review is the descriptor for the review sub-agent. It runs on a model other
// than the task's, reads the task prompt, the acceptance criteria and the
// task's diff from its prompt (no workspace access), and answers with
// structured findings and a verdict that the task's next turn can act on.
var Review = Role{
	Slug:               "review",
	Title:              "Review",
	Description:        "Reviews a task's diff on a second model and reports findings with a verdict.",
	PromptTemplateName: "review",
}
