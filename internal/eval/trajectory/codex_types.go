package trajectory

import "encoding/json"

// Codex CLI event schema mirror. Source of truth upstream:
// https://github.com/openai/codex codex-rs/exec/src/exec_events.rs
// (ThreadEvent enum — 8 variants discriminated by "type").
//
// Usage mirrors the Claude side: callers inspect StreamEvent.Type and
// call StreamEvent.Decode into one of the typed event structs below.
// The nested ThreadItem carried by item.* events is itself a
// discriminator; its typed variants live alongside the events.

// Top-level Codex event type discriminators.
const (
	CodexTypeThreadStarted = "thread.started"
	CodexTypeTurnStarted   = "turn.started"
	CodexTypeTurnCompleted = "turn.completed"
	CodexTypeTurnFailed    = "turn.failed"
	CodexTypeItemStarted   = "item.started"
	CodexTypeItemUpdated   = "item.updated"
	CodexTypeItemCompleted = "item.completed"
	CodexTypeError         = "error"
)

// Thread item type discriminators (snake_case on the wire).
const (
	CodexItemAgentMessage     = "agent_message"
	CodexItemReasoning        = "reasoning"
	CodexItemCommandExecution = "command_execution"
	CodexItemFileChange       = "file_change"
	CodexItemMcpToolCall      = "mcp_tool_call"
	CodexItemCollabToolCall   = "collab_tool_call"
	CodexItemWebSearch        = "web_search"
	CodexItemTodoList         = "todo_list"
	CodexItemError            = "error"
)

// ThreadStartedEvent is the first event of every run. The thread_id
// survives across runs — it's what `codex exec --resume=<id>` refers to.
type ThreadStartedEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
}

// TurnStartedEvent marks the beginning of a single prompt/response
// cycle. Empty by design — it only delimits the stream.
type TurnStartedEvent struct {
	Type string `json:"type"`
}

// TurnCompletedEvent closes a successful turn with the token usage
// for that turn only (not cumulative across the thread).
type TurnCompletedEvent struct {
	Type  string     `json:"type"`
	Usage CodexUsage `json:"usage"`
}

// TurnFailedEvent closes a turn that hit an error. The inner error
// structure mirrors top-level ThreadErrorEvent.
type TurnFailedEvent struct {
	Type  string            `json:"type"`
	Error ThreadErrorEvent `json:"error"`
}

// ItemStartedEvent is emitted when a new thread item first appears
// (typically in an in-progress state).
type ItemStartedEvent struct {
	Type string     `json:"type"`
	Item ThreadItem `json:"item"`
}

// ItemUpdatedEvent reports a change to a previously-seen item —
// streaming text, command output appending, patch status advancing.
type ItemUpdatedEvent struct {
	Type string     `json:"type"`
	Item ThreadItem `json:"item"`
}

// ItemCompletedEvent marks an item reaching a terminal state — success
// or failure. Counterpart to ItemStartedEvent.
type ItemCompletedEvent struct {
	Type string     `json:"type"`
	Item ThreadItem `json:"item"`
}

// ThreadErrorEvent is the top-level fatal-error variant; also appears
// as the inner payload of TurnFailedEvent.
type ThreadErrorEvent struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message"`
}

