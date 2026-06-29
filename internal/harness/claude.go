package harness

import (
	"encoding/json"
	"io"
)

func init() {
	Register(&claudeHarness{})
}

// claudeHarness adapts the `claude` CLI to the canonical Harness contract.
type claudeHarness struct{}

// ID returns harness.Claude.
func (claudeHarness) ID() ID { return Claude }

// BuildArgv assembles the claude argv for a Request. The argv shape is:
//
//	claude --dangerously-skip-permissions
//	       -p <prompt> --verbose --output-format stream-json
//	       [--model <model>] [--resume <session>]
//	       [--append-system-prompt <system-prompt>]
//
// The `--dangerously-skip-permissions` flag is required when claude runs in a
// piped non-TTY context: without it claude waits for interactive permission
// prompts and buffers all stream-json output until the task ends.
func (claudeHarness) BuildArgv(req Request) ([]string, io.Reader, error) {
	argv := []string{"--dangerously-skip-permissions"}
	argv = append(argv, "-p", req.Prompt, "--verbose", "--output-format", "stream-json")
	if req.Model != "" {
		argv = append(argv, "--model", req.Model)
	}
	if req.SessionID != "" {
		argv = append(argv, "--resume", req.SessionID)
	}
	if req.SystemPrompt != "" {
		argv = append(argv, "--append-system-prompt", req.SystemPrompt)
	}
	return argv, nil, nil
}

// claudeResultLine is the subset of claude's terminal stream-json object
// that downstream consumers act on.
type claudeResultLine struct {
	Result       string       `json:"result"`
	SessionID    string       `json:"session_id"`
	ThreadID     string       `json:"thread_id"`
	StopReason   string       `json:"stop_reason"`
	Subtype      string       `json:"subtype"`
	IsError      bool         `json:"is_error"`
	TotalCostUSD float64      `json:"total_cost_usd"`
	Usage        *claudeUsage `json:"usage,omitempty"`
}

type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// claudeStreamLine is the discriminator for non-terminal stream-json
// events. The terminal result line has no `type` field. The model is
// reported in two shapes: the system/init line carries it top-level
// (`model`, including a context-window variant suffix such as "[1m]"),
// while each assistant line carries the per-turn model nested under
// `message.model`.
type claudeStreamLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Model   string `json:"model"`
	Message *struct {
		Model string `json:"model"`
	} `json:"message"`
}

// ParseEvent maps one NDJSON line of claude output to a canonical Event.
// Lines that do not match a known schema yield Event{Kind: KindUnknown}
// with Raw populated, so callers can record but not crash on schema drift.
func (claudeHarness) ParseEvent(raw []byte) (Event, error) {
	evt := Event{Raw: append([]byte(nil), raw...)}

	var line claudeStreamLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return evt, nil
	}

	switch line.Type {
	case "system":
		evt.Kind = KindSystemInit
		evt.Model = line.Model
		return evt, nil
	case "assistant":
		evt.Kind = KindAssistantText
		if line.Message != nil {
			evt.Model = line.Message.Model
		}
		return evt, nil
	case "user":
		evt.Kind = KindUserResult
		return evt, nil
	}

	// Terminal result line: claude emits it either typeless or with
	// type:"result". Key on the line shape, not on a non-empty result —
	// the waiting / test-run states carry an empty result with an empty
	// stop_reason and must still be recognised as the terminal event.
	var res claudeResultLine
	if err := json.Unmarshal(raw, &res); err != nil {
		return evt, nil
	}
	if line.Type != "result" && res.SessionID == "" && res.StopReason == "" &&
		res.Result == "" && !res.IsError && res.Usage == nil {
		// A typeless line with none of the result fields is not a claude
		// result envelope (e.g. a stray object); leave it KindUnknown.
		return evt, nil
	}
	evt.Kind = KindResult
	evt.SessionID = res.SessionID
	if evt.SessionID == "" {
		evt.SessionID = res.ThreadID
	}
	evt.StopReason = res.StopReason
	evt.Subtype = res.Subtype
	evt.Text = res.Result
	// total_cost_usd is a top-level field independent of the token usage
	// object; surface it even when usage is absent so cost-budget
	// accounting still sees it.
	if res.Usage != nil || res.TotalCostUSD != 0 {
		evt.Usage = &Usage{CostUSD: res.TotalCostUSD}
		if res.Usage != nil {
			evt.Usage.InputTokens = res.Usage.InputTokens
			evt.Usage.OutputTokens = res.Usage.OutputTokens
			evt.Usage.CacheCreationTokens = res.Usage.CacheCreationInputTokens
			evt.Usage.CacheReadTokens = res.Usage.CacheReadInputTokens
		}
	}
	if res.IsError {
		evt.Kind = KindError
	}
	return evt, nil
}

// AuthEnv populates the env vars claude reads at startup. Either
// CLAUDE_CODE_OAUTH_TOKEN (long-lived subscription token) or
// ANTHROPIC_API_KEY is sufficient; both are surfaced when present so the
// caller can decide which takes precedence.
func (claudeHarness) AuthEnv(cfg AuthConfig) (map[string]string, error) {
	env := map[string]string{}
	if cfg.ClaudeOAuthToken != "" {
		env["CLAUDE_CODE_OAUTH_TOKEN"] = cfg.ClaudeOAuthToken
	}
	if cfg.AnthropicAPIKey != "" {
		env["ANTHROPIC_API_KEY"] = cfg.AnthropicAPIKey
	}
	return env, nil
}

// Capabilities reports claude's optional-feature matrix.
func (claudeHarness) Capabilities() Capabilities {
	return Capabilities{
		SupportsResume:       true,
		SupportsMCP:          true,
		SupportsSystemPrompt: true,
		EmitsUsage:           true,
		EmitsCost:            true,
	}
}
