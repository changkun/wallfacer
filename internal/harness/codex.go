package harness

import (
	"encoding/json"
	"io"
	"strings"
)

func init() {
	Register(&codexHarness{})
}

// codexHarness adapts the `codex` CLI to the canonical Harness contract.
type codexHarness struct{}

// ID returns harness.Codex.
func (codexHarness) ID() ID { return Codex }

// BuildArgv assembles the codex argv for a Request. The shape mirrors
// codex-agent.sh and host_codex.go:
//
//	codex exec --full-auto --sandbox workspace-write --skip-git-repo-check
//	           --json --color never
//	           [--model <model>]
//	           <prompt>
//
// SystemPrompt is prepended into the prompt since codex's exec subcommand
// has no append-system-prompt equivalent. SessionID is ignored because
// codex exec does not support session resume via a stable flag.
func (codexHarness) BuildArgv(req Request) ([]string, io.Reader, error) {
	argv := []string{
		"exec",
		"--full-auto",
		"--sandbox", "workspace-write",
		"--skip-git-repo-check",
		"--json",
		"--color", "never",
	}
	if req.Model != "" {
		argv = append(argv, "--model", req.Model)
	}

	prompt := req.Prompt
	if req.SystemPrompt != "" {
		prompt = req.SystemPrompt + "\n\n---\n\n" + prompt
	}
	argv = append(argv, prompt)
	return argv, nil, nil
}

type codexUsageLine struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CachedInputTokens        int `json:"cached_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// codexEventLine captures the fields harness.Codex sniffs from codex's
// native JSON event stream. Unknown fields are ignored.
//
// It also carries the top-level claude-shaped result fields (Result,
// IsError, Subtype) so the parser can recognise the normalized result
// envelope the host launcher emits (codex's final assistant message lives
// in --output-last-message, not in the event stream) without a separate
// claude-shaped decode pass.
type codexEventLine struct {
	Type         string          `json:"type"`
	SessionID    string          `json:"session_id,omitempty"`
	ThreadID     string          `json:"thread_id,omitempty"`
	StopReason   string          `json:"stop_reason,omitempty"`
	TotalCostUSD *float64        `json:"total_cost_usd,omitempty"`
	Usage        *codexUsageLine `json:"usage,omitempty"`
	Result       string          `json:"result,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	Subtype      string          `json:"subtype,omitempty"`
	Item         json.RawMessage `json:"item,omitempty"`
}

// codexItem is the nested object on an item.* event. Codex's exec --json
// stream wraps every thread item as {"type":"item.completed","item":{...}};
// the item's own "type" field discriminates the kind (agent_message,
// reasoning, command_execution, file_change, mcp_tool_call, web_search,
// todo_list, error). See takopi.dev's exec-json cheatsheet and
// openai/codex codex-rs.
type codexItem struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	Text             string `json:"text"`
	Command          string `json:"command"`
	AggregatedOutput string `json:"aggregated_output"`
	ExitCode         *int   `json:"exit_code"`
	Status           string `json:"status"`
}

