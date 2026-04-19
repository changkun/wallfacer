package agents

// Role is a descriptor for one sub-agent role: what it is, what it
// reads from, and what prompt template it renders. The runner owns
// the dispatch plumbing (mount profile, parse function, sandbox
// routing) via its own binding table keyed by Role.Slug. Consumers
// who render agents (the Agents tab, the Flow composer) see only
// these neutral descriptor fields.
type Role struct {
	// Slug is the kebab-case identifier. Every reference to an
	// agent from other packages (Flow steps, API URLs, log labels)
	// is by slug. Required and unique within a registry.
	Slug string

	// Title is the human-readable name shown in UI.
	Title string

	// Description is the one-line summary the Agents tab renders.
	Description string

	// PromptTemplateName names the prompts-package API template
	// this agent renders (e.g. "title", "refinement"). Empty when
	// the agent consumes a prompt handed to it by the caller
	// without a built-in template (implementation, testing).
	PromptTemplateName string

	// Capabilities is a declarative list of what the agent needs
	// from its execution environment. Values are stable strings
	// ("workspace.read", "workspace.write", "board.context"); the
	// runner translates them into concrete container mounts and
	// the Flow UI surfaces them to the user.
	Capabilities []string

	// Multiturn is advisory metadata: true when the agent
	// participates in a multi-turn session loop. UI consumers use
	// it to label the row; the runner's binding table is the
	// source of truth for dispatch.
	Multiturn bool
}

// Capability values referenced from built-in descriptors. API
// responses surface the raw strings.
const (
	CapWorkspaceRead  = "workspace.read"
	CapWorkspaceWrite = "workspace.write"
	CapBoardContext   = "board.context"
)

// Registry is the merged catalog of built-in and user-authored
// agents. User-authored loading lands in a later task; for now the
// registry wraps the built-in list.
type Registry struct {
	order []string
	byKey map[string]Role
}

// NewBuiltinRegistry returns the registry populated with the seven
// built-in agent roles in registration order.
func NewBuiltinRegistry() *Registry {
	return NewRegistry(BuiltinAgents...)
}

// NewRegistry returns a Registry populated with the given roles in
// declaration order. Exported so tests (and future user-authored
// loaders) can assemble registries without mutating package state.
func NewRegistry(roles ...Role) *Registry {
	reg := &Registry{byKey: make(map[string]Role, len(roles))}
	for _, a := range roles {
		reg.order = append(reg.order, a.Slug)
		reg.byKey[a.Slug] = a
	}
	return reg
}

// Get returns the Role with the given slug and whether it was found.
func (r *Registry) Get(slug string) (Role, bool) {
	role, ok := r.byKey[slug]
	return role, ok
}

// List returns the registry's roles in registration order.
func (r *Registry) List() []Role {
	out := make([]Role, 0, len(r.order))
	for _, slug := range r.order {
		out = append(out, r.byKey[slug])
	}
	return out
}
