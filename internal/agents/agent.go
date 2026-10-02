package agents

// Role is a descriptor for one built-in sub-agent role: what it is, what it
// reads from, and what prompt template it renders. The runner owns the
// dispatch plumbing (mount profile, parse function, sandbox routing) via its
// own binding table keyed by Role.Slug.
type Role struct {
	// Slug is the kebab-case identifier. The runner's binding table, log
	// labels and container names refer to a role by slug. Required and
	// unique among the built-in roles.
	Slug string

	// Title is the human-readable name of the role.
	Title string

	// Description is the one-line summary of what the role does.
	Description string

	// PromptTemplateName names the prompts-package API template
	// this agent renders (e.g. "title", "commit_message"). Empty when
	// the agent consumes a prompt handed to it by the caller
	// without a built-in template (implementation, testing).
	PromptTemplateName string

	// Capabilities is a declarative list of what the agent needs
	// from its execution environment. Values are stable strings
	// ("workspace.read", "workspace.write", "board.context").
	Capabilities []string

	// Multiturn is advisory metadata: true when the agent
	// participates in a multi-turn session loop. The runner's binding
	// table is the source of truth for dispatch.
	Multiturn bool

	// Harness pins one invocation of a role to a specific coding harness
	// (a harness.ID such as "claude" or "codex"). The built-in values leave
	// it empty; a caller that must reach a particular harness sets it on a
	// copy of a built-in role for that call. A pin layers above every other
	// tier of the runner's harness resolution (task sandbox, per-activity
	// env setting, default env setting, harness.Default()). Empty inherits
	// those tiers.
	Harness string
}

// Capability values referenced from built-in descriptors.
const (
	CapWorkspaceRead  = "workspace.read"
	CapWorkspaceWrite = "workspace.write"
	CapBoardContext   = "board.context"
)
