package harness

import (
	"encoding/json"
	"io"
	"strings"
)

func init() {
	Register(&piHarness{})
}

// piHarness adapts Armin Ronacher's Pi coding agent (the `pi` CLI from
// earendil-works/pi) to the canonical Harness contract. This is NOT
// Inflection's Pi chatbot; it is a local coding agent with a four-tool core
// (Read, Write, Edit, Bash).
//
// Pi's `--mode json` emits one JSON object per line: a `session` header,
// `agent_start` / `turn_start` lifecycle markers, `message_*` events,
// `tool_execution_*` events, and a terminal `agent_end` carrying the full
// message list. Token usage and the stop reason live on each assistant
// message (under `usage` / `stopReason`), not on a dedicated result event —
// so the terminal result is synthesized from the last assistant message in
// `agent_end`.
type piHarness struct{}

// ID returns harness.Pi.
func (piHarness) ID() ID { return Pi }

// BuildArgv assembles the pi argv for a Request:
//
//	pi -p --mode json
//	   [--provider <name>] [--model <id>]
//	   [--session <id>]
//	   [--tools Read | --tools Read,Write,Edit]
//	   <prompt>          (positional, last)
//
// Pi's `-p` is a boolean ("print and exit"); the prompt is a positional
// argument after the flags, not the value of `-p`. Model selection uses a
// two-flag split: a "provider/model" Request.Model is cut on the first "/"
// into `--provider <provider> --model <model>`; a bare value (no "/") is
// passed as `--model` alone, letting pi resolve the provider itself.
// SystemPrompt has no native pass-through (Capabilities reports
// SupportsSystemPrompt=false); it is prepended into the prompt.
func (piHarness) BuildArgv(req Request) ([]string, io.Reader, error) {
	prompt := req.Prompt
	if req.SystemPrompt != "" {
		prompt = req.SystemPrompt + "\n\n---\n\n" + prompt
	}

	argv := []string{"-p", "--mode", "json"}

	if req.Model != "" {
		if provider, model, ok := strings.Cut(req.Model, "/"); ok {
			argv = append(argv, "--provider", provider, "--model", model)
		} else {
			argv = append(argv, "--model", req.Model)
		}
	}
	if req.SessionID != "" {
		argv = append(argv, "--session", req.SessionID)
	}

	// Pi's four-tool core is Read, Write, Edit, Bash. ReadOnly restricts to
	// Read; Edit grants Read,Write,Edit (no Bash); Full leaves the default
	// (all four enabled) by omitting --tools.
	switch req.Permission {
	case PermissionReadOnly:
		argv = append(argv, "--tools", "Read")
	case PermissionEdit:
		argv = append(argv, "--tools", "Read,Write,Edit")
	case PermissionFull:
		// no --tools: all default tools enabled
	}

	// The prompt is positional and must come last.
	argv = append(argv, prompt)
	return argv, nil, nil
}

// piTextContent is one block of a pi message's content array. Only text
// blocks (Type=="text") carry assistant prose; thinking and tool-call blocks
// unmarshal here with an empty Text and are ignored by piMessageText.
type piTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// piUsage is the token accounting attached to each assistant message. Field
// names are pi's: input / output / cacheRead / cacheWrite. The nested cost
// object is not surfaced (Capabilities reports EmitsCost=false).
type piUsage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cacheRead"`
	CacheWrite int `json:"cacheWrite"`
}

// piMessage is the pi message envelope. Content is kept raw because its
// shape varies by role: assistant content is an array of typed blocks, while
// user content may be a plain string — decoding it eagerly into a typed slice
// would fail the whole line. Usage and StopReason are populated only for
// assistant messages.
type piMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Usage      *piUsage        `json:"usage"`
	StopReason string          `json:"stopReason"`
}

