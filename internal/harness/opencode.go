package harness

import (
	"encoding/json"
	"io"
)

func init() {
	Register(&openCodeHarness{})
}

// openCodeHarness adapts the `opencode` CLI to the canonical Harness
// contract. opencode's `run --format json` mode emits NDJSON, one object per
// line, shaped {type, timestamp, sessionID, part?|error?}. Crucially it emits
// no terminal result event: the run loop simply breaks when the session goes
// idle. The host launcher therefore aggregates the final text + token usage
// and appends a synthesized {"type":"result", ...} line, which ParseEvent
// recognises as KindResult. This mirrors the codex output-last-message path.
type openCodeHarness struct{}

// ID returns harness.OpenCode.
func (openCodeHarness) ID() ID { return OpenCode }

// BuildArgv assembles the opencode argv for a Request:
//
//	opencode run --format json
//	             [--dir <cwd>] [--model <provider/model>] [--session <id>]
//	             [--agent plan]                     (Permission ReadOnly)
//	             [--dangerously-skip-permissions]   (Permission Edit | Full)
//	             <prompt>
//
// opencode has no append-system-prompt flag (Capabilities.SupportsSystemPrompt
// == false), so SystemPrompt is prepended into the positional prompt. The
// read-only "plan" agent maps ReadOnly; write modes auto-approve permissions
// so headless edits actually apply (without the flag opencode blocks on an
// interactive approval prompt and produces no commit).
//
// Note: opencode 1.17 has no `--mode build|plan` flag — build/plan are agents,
// selected via `--agent`. The default (build) agent is used for write modes.
func (openCodeHarness) BuildArgv(req Request) ([]string, io.Reader, error) {
	prompt := req.Prompt
	if req.SystemPrompt != "" {
		prompt = req.SystemPrompt + "\n\n---\n\n" + prompt
	}

	argv := []string{"run", "--format", "json"}
	if req.Cwd != "" {
		argv = append(argv, "--dir", req.Cwd)
	}
	if req.Model != "" {
		argv = append(argv, "--model", req.Model)
	}
	if req.SessionID != "" {
		argv = append(argv, "--session", req.SessionID)
	}
	switch req.Permission {
	case PermissionReadOnly:
		argv = append(argv, "--agent", "plan")
	case PermissionEdit, PermissionFull:
		argv = append(argv, "--dangerously-skip-permissions")
	}
	argv = append(argv, prompt)
	return argv, nil, nil
}

// openCodeTokens is opencode's token-accounting shape, carried on
// step-finish parts and on the synthesized result line's usage object.
type openCodeTokens struct {
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
	Cache     struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

// openCodeToolState is the state object on a tool part. emit("tool_use") fires
// once the tool has completed or errored, so status is "completed" / "error"
// in practice; pending/running are handled defensively for schema drift.
type openCodeToolState struct {
	Status string          `json:"status"`
	Input  json.RawMessage `json:"input"`
	Output string          `json:"output"`
	Error  string          `json:"error"`
}

// openCodePart is the message part nested under a real opencode event line.
type openCodePart struct {
	Type   string             `json:"type"`
	Text   string             `json:"text"`
	Tool   string             `json:"tool"`
	CallID string             `json:"callID"`
	State  *openCodeToolState `json:"state"`
}

// openCodeError is the session error payload on an "error" event.
type openCodeError struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

// openCodeLine captures the fields harness.OpenCode sniffs from opencode's
// run --format json output. Top-level Type is the emit() type (text, reasoning,
// tool_use, step_start, step_finish, error). The synthesized terminal line
// uses Type "result" and carries Result / Usage / Cost / IsError at top level.
// Unknown fields are ignored.
type openCodeLine struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionID"`
	Part      *openCodePart   `json:"part"`
	Error     *openCodeError  `json:"error"`
	Result    string          `json:"result"`
	IsError   bool            `json:"is_error"`
	StopReason string         `json:"stop_reason"`
	Usage     *openCodeTokens `json:"usage"`
	Cost      float64         `json:"cost"`
}

