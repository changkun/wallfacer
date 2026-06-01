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
	Text       string          // populated for KindAssistantText
	Tool       *ToolCall       // populated for KindToolCall{Start,End}
	Usage      *Usage          // populated on KindResult when EmitsUsage
	StopReason string          // populated on KindResult
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
)

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