// ParseEvent maps one NDJSON line of codex output to a canonical Event.
// Codex emits dot-namespaced event types (thread.*, turn.*, item.*); this
// adapter recognises the high-leverage ones, plus the normalized result
// envelope the host launcher appends (a typeless line carrying the final
// message text from --output-last-message). Unrecognised lines fall
// through to KindUnknown.
func (codexHarness) ParseEvent(raw []byte) (Event, error) {
	evt := Event{Raw: append([]byte(nil), raw...)}

	var line codexEventLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return evt, nil
	}

	if line.SessionID != "" {
		evt.SessionID = line.SessionID
	} else if line.ThreadID != "" {
		evt.SessionID = line.ThreadID
	}

	usageFrom := func() *Usage {
		// total_cost_usd is independent of the token usage object; surface
		// a Usage carrying cost even when token counts are absent.
		if line.Usage == nil && line.TotalCostUSD == nil {
			return nil
		}
		u := &Usage{}
		if line.Usage != nil {
			cacheRead := line.Usage.CacheReadInputTokens
			if cacheRead == 0 {
				cacheRead = line.Usage.CachedInputTokens
			}
			u.InputTokens = line.Usage.InputTokens
			u.OutputTokens = line.Usage.OutputTokens
			u.CacheCreationTokens = line.Usage.CacheCreationInputTokens
			u.CacheReadTokens = cacheRead
		}
		if line.TotalCostUSD != nil {
			u.CostUSD = *line.TotalCostUSD
		}
		return u
	}

	switch {
	case line.Type == "thread.started":
		evt.Kind = KindSystemInit
	case line.Type == "turn.completed":
		evt.Kind = KindResult
		evt.StopReason = line.StopReason
		if evt.StopReason == "" {
			evt.StopReason = "end_turn"
		}
		evt.Text = line.Result
		evt.Subtype = line.Subtype
		evt.Usage = usageFrom()
		if line.IsError {
			evt.Kind = KindError
		}
	case strings.HasPrefix(line.Type, "item."):
		// Items cover assistant messages, reasoning, command executions, file
		// changes, etc. Key on item.completed so each item yields exactly one
		// event (item.started / item.updated are intermediate and left
		// KindUnknown). codexParseItem fills evt from the nested item.
		if line.Type == "item.completed" {
			codexParseItem(&evt, line.Item)
		}
	case line.Type == "turn.failed" || line.Type == "error":
		evt.Kind = KindError
	case line.Type == "result" || (line.Type == "" &&
		(line.Result != "" || line.StopReason != "" || line.SessionID != "" || line.IsError)):
		// Normalized result envelope (claude-shaped, typeless or
		// type:"result") appended by the host launcher with the final
		// message recovered from --output-last-message. Recognised so the
		// codex harness owns parsing of its own normalized output.
		evt.Kind = KindResult
		evt.StopReason = line.StopReason
		evt.Text = line.Result
		evt.Subtype = line.Subtype
		evt.Usage = usageFrom()
		if line.IsError {
			evt.Kind = KindError
		}
	}
	return evt, nil
}

// codexParseItem fills evt from the nested item of an item.completed event.
// One item → one canonical Event:
//   - agent_message → KindAssistantText (item.text)
//   - reasoning     → KindThinking (item.text)
//   - command_execution → KindToolCallEnd (command as input, aggregated_output
//     as output, an error when status=="failed")
//   - file_change / mcp_tool_call / web_search → KindToolCallEnd with the raw
//     item as the call detail (generic v1 — no per-subtype summary)
//   - error → KindError
//
// An unrecognised item with text degrades to assistant text; otherwise it is
// left KindUnknown (dropped from the normalized view, preserved in Raw).
func codexParseItem(evt *Event, rawItem json.RawMessage) {
	var it codexItem
	if err := json.Unmarshal(rawItem, &it); err != nil {
		return
	}
	switch it.Type {
	case "agent_message":
		evt.Kind = KindAssistantText
		evt.Text = it.Text
	case "reasoning":
		evt.Kind = KindThinking
		evt.Text = it.Text
	case "command_execution":
		evt.Kind = KindToolCallEnd
		tc := &ToolCall{ID: it.ID, Name: "shell"}
		if input, err := json.Marshal(map[string]string{"command": it.Command}); err == nil {
			tc.Input = input
		}
		if it.AggregatedOutput != "" {
			if out, err := json.Marshal(it.AggregatedOutput); err == nil {
				tc.Output = out
			}
		}
		if it.Status == "failed" {
			if it.AggregatedOutput != "" {
				tc.Error = it.AggregatedOutput
			} else {
				tc.Error = "command failed"
			}
		}
		evt.Tool = tc
	case "file_change", "mcp_tool_call", "web_search":
		evt.Kind = KindToolCallEnd
		evt.Tool = &ToolCall{ID: it.ID, Name: it.Type, Input: rawItem}
	case "error":
		evt.Kind = KindError
		evt.Text = it.Text
	default:
		if it.Text != "" {
			evt.Kind = KindAssistantText
			evt.Text = it.Text
		}
	}
}

// AuthEnv populates the env vars codex reads at startup. OPENAI_API_KEY
// is the primary credential; OPENAI_BASE_URL is left to the caller's env
// passthrough since it is not stored on AuthConfig.
func (codexHarness) AuthEnv(cfg AuthConfig) (map[string]string, error) {
	env := map[string]string{}
	if cfg.OpenAIAPIKey != "" {
		env["OPENAI_API_KEY"] = cfg.OpenAIAPIKey
	}
	return env, nil
}

// Capabilities reports codex's optional-feature matrix.
func (codexHarness) Capabilities() Capabilities {
	return Capabilities{
		SupportsResume:       false, // codex exec has no resume flag yet
		SupportsMCP:          true,
		SupportsSystemPrompt: false, // prepended into prompt instead
		EmitsUsage:           true,
		EmitsCost:            false, // surfaced via Anthropic-style total_cost_usd only when present
	}
}
