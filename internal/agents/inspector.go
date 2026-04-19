package agents

// IdeaAgent (brainstorm) scans the workspace read-only and proposes
// up to three high-impact task ideas.
var IdeaAgent = Role{
	Slug:               "ideate",
	Title:              "Brainstorm",
	Description:        "Scans the workspace and proposes up to three high-impact task ideas.",
	PromptTemplateName: "ideation",
	Capabilities:       []string{CapWorkspaceRead},
}
