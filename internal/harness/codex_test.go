package harness

import (
	"strings"
	"testing"
)

func TestCodex_BuildArgv_Basic(t *testing.T) {
	h := codexHarness{}
	argv, stdin, err := h.BuildArgv(Request{Prompt: "do it"})
	if err != nil {
		t.Fatalf("BuildArgv: %v", err)
	}
	if stdin != nil {
		t.Errorf("stdin = %v, want nil", stdin)
	}

	if argv[0] != "exec" {
		t.Errorf("first arg = %q, want exec", argv[0])
	}

	joined := strings.Join(argv, " ")
	mustContain := []string{
		"--full-auto",
		"--sandbox workspace-write",
		"--skip-git-repo-check",
		"--json",
		"--color never",
	}
	for _, want := range mustContain {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %v", want, argv)
		}
	}
	if argv[len(argv)-1] != "do it" {
		t.Errorf("prompt should be last arg, got %q", argv[len(argv)-1])
	}
}

func TestCodex_BuildArgv_ModelAndSystemPrompt(t *testing.T) {
	argv, _, _ := codexHarness{}.BuildArgv(Request{
		Prompt:       "task",
		Model:        "gpt-5",
		SystemPrompt: "be careful",
	})

	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--model gpt-5") {
		t.Errorf("argv missing model: %v", argv)
	}
	// SystemPrompt prepended into prompt; the last arg holds the full thing.
	if !strings.Contains(argv[len(argv)-1], "be careful") {
		t.Errorf("system prompt should be prepended into prompt; got %q", argv[len(argv)-1])
	}
	if !strings.Contains(argv[len(argv)-1], "task") {
		t.Errorf("prompt body should be present; got %q", argv[len(argv)-1])
	}
}

func TestCodex_BuildArgv_IgnoresSessionID(t *testing.T) {
	argv, _, _ := codexHarness{}.BuildArgv(Request{
		Prompt:    "x",
		SessionID: "ignored",
	})
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "ignored") || strings.Contains(joined, "--resume") {
		t.Errorf("codex should not honor SessionID: %v", argv)
	}
}

func TestCodex_ParseEvent_TurnCompleted(t *testing.T) {
	raw := []byte(`{"type":"turn.completed","session_id":"sess-1","stop_reason":"end_turn","usage":{"input_tokens":100,"output_tokens":50,"cached_input_tokens":7,"cache_creation_input_tokens":3}}`)
	evt, err := codexHarness{}.ParseEvent(raw)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Kind != KindResult {
		t.Errorf("Kind = %v, want KindResult", evt.Kind)
	}
	if evt.SessionID != "sess-1" {
		t.Errorf("SessionID = %q", evt.SessionID)
	}
	if evt.StopReason != "end_turn" {
		t.Errorf("StopReason = %q", evt.StopReason)
	}
	if evt.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if evt.Usage.InputTokens != 100 || evt.Usage.OutputTokens != 50 {
		t.Errorf("Usage tokens = %+v", evt.Usage)
	}
	if evt.Usage.CacheReadTokens != 7 {
		t.Errorf("CacheReadTokens = %d, want 7 (from cached_input_tokens)", evt.Usage.CacheReadTokens)
	}
	if evt.Usage.CacheCreationTokens != 3 {
		t.Errorf("CacheCreationTokens = %d, want 3", evt.Usage.CacheCreationTokens)
	}
}

func TestCodex_ParseEvent_ThreadStarted(t *testing.T) {
	raw := []byte(`{"type":"thread.started","session_id":"abc"}`)
	evt, _ := codexHarness{}.ParseEvent(raw)
	if evt.Kind != KindSystemInit {
		t.Errorf("Kind = %v, want KindSystemInit", evt.Kind)
	}
	if evt.SessionID != "abc" {
		t.Errorf("SessionID = %q", evt.SessionID)
	}
}

func TestCodex_ParseEvent_Item(t *testing.T) {
	raw := []byte(`{"type":"item.agent_message","content":"hello"}`)
	evt, _ := codexHarness{}.ParseEvent(raw)
	if evt.Kind != KindAssistantText {
		t.Errorf("Kind = %v, want KindAssistantText for item.* events", evt.Kind)
	}
}

func TestCodex_ParseEvent_TurnFailed(t *testing.T) {
	raw := []byte(`{"type":"turn.failed","session_id":"x"}`)
	evt, _ := codexHarness{}.ParseEvent(raw)
	if evt.Kind != KindError {
		t.Errorf("Kind = %v, want KindError", evt.Kind)
	}
}

func TestCodex_ParseEvent_Unknown(t *testing.T) {
	raw := []byte(`{"type":"thread.future","subtype":"unknown"}`)
	evt, err := codexHarness{}.ParseEvent(raw)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Kind != KindUnknown {
		t.Errorf("Kind = %v, want KindUnknown", evt.Kind)
	}
}

func TestCodex_AuthEnv(t *testing.T) {
	env, err := codexHarness{}.AuthEnv(AuthConfig{OpenAIAPIKey: "sk-test"})
	if err != nil {
		t.Fatalf("AuthEnv: %v", err)
	}
	if env["OPENAI_API_KEY"] != "sk-test" {
		t.Errorf("AuthEnv = %v", env)
	}
}

func TestCodex_Capabilities(t *testing.T) {
	caps := codexHarness{}.Capabilities()
	if caps.SupportsResume {
		t.Error("Codex should not advertise resume")
	}
	if caps.SupportsSystemPrompt {
		t.Error("Codex should not advertise system-prompt flag")
	}
	if !caps.EmitsUsage {
		t.Error("Codex should advertise usage")
	}
}

func TestCodex_RegisteredAtInit(t *testing.T) {
	h, ok := Lookup(Codex)
	if !ok {
		t.Fatal("Codex not registered")
	}
	if h.ID() != Codex {
		t.Errorf("Lookup(Codex).ID() = %q", h.ID())
	}
}
