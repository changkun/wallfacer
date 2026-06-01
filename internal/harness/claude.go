package harness

import (
	"encoding/json"
	"io"
	"os"
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
//	claude --dangerously-skip-permissions [--append-system-prompt /fast]
//	       -p <prompt> --verbose --output-format stream-json
//	       [--model <model>] [--resume <session>]
//	       [--append-system-prompt <system-prompt>]
//
// The `--dangerously-skip-permissions` flag is required when claude runs in a
// piped non-TTY context: without it claude waits for interactive permission
// prompts and buffers all stream-json output until the task ends. `/fast`
// activates Claude Code's fast mode when WALLFACER_SANDBOX_FAST is "true"
// (the default) and is suppressed when explicitly disabled.
func (claudeHarness) BuildArgv(req Request) ([]string, io.Reader, error) {
	argv := []string{"--dangerously-skip-permissions"}
	if sandboxFastEnabled() {
		argv = append(argv, "--append-system-prompt", "/fast")
	}
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

// sandboxFastEnabled reports whether Claude's `/fast` mode should be enabled.
// Defaults to true; only an explicit "false" disables it.
func sandboxFastEnabled() bool {
	v := os.Getenv("WALLFACER_SANDBOX_FAST")
	return v != "false"
}

// claudeResultLine is the subset of claude's terminal stream-json object
// that downstream consumers act on.
type claudeResultLine struct {
	Result       string       `json:"result"`
	SessionID    string       `json:"session_id"`
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
// events. The terminal result line has no `type` field.
type claudeStreamLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}

// ParseEvent maps one NDJSON line of claude output to a canonical Event.
// Lines that do not match a known schema yield Event{Kind: KindUnknown}
// with Raw populated, so callers can record but not crash on schema drift.
func (claudeHarness) ParseEvent(raw []byte) (Event, error) {
	evt := Event{Raw: append([]byte(nil), raw...)}

	// Terminal result line: discriminated by presence of a top-level
	// `result` field with no `type`.
	var res claudeResultLine
	if err := json.Unmarshal(raw, &res); err == nil && res.SessionID != "" && res.Result != "" {
		evt.Kind = KindResult
		evt.SessionID = res.SessionID
		evt.StopReason = res.StopReason
		evt.Text = res.Result
		if res.Usage != nil {
			evt.Usage = &Usage{
				InputTokens:         res.Usage.InputTokens,
				OutputTokens:        res.Usage.OutputTokens,
				CacheCreationTokens: res.Usage.CacheCreationInputTokens,
				CacheReadTokens:     res.Usage.CacheReadInputTokens,
				CostUSD:             res.TotalCostUSD,
			}
		}
		if res.IsError {
			evt.Kind = KindError
		}
		return evt, nil
	}

	// Intermediate stream-json line.
	var line claudeStreamLine
	if err := json.Unmarshal(raw, &line); err == nil {
		switch line.Type {
		case "system":
			if line.Subtype == "init" {
				evt.Kind = KindSystemInit
			}
		case "assistant":
			evt.Kind = KindAssistantText
		case "user":
			evt.Kind = KindUserResult
		}
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
