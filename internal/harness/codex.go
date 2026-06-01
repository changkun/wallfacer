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
//	           [--config model_reasoning_effort="low"]
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
	if sandboxFastEnabled() {
		argv = append(argv, "--config", `model_reasoning_effort="low"`)
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

// codexEventLine captures the fields harness.Codex sniffs from codex's
// native JSON event stream. Unknown fields are ignored.
type codexEventLine struct {
	Type         string   `json:"type"`
	SessionID    string   `json:"session_id,omitempty"`
	StopReason   string   `json:"stop_reason,omitempty"`
	TotalCostUSD *float64 `json:"total_cost_usd,omitempty"`
	Usage        *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CachedInputTokens        int `json:"cached_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage,omitempty"`
}

// ParseEvent maps one NDJSON line of codex output to a canonical Event.
// Codex emits dot-namespaced event types (thread.*, turn.*, item.*); this
// adapter recognises the high-leverage ones and falls through to
// KindUnknown for the rest.
func (codexHarness) ParseEvent(raw []byte) (Event, error) {
	evt := Event{Raw: append([]byte(nil), raw...)}

	var line codexEventLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return evt, nil
	}

	if line.SessionID != "" {
		evt.SessionID = line.SessionID
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
		if line.Usage != nil {
			cacheRead := line.Usage.CacheReadInputTokens
			if cacheRead == 0 {
				cacheRead = line.Usage.CachedInputTokens
			}
			evt.Usage = &Usage{
				InputTokens:         line.Usage.InputTokens,
				OutputTokens:        line.Usage.OutputTokens,
				CacheCreationTokens: line.Usage.CacheCreationInputTokens,
				CacheReadTokens:     cacheRead,
			}
			if line.TotalCostUSD != nil {
				evt.Usage.CostUSD = *line.TotalCostUSD
			}
		}
	case strings.HasPrefix(line.Type, "item."):
		// Items cover assistant messages, tool calls, file changes, etc.
		// v1 maps the family to KindAssistantText; downstream consumers
		// inspect Raw for the specific subtype until a richer mapping
		// is justified.
		evt.Kind = KindAssistantText
	case line.Type == "turn.failed" || line.Type == "error":
		evt.Kind = KindError
	}
	return evt, nil
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
