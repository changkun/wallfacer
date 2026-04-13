package trajectory

import "encoding/json"

// Claude Code SDK message schema mirror. Source of truth upstream:
// https://github.com/anthropics/claude-code src/entrypoints/sdk/coreSchemas.ts
// (SDKMessageSchema — a 22-variant discriminated union).
//
// Not every variant is typed here; callers use (SDKMessage).Type and
// (SDKMessage).Subtype to discriminate and Decode to unmarshal into the
// typed struct below. New variants can be added without breaking callers.

// Discriminator values for SDKMessage.Type.
const (
	TypeAssistant   = "assistant"
	TypeUser        = "user"
	TypeResult      = "result"
	TypeSystem      = "system"
	TypeStreamEvent = "stream_event"
)

// Discriminator values for SDKMessage.Subtype on system and result.
const (
	SubtypeInit                        = "init"
	SubtypeCompactBoundary             = "compact_boundary"
	SubtypeStatus                      = "status"
	SubtypePostTurnSummary             = "post_turn_summary"
	SubtypeSuccess                     = "success"
	SubtypeErrorDuringExecution        = "error_during_execution"
	SubtypeErrorMaxTurns               = "error_max_turns"
	SubtypeErrorMaxBudgetUSD           = "error_max_budget_usd"
	SubtypeErrorMaxStructuredOutRetry  = "error_max_structured_output_retries"
)

// SDKMessage is one line of Claude Code stream-json output with the
// Type and Subtype discriminators surfaced for dispatch. Raw preserves
// the full JSON payload so callers can decode into a typed variant —
// or hand the bytes onward unchanged when no typed form is available.
type SDKMessage struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// Raw is the full JSON line as received. Not populated by json
	// decoding — the adapter sets it from the source bytes.
	Raw json.RawMessage `json:"-"`
}

// Decode unmarshals m.Raw into v. Returns an error if Raw is empty,
// which signals that the message was hand-constructed rather than
// produced by an adapter.
func (m SDKMessage) Decode(v any) error {
	if len(m.Raw) == 0 {
		return ErrNoRawPayload
	}
	return json.Unmarshal(m.Raw, v)
}

// SDKAssistantMessage is the assistant turn — model output. The inner
// Message field mirrors Anthropic's APIAssistantMessage from the
// @anthropic-ai/sdk package (content blocks: text, thinking, tool_use)
// and is preserved as raw JSON so this package does not have to track
// the API SDK's type churn.
type SDKAssistantMessage struct {
	Type            string          `json:"type"`
	Message         json.RawMessage `json:"message"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	Error           string          `json:"error,omitempty"`
	UUID            string          `json:"uuid"`
	SessionID       string          `json:"session_id"`
}

// SDKUserMessage carries user input or tool results back into the loop.
// IsReplay is set when Claude Code is replaying a persisted session
// (the SDKUserMessageReplay variant upstream); callers that want to
// separate live input from replays should filter on it.
type SDKUserMessage struct {
	Type            string          `json:"type"`
	Message         json.RawMessage `json:"message"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	IsSynthetic     bool            `json:"isSynthetic,omitempty"`
	ToolUseResult   json.RawMessage `json:"tool_use_result,omitempty"`
	Priority        string          `json:"priority,omitempty"`
	Timestamp       string          `json:"timestamp,omitempty"`
	UUID            string          `json:"uuid,omitempty"`
	SessionID       string          `json:"session_id,omitempty"`
	IsReplay        bool            `json:"isReplay,omitempty"`
}

