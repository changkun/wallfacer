package harness

import (
	"strings"
	"testing"
)

func TestClaude_BuildArgv_Basic(t *testing.T) {
	h := claudeHarness{}
	argv, stdin, err := h.BuildArgv(Request{Prompt: "do the thing"})
	if err != nil {
		t.Fatalf("BuildArgv: %v", err)
	}
	if stdin != nil {
		t.Errorf("stdin = %v, want nil", stdin)
	}

	joined := strings.Join(argv, " ")
	mustContain := []string{
		"--dangerously-skip-permissions",
		"-p do the thing",
		"--verbose --output-format stream-json",
	}
	for _, want := range mustContain {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %v", want, argv)
		}
	}
}

func TestClaude_BuildArgv_ModelResumeSystemPrompt(t *testing.T) {
	h := claudeHarness{}
	argv, _, _ := h.BuildArgv(Request{
		Prompt:       "task",
		Model:        "claude-sonnet-4-6",
		SessionID:    "sess-abc",
		SystemPrompt: "be careful",
	})

	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"--model claude-sonnet-4-6",
		"--resume sess-abc",
		"--append-system-prompt be careful",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %v", want, argv)
		}
	}
}

func TestClaude_ParseEvent_ResultLine(t *testing.T) {
	raw := []byte(`{"result":"Add tests","session_id":"abc","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.05,"usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":10,"cache_read_input_tokens":5}}`)
	h := claudeHarness{}
	evt, err := h.ParseEvent(raw)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Kind != KindResult {
		t.Errorf("Kind = %v, want KindResult", evt.Kind)
	}
	if evt.SessionID != "abc" {
		t.Errorf("SessionID = %q, want abc", evt.SessionID)
	}
	if evt.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", evt.StopReason)
	}
	if evt.Text != "Add tests" {
		t.Errorf("Text = %q, want Add tests", evt.Text)
	}
	if evt.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if evt.Usage.InputTokens != 100 || evt.Usage.OutputTokens != 50 ||
		evt.Usage.CacheCreationTokens != 10 || evt.Usage.CacheReadTokens != 5 ||
		evt.Usage.CostUSD != 0.05 {
		t.Errorf("Usage mismatch: %+v", evt.Usage)
	}
}

func TestClaude_ParseEvent_IsErrorMapsToKindError(t *testing.T) {
	raw := []byte(`{"result":"rate limited","session_id":"x","stop_reason":"end_turn","is_error":true}`)
	evt, err := claudeHarness{}.ParseEvent(raw)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Kind != KindError {
		t.Errorf("Kind = %v, want KindError", evt.Kind)
	}
}

func TestClaude_ParseEvent_SystemInit(t *testing.T) {
	raw := []byte(`{"type":"system","subtype":"init","model":"claude-opus-4-8[1m]"}`)
	evt, _ := claudeHarness{}.ParseEvent(raw)
	if evt.Kind != KindSystemInit {
		t.Errorf("Kind = %v, want KindSystemInit", evt.Kind)
	}
	// The init line carries the session-primary model top-level, including
	// the context-window variant suffix; ParseEvent must lift it verbatim.
	if evt.Model != "claude-opus-4-8[1m]" {
		t.Errorf("Model = %q, want %q", evt.Model, "claude-opus-4-8[1m]")
	}
}

func TestClaude_ParseEvent_Assistant(t *testing.T) {
	raw := []byte(`{"type":"assistant","message":{"model":"claude-opus-4-8","content":[{"type":"text","text":"hello"}]}}`)
	evt, _ := claudeHarness{}.ParseEvent(raw)
	if evt.Kind != KindAssistantText {
		t.Errorf("Kind = %v, want KindAssistantText", evt.Kind)
	}
	// The per-turn model rides nested under message.model.
	if evt.Model != "claude-opus-4-8" {
		t.Errorf("Model = %q, want %q", evt.Model, "claude-opus-4-8")
	}
}

func TestClaude_ParseEvent_NoModel(t *testing.T) {
	// An assistant line with no model field leaves Model empty rather than
	// inventing a value.
	raw := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`)
	evt, _ := claudeHarness{}.ParseEvent(raw)
	if evt.Model != "" {
		t.Errorf("Model = %q, want empty", evt.Model)
	}
}

func TestClaude_ParseEvent_Unknown(t *testing.T) {
	raw := []byte(`{"type":"future-event","subtype":"unknown"}`)
	evt, err := claudeHarness{}.ParseEvent(raw)
	if err != nil {
		t.Fatalf("ParseEvent on unknown line returned error: %v", err)
	}
	if evt.Kind != KindUnknown {
		t.Errorf("Kind = %v, want KindUnknown", evt.Kind)
	}
	if len(evt.Raw) == 0 {
		t.Error("Raw should be preserved for unknown lines")
	}
}

func TestClaude_AuthEnv(t *testing.T) {
	h := claudeHarness{}
	env, err := h.AuthEnv(AuthConfig{
		ClaudeOAuthToken: "oauth-tok",
		AnthropicAPIKey:  "sk-ant-k",
	})
	if err != nil {
		t.Fatalf("AuthEnv: %v", err)
	}
	if env["CLAUDE_CODE_OAUTH_TOKEN"] != "oauth-tok" || env["ANTHROPIC_API_KEY"] != "sk-ant-k" {
		t.Errorf("AuthEnv = %v", env)
	}
}

func TestClaude_Capabilities(t *testing.T) {
	caps := claudeHarness{}.Capabilities()
	if !caps.SupportsResume || !caps.SupportsMCP || !caps.SupportsSystemPrompt ||
		!caps.EmitsUsage || !caps.EmitsCost {
		t.Errorf("expected all-true capabilities, got %+v", caps)
	}
}

func TestClaude_RegisteredAtInit(t *testing.T) {
	h, ok := Lookup(Claude)
	if !ok {
		t.Fatal("Claude not registered")
	}
	if h.ID() != Claude {
		t.Errorf("Lookup(Claude).ID() = %q, want %q", h.ID(), Claude)
	}
}
