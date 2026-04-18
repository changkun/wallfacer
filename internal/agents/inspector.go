package agents

// Refinement expands a task prompt into a detailed implementation
// spec by reading the workspace (read-only).
var Refinement = Role{
	Slug:               "refine",
	Title:              "Refinement",
	Description:        "Expands a task prompt into a detailed implementation spec.",
	PromptTemplateName: "refinement",
	Capabilities:       []string{CapWorkspaceRead},
}

// IdeaAgent (brainstorm) scans the workspace read-only and proposes
// up to three high-impact task ideas.
var IdeaAgent = Role{
	Slug:               "ideate",
	Title:              "Brainstorm",
	Description:        "Scans the workspace and proposes up to three high-impact task ideas.",
	PromptTemplateName: "ideation",
	Capabilities:       []string{CapWorkspaceRead},
}