// piLine captures the fields harness.Pi sniffs from pi's --mode json output.
// Unknown fields are ignored.
type piLine struct {
	Type       string          `json:"type"`
	ID         string          `json:"id"`         // session header id
	ToolCallID string          `json:"toolCallId"` // tool_execution_* events
	ToolName   string          `json:"toolName"`
	Args       json.RawMessage `json:"args"`
	Result     json.RawMessage `json:"result"`
	IsError    bool            `json:"isError"`
	Message    *piMessage      `json:"message"`  // message_* events
	Messages   []piMessage     `json:"messages"` // agent_end
}

// ParseEvent maps one JSON line of pi's --mode json output to a canonical
// Event. Unrecognised lines yield Event{Kind: KindUnknown} with Raw set so
// callers record but do not crash on schema drift.
func (piHarness) ParseEvent(raw []byte) (Event, error) {
	evt := Event{Raw: append([]byte(nil), raw...)}

	var line piLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return evt, nil
	}

	switch line.Type {
	case "session":
		// First line: the session header. `id` is the session id pi reuses
		// for --session resume.
		evt.Kind = KindSystemInit
		evt.SessionID = line.ID
	case "tool_execution_start":
		evt.Kind = KindToolCallStart
		evt.Tool = &ToolCall{ID: line.ToolCallID, Name: line.ToolName, Input: line.Args}
	case "tool_execution_end":
		evt.Kind = KindToolCallEnd
		evt.Tool = &ToolCall{ID: line.ToolCallID, Name: line.ToolName, Output: line.Result}
		if line.IsError {
			evt.Tool.Error = string(line.Result)
		}
	case "message_end":
		// A completed message. Only assistant messages carry prose worth
		// surfacing; the accumulator keeps the last non-empty text.
		if line.Message != nil && line.Message.Role == "assistant" {
			evt.Kind = KindAssistantText
			evt.Text = piMessageText(line.Message)
		}
	case "agent_end":
		// Terminal event. Synthesize the canonical result from the last
		// assistant message: its text, stop reason, and usage.
		evt.Kind = KindResult
		if msg := piLastAssistant(line.Messages); msg != nil {
			evt.Text = piMessageText(msg)
			evt.StopReason = msg.StopReason
			if msg.StopReason == "error" {
				evt.Kind = KindError
			}
			if msg.Usage != nil {
				evt.Usage = &Usage{
					InputTokens:         msg.Usage.Input,
					OutputTokens:        msg.Usage.Output,
					CacheReadTokens:     msg.Usage.CacheRead,
					CacheCreationTokens: msg.Usage.CacheWrite,
				}
			}
		}
	}
	return evt, nil
}

// piMessageText concatenates the text blocks of a pi message, skipping
// thinking and tool-call blocks.
func piMessageText(msg *piMessage) string {
	if msg == nil || len(msg.Content) == 0 {
		return ""
	}
	var blocks []piTextContent
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		// Content is a plain string (user messages), not assistant prose.
		return ""
	}
	var out strings.Builder
	for _, c := range blocks {
		if c.Type == "text" {
			out.WriteString(c.Text)
		}
	}
	return out.String()
}

// piLastAssistant returns the last assistant message in msgs, or nil. pi's
// agent_end lists the full conversation; the final assistant turn holds the
// run's terminal text, stop reason, and usage.
func piLastAssistant(msgs []piMessage) *piMessage {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return &msgs[i]
		}
	}
	return nil
}

// AuthEnv returns no env vars. Provider keys (ANTHROPIC_API_KEY,
// OPENAI_API_KEY, etc.) are read by pi directly from the inherited process
// environment. AuthConfig.PiAPIKey is reserved for a future Pi-specific
// subscription provider and is unused in v1.
func (piHarness) AuthEnv(AuthConfig) (map[string]string, error) {
	return map[string]string{}, nil
}

// Capabilities reports pi's optional-feature matrix.
func (piHarness) Capabilities() Capabilities {
	return Capabilities{
		SupportsResume:       true,
		SupportsMCP:          true,
		SupportsSystemPrompt: false, // no system-prompt pass-through here; we prepend
		EmitsUsage:           true,
		EmitsCost:            false, // cost object not surfaced
		NeedsTTY:             false,
	}
}
