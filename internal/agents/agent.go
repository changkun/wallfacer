package agents

import (
	"time"

	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

// MountMode enumerates the three fundamental container-mount profiles
// the sub-agent roles in wallfacer fall into. See shared/agent-abstraction
// for the audit that grouped the seven existing roles into these tiers.
type MountMode int

const (
	// MountNone gives the container no workspace access. Suits
	// headless roles — title, oversight, commit-message — whose
	// input is the task's prompt and (for oversight/commit) a bundle
	// of pre-rendered text.
	MountNone MountMode = iota
	// MountReadOnly mounts every configured workspace read-only plus
	// the workspace instructions file. Suits inspector roles —
	// refinement, ephemeral ideation — that need to read the code
	// but must not modify it.
	MountReadOnly
	// MountReadWrite mounts each task worktree read-write and, when
	// Role.MountBoard is true, the board manifest + sibling
	// worktrees read-only. Suits heavyweight roles — implementation
	// and testing — that produce commits.
	MountReadWrite
)

// String renders a MountMode for API responses and logging.
func (m MountMode) String() string {
	switch m {
	case MountNone:
		return "none"
	case MountReadOnly:
		return "read-only"
	case MountReadWrite:
		return "read-write"
	}
	return "unknown"
}

// Usage mirrors the token-usage JSON object emitted by the agent
// container. Fields map directly to the Anthropic API usage response.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// Output is the top-level result object emitted by an agent container
// (either as a single JSON blob or as the last line of NDJSON
// stream-json). The runner parses the raw container stdout into this
// type; role ParseResult functions then extract role-specific
// structured payloads from Output.Result.
type Output struct {
	Result       string  `json:"result"`
	SessionID    string  `json:"session_id"`
	ThreadID     string  `json:"thread_id,omitempty"`
	StopReason   string  `json:"stop_reason"`
	Subtype      string  `json:"subtype"`
	IsError      bool    `json:"is_error"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	Usage        Usage   `json:"usage"`

	// ActualSandbox is set by the runner (not parsed from JSON) to
	// record which sandbox actually executed this turn, including
	// fallback scenarios where the primary sandbox hit a token limit
	// and Codex took over.
	ActualSandbox sandbox.Type `json:"-"`
}

// Role is a declarative descriptor for one kind of sub-agent. The
// runner's runAgent function dispatches on the descriptor's fields
// instead of calling role-specific launcher code, so adding a new
// role reduces to defining a new Role value plus (when needed) a
// template and a ParseResult.
type Role struct {
	// Activity names the per-activity sandbox routing bucket (feeds
	// runner.sandboxForTaskActivity). Required.
	Activity store.SandboxActivity
	// Name is the kebab-case identifier used when composing
	// container names: wallfacer-<Name>-<uuid8>. Also acts as the
	// slug consumers reference the role by. Required and unique
	// across the registry.
	Name string
	// Description is the one-line human summary the Agents tab
	// renders in the row list. Empty for roles that are wired only
	// from code (e.g. implementation) where the context already
	// tells the user what the role does.
	Description string
	// Timeout is a function so roles whose timeout depends on the
	// task (implementation, idea-agent) can derive it at call time.
	// Roles with a fixed timeout return a constant. Nil means "let
	// the caller's context own the deadline".
	Timeout func(*store.Task) time.Duration
	// MountMode selects the workspace-mount profile. See the
	// MountMode constants for the semantics of each tier.
	MountMode MountMode
	// MountBoard, when true, mounts the board manifest and sibling
	// worktrees read-only alongside the workspace. Only meaningful
	// for MountReadWrite roles today.
	MountBoard bool
	// SingleTurn, when true, skips the --resume session loop.
	// Headless and inspector roles use SingleTurn=true; the
	// heavyweight turn loop in execute.go drives multi-turn roles
	// itself.
	SingleTurn bool
	// ParseResult extracts the role-specific structured output from
	// the raw Output.Result string. Returning any lets each role
	// decode its own schema without leaking a shared type. The
	// concrete type is documented per descriptor in headless.go,
	// inspector.go, and heavyweight.go.
	ParseResult func(output *Output) (any, error)
	// Model, when non-nil, overrides the default per-sandbox model
	// lookup for this role. Title uses CLAUDE_TITLE_MODEL; other
	// roles inherit CLAUDE_DEFAULT_MODEL via the runner's default
	// resolver. Nil means "use the runner's default model
	// resolver".
	Model func(sb sandbox.Type) string
}

// Registry is the merged catalog of built-in and user-authored agents.
// User-authored loading lands in a later task; for now the registry
// wraps the built-in list and exposes a lookup + listing surface.
type Registry struct {
	order []string
	byKey map[string]Role
}

// NewBuiltinRegistry returns the registry populated with the seven
// built-in agent roles. The order of entries is stable so the Agents
// tab can render a deterministic list.
func NewBuiltinRegistry() *Registry {
	reg := &Registry{byKey: make(map[string]Role, len(BuiltinAgents))}
	for _, a := range BuiltinAgents {
		reg.order = append(reg.order, a.Name)
		reg.byKey[a.Name] = a
	}
	return reg
}

// Get returns the Role with the given slug and whether it was found.
func (r *Registry) Get(slug string) (Role, bool) {
	role, ok := r.byKey[slug]
	return role, ok
}

// List returns the registry's roles in registration order. Returned
// slice is a copy so callers cannot mutate registry state.
func (r *Registry) List() []Role {
	out := make([]Role, 0, len(r.order))
	for _, slug := range r.order {
		out = append(out, r.byKey[slug])
	}
	return out
}