// ParseEvent maps one NDJSON line of opencode output to a canonical Event.
// Recognised top-level types: text / reasoning → KindAssistantText,
// tool_use → KindToolCall{Start,End}, step_start → KindSystemInit, error →
// KindError, and the synthesized result → KindResult. Everything else
// (step_finish, future event types) yields KindUnknown with Raw preserved,
// matching the spec's tolerance for opencode's less-standardised schema.
func (openCodeHarness) ParseEvent(raw []byte) (Event, error) {
	evt := Event{Raw: append([]byte(nil), raw...)}

	var line openCodeLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return evt, nil
	}
	evt.SessionID = line.SessionID

	switch line.Type {
	case "text":
		evt.Kind = KindAssistantText
		if line.Part != nil {
			evt.Text = line.Part.Text
		}
	case "reasoning":
		// Thinking output. Surface as an assistant event but leave Text empty
		// so the runner's result fallback never picks a reasoning block over
		// the final answer.
		evt.Kind = KindAssistantText
	case "tool_use":
		tc := &ToolCall{}
		status := ""
		if line.Part != nil {
			tc.ID = line.Part.CallID
			tc.Name = line.Part.Tool
			if line.Part.State != nil {
				status = line.Part.State.Status
				tc.Input = line.Part.State.Input
				tc.Error = line.Part.State.Error
				if line.Part.State.Output != "" {
					if out, err := json.Marshal(line.Part.State.Output); err == nil {
						tc.Output = out
					}
				}
			}
		}
		evt.Tool = tc
		switch status {
		case "pending", "running":
			evt.Kind = KindToolCallStart
		default:
			evt.Kind = KindToolCallEnd
		}
	case "step_start":
		// opencode emits no session-start event carrying the id; the first
		// step_start is the earliest signal, so surface it as KindSystemInit
		// (it already carries sessionID) for the kill-before-result path.
		evt.Kind = KindSystemInit
	case "error":
		evt.Kind = KindError
		if line.Error != nil {
			evt.Subtype = line.Error.Name
			evt.Text = string(line.Error.Data)
		}
	case "result":
		// Synthesized terminal line appended by the host launcher (opencode
		// emits no native result event).
		evt.Kind = KindResult
		evt.StopReason = line.StopReason
		if evt.StopReason == "" {
			evt.StopReason = "end_turn"
		}
		evt.Text = line.Result
		if line.Usage != nil || line.Cost != 0 {
			u := &Usage{CostUSD: line.Cost}
			if line.Usage != nil {
				u.InputTokens = line.Usage.Input
				u.OutputTokens = line.Usage.Output
				u.CacheReadTokens = line.Usage.Cache.Read
				u.CacheCreationTokens = line.Usage.Cache.Write
			}
			evt.Usage = u
		}
		if line.IsError {
			evt.Kind = KindError
		}
	}
	return evt, nil
}

// AuthEnv populates the env vars opencode reads at startup. opencode manages
// per-provider credentials itself (`opencode auth login`, stored in its own
// config), so wallfacer sets no provider keys here. OPENCODE_SERVER_PASSWORD
// is surfaced for the future `opencode run --attach` warm-start path.
func (openCodeHarness) AuthEnv(cfg AuthConfig) (map[string]string, error) {
	env := map[string]string{}
	if cfg.OpenCodeServerPassword != "" {
		env["OPENCODE_SERVER_PASSWORD"] = cfg.OpenCodeServerPassword
	}
	return env, nil
}

// Capabilities reports opencode's optional-feature matrix.
func (openCodeHarness) Capabilities() Capabilities {
	return Capabilities{
		SupportsResume:       true,
		SupportsMCP:          true,
		SupportsSystemPrompt: false, // no append-system-prompt flag; we prepend
		EmitsUsage:           true,
		EmitsCost:            false, // per-step cost is unreliable across providers
		NeedsTTY:             false,
	}
}