// SDKResultSuccess closes a successful run. Contains the per-model usage
// breakdown (input/output/cache tokens) and aggregate cost — the bread
// and butter of every cost metric we'll compute.
type SDKResultSuccess struct {
	Type              string                `json:"type"`
	Subtype           string                `json:"subtype"`
	DurationMS        float64               `json:"duration_ms"`
	DurationAPIMS     float64               `json:"duration_api_ms"`
	IsError           bool                  `json:"is_error"`
	NumTurns          int                   `json:"num_turns"`
	Result            string                `json:"result"`
	StopReason        *string               `json:"stop_reason"`
	TotalCostUSD      float64               `json:"total_cost_usd"`
	Usage             json.RawMessage       `json:"usage"`
	ModelUsage        map[string]ModelUsage `json:"modelUsage"`
	PermissionDenials []PermissionDenial    `json:"permission_denials"`
	StructuredOutput  json.RawMessage       `json:"structured_output,omitempty"`
	UUID              string                `json:"uuid"`
	SessionID         string                `json:"session_id"`
}

// SDKResultError closes a run that terminated without producing a
// success result — timeout, budget exhaustion, max turns, max
// structured-output retries. Subtype carries which of those it was.
type SDKResultError struct {
	Type              string                `json:"type"`
	Subtype           string                `json:"subtype"`
	DurationMS        float64               `json:"duration_ms"`
	DurationAPIMS     float64               `json:"duration_api_ms"`
	IsError           bool                  `json:"is_error"`
	NumTurns          int                   `json:"num_turns"`
	StopReason        *string               `json:"stop_reason"`
	TotalCostUSD      float64               `json:"total_cost_usd"`
	Usage             json.RawMessage       `json:"usage"`
	ModelUsage        map[string]ModelUsage `json:"modelUsage"`
	PermissionDenials []PermissionDenial    `json:"permission_denials"`
	Errors            []string              `json:"errors"`
	UUID              string                `json:"uuid"`
	SessionID         string                `json:"session_id"`
}

// SDKSystemInit is the first message of a session — environment
// manifest. Contains the claude-code CLI version string used to produce
// the trajectory, which is exactly the metadata we need to pin adapter
// behavior per version.
type SDKSystemInit struct {
	Type              string            `json:"type"`
	Subtype           string            `json:"subtype"`
	Agents            []string          `json:"agents,omitempty"`
	APIKeySource      string            `json:"apiKeySource"`
	Betas             []string          `json:"betas,omitempty"`
	ClaudeCodeVersion string            `json:"claude_code_version"`
	CWD               string            `json:"cwd"`
	Tools             []string          `json:"tools"`
	MCPServers        []MCPServerStatus `json:"mcp_servers"`
	Model             string            `json:"model"`
	PermissionMode    string            `json:"permissionMode"`
	SlashCommands     []string          `json:"slash_commands"`
	OutputStyle       string            `json:"output_style"`
	Skills            []string          `json:"skills"`
	Plugins           []PluginInfo      `json:"plugins"`
	UUID              string            `json:"uuid"`
	SessionID         string            `json:"session_id"`
}

// SDKPartialAssistantMessage is emitted only when Claude Code is
// invoked with --include-partial-messages. Each one wraps a raw
// Anthropic stream event (message_start, content_block_delta, etc.).
type SDKPartialAssistantMessage struct {
	Type            string          `json:"type"`
	Event           json.RawMessage `json:"event"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	UUID            string          `json:"uuid"`
	SessionID       string          `json:"session_id"`
}

// ModelUsage is the per-model cost/token breakdown attached to result
// messages. Mirrors ModelUsageSchema upstream.
type ModelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	WebSearchRequests        int     `json:"webSearchRequests"`
	CostUSD                  float64 `json:"costUSD"`
	ContextWindow            int     `json:"contextWindow"`
	MaxOutputTokens          int     `json:"maxOutputTokens"`
}

// PermissionDenial records a tool call that the permission system
// blocked, attached to result messages.
type PermissionDenial struct {
	ToolName  string          `json:"tool_name"`
	ToolUseID string          `json:"tool_use_id"`
	ToolInput json.RawMessage `json:"tool_input"`
}

// MCPServerStatus is one row of the mcp_servers list on system init.
type MCPServerStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// PluginInfo is one row of the plugins list on system init.
type PluginInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Source string `json:"source,omitempty"`
}