// CodexUsage is the per-turn token breakdown carried by
// TurnCompletedEvent.
type CodexUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	CachedInputTokens  int64 `json:"cached_input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
}

// ThreadItem is the common wrapper for all item.* event payloads —
// id and type flattened together with the variant-specific fields.
// Callers dispatch on Type and decode Raw into the matching typed
// struct below.
type ThreadItem struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

// UnmarshalJSON preserves the raw bytes so callers can decode the
// flattened variant fields via DecodeDetails even after the item has
// been extracted from its carrying event.
func (i *ThreadItem) UnmarshalJSON(data []byte) error {
	type alias ThreadItem
	var tmp alias
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*i = ThreadItem(tmp)
	i.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// DecodeDetails unmarshals the item's raw bytes into v. Use to pull
// out the variant-specific fields (e.g. AgentMessageItem, FileChangeItem)
// once Type has been inspected.
func (i ThreadItem) DecodeDetails(v any) error {
	if len(i.Raw) == 0 {
		return ErrNoRawPayload
	}
	return json.Unmarshal(i.Raw, v)
}

// AgentMessageItem is the agent's natural-language response, or a
// JSON string when structured output is configured.
type AgentMessageItem struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
}

// ReasoningItem is a summary of the agent's internal reasoning —
// emitted only when reasoning summaries are enabled.
type ReasoningItem struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
}

// CommandExecutionStatus enumerates the lifecycle states of a shell
// command executed by the agent.
type CommandExecutionStatus string

// CommandExecutionStatus values.
const (
	CommandInProgress CommandExecutionStatus = "in_progress"
	CommandCompleted  CommandExecutionStatus = "completed"
	CommandFailed     CommandExecutionStatus = "failed"
	CommandDeclined   CommandExecutionStatus = "declined"
)

// CommandExecutionItem records a shell command — spawned, running,
// and closed with an exit code.
type CommandExecutionItem struct {
	ID               string                 `json:"id"`
	Type             string                 `json:"type"`
	Command          string                 `json:"command"`
	AggregatedOutput string                 `json:"aggregated_output"`
	ExitCode         *int                   `json:"exit_code,omitempty"`
	Status           CommandExecutionStatus `json:"status"`
}

// PatchChangeKind enumerates filesystem operations in a FileChangeItem.
type PatchChangeKind string

// PatchChangeKind values.
const (
	PatchAdd    PatchChangeKind = "add"
	PatchDelete PatchChangeKind = "delete"
	PatchUpdate PatchChangeKind = "update"
)

// PatchApplyStatus enumerates the lifecycle states of a patch.
type PatchApplyStatus string

// PatchApplyStatus values.
const (
	PatchInProgress PatchApplyStatus = "in_progress"
	PatchCompleted  PatchApplyStatus = "completed"
	PatchFailed     PatchApplyStatus = "failed"
)

// FileUpdateChange is one path touched by a FileChangeItem.
type FileUpdateChange struct {
	Path string          `json:"path"`
	Kind PatchChangeKind `json:"kind"`
}

// FileChangeItem is a set of file modifications emitted as a single
// unit — typically a completed apply_patch tool call.
type FileChangeItem struct {
	ID      string             `json:"id"`
	Type    string             `json:"type"`
	Changes []FileUpdateChange `json:"changes"`
	Status  PatchApplyStatus   `json:"status"`
}

// McpToolCallStatus enumerates the lifecycle states of an MCP tool call.
type McpToolCallStatus string

// McpToolCallStatus values.
const (
	McpToolInProgress McpToolCallStatus = "in_progress"
	McpToolCompleted  McpToolCallStatus = "completed"
	McpToolFailed     McpToolCallStatus = "failed"
)

// McpToolCallItemResult is the result payload for a successful MCP
// tool invocation. Content is kept as raw JSON to avoid pulling in a
// full MCP type mirror.
type McpToolCallItemResult struct {
	Content           []json.RawMessage `json:"content"`
	StructuredContent json.RawMessage   `json:"structured_content,omitempty"`
}

// McpToolCallItemError is the failure payload for an MCP tool call.
type McpToolCallItemError struct {
	Message string `json:"message"`
}

// McpToolCallItem represents one MCP tool call — both the dispatch and
// its terminal result (success or failure).
type McpToolCallItem struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Server    string                 `json:"server"`
	Tool      string                 `json:"tool"`
	Arguments json.RawMessage        `json:"arguments,omitempty"`
	Result    *McpToolCallItemResult `json:"result,omitempty"`
	Error     *McpToolCallItemError  `json:"error,omitempty"`
	Status    McpToolCallStatus      `json:"status"`
}

// CollabToolCallStatus enumerates the lifecycle states of a collab
// tool call.
type CollabToolCallStatus string

// CollabToolCallStatus values.
const (
	CollabToolInProgress CollabToolCallStatus = "in_progress"
	CollabToolCompleted  CollabToolCallStatus = "completed"
	CollabToolFailed     CollabToolCallStatus = "failed"
)

// CollabAgentStatus enumerates the lifecycle states of an agent
// spawned or referenced by a collab tool call.
type CollabAgentStatus string

// CollabAgentStatus values.
const (
	CollabAgentPendingInit CollabAgentStatus = "pending_init"
	CollabAgentRunning     CollabAgentStatus = "running"
	CollabAgentInterrupted CollabAgentStatus = "interrupted"
	CollabAgentCompleted   CollabAgentStatus = "completed"
	CollabAgentErrored     CollabAgentStatus = "errored"
	CollabAgentShutdown    CollabAgentStatus = "shutdown"
	CollabAgentNotFound    CollabAgentStatus = "not_found"
)

// CollabAgentState is the last known state of one agent in a collab
// session.
type CollabAgentState struct {
	Status  CollabAgentStatus `json:"status"`
	Message *string           `json:"message,omitempty"`
}

// CollabToolCallItem records an invocation of a collab tool —
// spawn_agent, send_input, wait, close_agent.
type CollabToolCallItem struct {
	ID                string                      `json:"id"`
	Type              string                      `json:"type"`
	Tool              string                      `json:"tool"`
	SenderThreadID    string                      `json:"sender_thread_id"`
	ReceiverThreadIDs []string                    `json:"receiver_thread_ids"`
	Prompt            *string                     `json:"prompt,omitempty"`
	AgentsStates      map[string]CollabAgentState `json:"agents_states"`
	Status            CollabToolCallStatus        `json:"status"`
}

// WebSearchItem records a web search request. Action mirrors upstream
// codex_protocol::models::WebSearchAction and is kept raw because its
// definition lives in a separate crate and carries vendor-specific
// structure this package does not need to interpret.
type WebSearchItem struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Query  string          `json:"query"`
	Action json.RawMessage `json:"action"`
}

// ErrorItem is a non-fatal error surfaced as a thread item.
type ErrorItem struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// TodoItem is one entry in a TodoListItem.
type TodoItem struct {
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
}

// TodoListItem tracks the agent's running to-do list — first emitted
// when a plan is issued, updated as steps change state.
type TodoListItem struct {
	ID    string     `json:"id"`
	Type  string     `json:"type"`
	Items []TodoItem `json:"items"`
}
