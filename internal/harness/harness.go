package harness

import (
	"encoding/json"
	"io"
)

// ID identifies a harness implementation. String values match the
// existing sandbox.Type values for forward compatibility.
type ID string

// Tier-A harness identifiers.
const (
	Claude   ID = "claude"
	Codex    ID = "codex"
	Cursor   ID = "cursor"
	OpenCode ID = "opencode"
	Pi       ID = "pi"
)

// Harness adapts a coding-agent CLI to wallfacer's canonical task model.
type Harness interface {
	// ID returns the harness identifier.
	ID() ID

	// BuildArgv translates a canonical Request into the CLI's argv and
	// optional stdin payload.
	BuildArgv(req Request) (argv []string, stdin io.Reader, err error)

	// ParseEvent parses one NDJSON line of harness output into a
	// canonical Event. Lines the harness does not recognise should be
	// returned as Event{Kind: KindUnknown, Raw: raw} rather than an
	// error, so callers can record but not crash on schema drift.
	ParseEvent(raw []byte) (Event, error)

	// AuthEnv returns the env vars to set when launching the harness
	// process, populated from the user's stored credentials.
	AuthEnv(cfg AuthConfig) (map[string]string, error)

	// Capabilities describes which optional features this harness
	// supports. Callers should consult Capabilities before populating
	// the corresponding Request fields.
	Capabilities() Capabilities
}

// Request is the canonical task input handed to a harness or executor.
// Fields not supported by a given harness are silently ignored — the
// harness is responsible for choosing a reasonable degradation.
type Request struct {
	Prompt       string
	Cwd          string
	Model        string     // harness-specific format (e.g. "sonnet" vs "openai/gpt-5")
	SessionID    string     // empty ⇒ new session; non-empty ⇒ resume
	Permission   Permission // ReadOnly | Edit | Full
	SystemPrompt string     // appended if SupportsSystemPrompt; otherwise prepended into Prompt
	MCPServers   []MCPServer
	MaxTurns     int     // 0 ⇒ no cap
	MaxCostUSD   float64 // 0 ⇒ no cap
}

// Event is one canonical update from a harness's output stream.
type Event struct {
	Kind       EventKind
	SessionID  string
	Text       string          // populated for KindAssistantText / KindResult
	Subtype    string          // harness-specific result subtype (e.g. error_max_tokens)
	Tool       *ToolCall       // populated for KindToolCall{Start,End}
	Usage      *Usage          // populated on KindResult when EmitsUsage
	StopReason string          // populated on KindResult
	Model      string          // model the harness reports for this event: init lines carry the session primary, assistant lines the per-turn model; empty when the harness does not report one
	Raw        json.RawMessage // original line — always preserved for replay / debugging
}

// EventKind enumerates canonical event types.
type EventKind int

// EventKind values. Implementations that encounter an unrecognised
// event MUST emit KindUnknown rather than an error.
const (
	KindUnknown EventKind = iota
	KindSystemInit
	KindAssistantText
	KindToolCallStart
	KindToolCallEnd
	KindUserResult
	KindResult
	KindError
	// KindThinking carries a reasoning / thinking block emitted on its own
	// output line (e.g. opencode "reasoning", codex reasoning items). Its
	// content rides in Event.Text, but consumers that accumulate the final
	// answer MUST ignore it (it is not the conclusion) — see the runner's
	// parseHarnessOutput, which keys the last-text fallback on
	// KindAssistantText only.
	KindThinking
)

// String returns the stable wire token for an EventKind, used by the
// normalized transcript stream (?format=normalized). Distinct from the int
// iota value so the wire shape never depends on enum ordering.
func (k EventKind) String() string {
	switch k {
	case KindSystemInit:
		return "system_init"
	case KindAssistantText:
		return "assistant"
	case KindThinking:
		return "thinking"
	case KindToolCallStart:
		return "tool_start"
	case KindToolCallEnd:
		return "tool_end"
	case KindUserResult:
		return "user_result"
	case KindResult:
		return "result"
	case KindError:
		return "error"
	default:
		return "unknown"
	}
}

// ToolCall carries the payload of KindToolCall{Start,End} events.
type ToolCall struct {
	ID     string
	Name   string
	Input  json.RawMessage
	Output json.RawMessage // populated on KindToolCallEnd
	Error  string          // populated when the tool call failed
}

// Usage carries token and cost accounting from a terminal Result event.
type Usage struct {
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	CostUSD             float64
}

// MCPServer is one MCP server configuration to surface to harnesses
// that support MCP. Stdio servers populate Command + Args; HTTP / SSE
// servers populate URL.
type MCPServer struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	URL     string
}
