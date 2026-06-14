package harness

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

func init() {
	Register(&cursorHarness{})
}

// cursorHarness adapts the `cursor-agent` CLI to the canonical Harness
// contract. Cursor emits Claude-style stream-json natively (a terminal
// `result` event carries session id, final text, and usage), so it needs
// none of codex's output-last-message wrapping.
type cursorHarness struct{}

// ID returns harness.Cursor.
func (cursorHarness) ID() ID { return Cursor }

// BuildArgv assembles the cursor-agent argv for a Request:
//
//	cursor-agent -p <prompt> --output-format stream-json --sandbox enabled
//	             [--workspace <cwd>] [--model <m>] [--resume <sid>]
//	             [--force --trust --approve-mcps]   (Permission Full)
//	             [--mode agent --force]             (Permission Edit)
//	             [--mode plan]                      (Permission ReadOnly)
//	             [--mcp-config <tmpfile>]
//
// SystemPrompt is prepended into the -p value because cursor-agent has no
// append-system-prompt flag. `--force` is mandatory for headless edits:
// without it cursor only *proposes* edits and exits without writing, so it
// is injected for both Edit and Full permission. `--sandbox enabled`
// matches cursor's default OS sandbox.
func (cursorHarness) BuildArgv(req Request) ([]string, io.Reader, error) {
	prompt := req.Prompt
	if req.SystemPrompt != "" {
		prompt = req.SystemPrompt + "\n\n---\n\n" + prompt
	}

	argv := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--sandbox", "enabled",
	}
	if req.Cwd != "" {
		argv = append(argv, "--workspace", req.Cwd)
	}
	if req.Model != "" {
		argv = append(argv, "--model", req.Model)
	}
	if req.SessionID != "" {
		argv = append(argv, "--resume", req.SessionID)
	}

	switch req.Permission {
	case PermissionFull:
		argv = append(argv, "--force", "--trust", "--approve-mcps")
	case PermissionEdit:
		// Default agent mode applies edits, but only with --force; without
		// it cursor proposes edits and exits without writing.
		argv = append(argv, "--mode", "agent", "--force")
	case PermissionReadOnly:
		argv = append(argv, "--mode", "plan")
	}

	if len(req.MCPServers) > 0 {
		path, err := writeCursorMCPConfig(req.MCPServers)
		if err != nil {
			return nil, nil, err
		}
		argv = append(argv, "--mcp-config", path)
	}

	return argv, nil, nil
}

// writeCursorMCPConfig writes the MCP server set to a temp JSON file in the
// shape cursor-agent's --mcp-config expects ({"mcpServers": {...}}) and
// returns its path. The file is not cleaned up; it is small and lives in
// the OS temp dir. MCP is not yet populated on the production run path, so
// this is exercised only by unit tests for now.
func writeCursorMCPConfig(servers []MCPServer) (string, error) {
	type entry struct {
		Command string            `json:"command,omitempty"`
		Args    []string          `json:"args,omitempty"`
		Env     map[string]string `json:"env,omitempty"`
		URL     string            `json:"url,omitempty"`
	}
	cfg := struct {
		MCPServers map[string]entry `json:"mcpServers"`
	}{MCPServers: make(map[string]entry, len(servers))}
	for _, s := range servers {
		cfg.MCPServers[s.Name] = entry{
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
			URL:     s.URL,
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "wallfacer-cursor-mcp-*.json")
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// cursorContent is one block of a cursor message's content array.
type cursorContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// cursorMessage is the message envelope cursor wraps user/assistant text in,
// mirroring the Anthropic SDK content-block shape.
type cursorMessage struct {
	Role    string          `json:"role"`
	Content []cursorContent `json:"content"`
}

// cursorUsage is the token accounting on cursor's terminal result event.
// Field names are camelCase; cost is not surfaced.
type cursorUsage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens"`
	CacheWriteTokens int `json:"cacheWriteTokens"`
}

// cursorLine captures the fields harness.Cursor sniffs from cursor-agent's
// stream-json output. Unknown fields are ignored.
type cursorLine struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	CallID    string          `json:"call_id"`
	Message   *cursorMessage  `json:"message"`
	ToolCall  json.RawMessage `json:"tool_call"`
	Result    string          `json:"result"`
	IsError   bool            `json:"is_error"`
	Usage     *cursorUsage    `json:"usage"`
}

// ParseEvent maps one NDJSON line of cursor-agent output to a canonical
// Event. Unrecognised lines yield Event{Kind: KindUnknown} with Raw set so
// callers record but do not crash on schema drift.
func (cursorHarness) ParseEvent(raw []byte) (Event, error) {
	evt := Event{Raw: append([]byte(nil), raw...)}

	var line cursorLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return evt, nil
	}
	evt.SessionID = line.SessionID

	switch line.Type {
	case "system":
		evt.Kind = KindSystemInit
	case "user":
		evt.Kind = KindUserResult
	case "assistant":
		evt.Kind = KindAssistantText
		evt.Text = cursorText(line.Message)
	case "tool_call":
		evt.Tool = &ToolCall{
			ID:    line.CallID,
			Name:  cursorToolName(line.ToolCall),
			Input: line.ToolCall,
		}
		switch line.Subtype {
		case "completed":
			evt.Kind = KindToolCallEnd
			evt.Tool.Output = line.ToolCall
		default:
			evt.Kind = KindToolCallStart
		}
	case "result":
		evt.Kind = KindResult
		evt.Subtype = line.Subtype
		evt.Text = line.Result
		if line.Usage != nil {
			evt.Usage = &Usage{
				InputTokens:         line.Usage.InputTokens,
				OutputTokens:        line.Usage.OutputTokens,
				CacheReadTokens:     line.Usage.CacheReadTokens,
				CacheCreationTokens: line.Usage.CacheWriteTokens,
			}
		}
		if line.IsError {
			evt.Kind = KindError
		}
	}
	return evt, nil
}

// cursorText concatenates the text blocks of a cursor message.
func cursorText(msg *cursorMessage) string {
	if msg == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range msg.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// cursorToolName returns the tool family name from a tool_call payload.
// Cursor nests the call under a single typed key (e.g. "shellToolCall");
// the key is the tool name. Returns "" when the payload has no single key.
func cursorToolName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	for k := range m {
		return k
	}
	return ""
}

// AuthEnv populates the env vars cursor-agent reads at startup.
// CURSOR_API_KEY is the headless credential; `cursor-agent login` is
// interactive and not triggered here.
func (cursorHarness) AuthEnv(cfg AuthConfig) (map[string]string, error) {
	env := map[string]string{}
	if cfg.CursorAPIKey != "" {
		env["CURSOR_API_KEY"] = cfg.CursorAPIKey
	}
	return env, nil
}

// Capabilities reports cursor's optional-feature matrix.
func (cursorHarness) Capabilities() Capabilities {
	return Capabilities{
		SupportsResume:       true,
		SupportsMCP:          true,
		SupportsSystemPrompt: false, // no append-system-prompt flag; we prepend
		EmitsUsage:           true,
		EmitsCost:            false, // not surfaced in result event
		NeedsTTY:             false,
	}
}
