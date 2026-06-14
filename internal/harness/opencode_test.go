package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCode_BuildArgv_Basic(t *testing.T) {
	argv, stdin, err := openCodeHarness{}.BuildArgv(Request{Prompt: "do it"})
	if err != nil {
		t.Fatalf("BuildArgv: %v", err)
	}
	if stdin != nil {
		t.Errorf("stdin = %v, want nil", stdin)
	}
	if argv[0] != "run" {
		t.Errorf("first arg = %q, want run", argv[0])
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--format json") {
		t.Errorf("argv must always request json: %v", argv)
	}
	if argv[len(argv)-1] != "do it" {
		t.Errorf("prompt should be last arg, got %q", argv[len(argv)-1])
	}
}

func TestOpenCode_BuildArgv_PermissionModes(t *testing.T) {
	tests := []struct {
		name       string
		perm       Permission
		wantSubstr string
		notSubstr  string
	}{
		{"readonly", PermissionReadOnly, "--agent plan", "--dangerously-skip-permissions"},
		{"edit", PermissionEdit, "--dangerously-skip-permissions", "--agent plan"},
		{"full", PermissionFull, "--dangerously-skip-permissions", "--agent plan"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			argv, _, _ := openCodeHarness{}.BuildArgv(Request{Prompt: "x", Permission: tc.perm})
			joined := strings.Join(argv, " ")
			if !strings.Contains(joined, tc.wantSubstr) {
				t.Errorf("argv missing %q: %v", tc.wantSubstr, argv)
			}
			if strings.Contains(joined, tc.notSubstr) {
				t.Errorf("argv should not contain %q: %v", tc.notSubstr, argv)
			}
		})
	}
}

func TestOpenCode_BuildArgv_DirModelSession(t *testing.T) {
	argv, _, _ := openCodeHarness{}.BuildArgv(Request{
		Prompt:    "task",
		Cwd:       "/work/repo",
		Model:     "anthropic/claude-sonnet-4-6",
		SessionID: "ses_99",
	})
	joined := strings.Join(argv, " ")
	for _, want := range []string{"--dir /work/repo", "--model anthropic/claude-sonnet-4-6", "--session ses_99"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %v", want, argv)
		}
	}
}

func TestOpenCode_BuildArgv_SystemPromptPrepended(t *testing.T) {
	argv, _, _ := openCodeHarness{}.BuildArgv(Request{
		Prompt:       "task body",
		SystemPrompt: "be careful",
	})
	last := argv[len(argv)-1]
	if !strings.Contains(last, "be careful") || !strings.Contains(last, "task body") {
		t.Errorf("system prompt should be prepended into prompt; got %q", last)
	}
	// SystemPrompt must reach the prompt, not a flag — opencode has no
	// append-system-prompt flag.
	if strings.Contains(strings.Join(argv, " "), "--append-system-prompt") {
		t.Errorf("opencode has no append-system-prompt flag: %v", argv)
	}
}

func TestOpenCode_ParseEvent_Success(t *testing.T) {
	evts := parseOpenCodeFixture(t, "success.jsonl")

	// step_start → init, text → assistant, step_finish → unknown, result → result.
	if evts[0].Kind != KindSystemInit {
		t.Errorf("line 0 Kind = %v, want KindSystemInit", evts[0].Kind)
	}
	if evts[1].Kind != KindAssistantText || evts[1].Text != "Hello! Done." {
		t.Errorf("line 1 = %+v, want assistant text 'Hello! Done.'", evts[1])
	}
	if evts[2].Kind != KindUnknown {
		t.Errorf("line 2 (step_finish) Kind = %v, want KindUnknown", evts[2].Kind)
	}
	term := evts[len(evts)-1]
	if term.Kind != KindResult {
		t.Fatalf("terminal Kind = %v, want KindResult", term.Kind)
	}
	if term.Text != "Hello! Done." {
		t.Errorf("terminal Text = %q", term.Text)
	}
	if term.SessionID != "ses_abc123" {
		t.Errorf("terminal SessionID = %q", term.SessionID)
	}
	if term.Usage == nil {
		t.Fatal("terminal Usage is nil")
	}
	if term.Usage.InputTokens != 1200 || term.Usage.OutputTokens != 85 {
		t.Errorf("Usage tokens = %+v", term.Usage)
	}
	if term.Usage.CacheReadTokens != 900 || term.Usage.CacheCreationTokens != 300 {
		t.Errorf("cache tokens = %+v", term.Usage)
	}
}

func TestOpenCode_ParseEvent_ToolCalls(t *testing.T) {
	evts := parseOpenCodeFixture(t, "tool-calls.jsonl")

	var starts, ends, toolErrs int
	for _, e := range evts {
		switch e.Kind {
		case KindToolCallStart:
			starts++
		case KindToolCallEnd:
			ends++
			if e.Tool != nil && e.Tool.Error != "" {
				toolErrs++
			}
		}
	}
	// Both tool_use events fire at completion → KindToolCallEnd; one errored.
	if ends != 2 {
		t.Errorf("tool-call ends = %d, want 2", ends)
	}
	if starts != 0 {
		t.Errorf("tool-call starts = %d, want 0 (opencode emits tool_use on completion)", starts)
	}
	if toolErrs != 1 {
		t.Errorf("tool errors = %d, want 1", toolErrs)
	}
	// First tool carries name + input.
	for _, e := range evts {
		if e.Kind == KindToolCallEnd && e.Tool != nil && e.Tool.ID == "call_ls" {
			if e.Tool.Name != "bash" {
				t.Errorf("tool name = %q, want bash", e.Tool.Name)
			}
			if len(e.Tool.Input) == 0 {
				t.Error("tool input not populated")
			}
		}
	}
	if term := evts[len(evts)-1]; term.Kind != KindResult {
		t.Errorf("terminal Kind = %v, want KindResult", term.Kind)
	}
}

func TestOpenCode_ParseEvent_Error(t *testing.T) {
	raw := []byte(`{"type":"error","sessionID":"ses_x","error":{"name":"ProviderAuthError","data":{"providerID":"anthropic"}}}`)
	evt, err := openCodeHarness{}.ParseEvent(raw)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Kind != KindError {
		t.Errorf("Kind = %v, want KindError", evt.Kind)
	}
	if evt.Subtype != "ProviderAuthError" {
		t.Errorf("Subtype = %q, want ProviderAuthError", evt.Subtype)
	}
}

func TestOpenCode_ParseEvent_ResultIsError(t *testing.T) {
	raw := []byte(`{"type":"result","sessionID":"s","result":"","is_error":true,"stop_reason":"error_during_execution"}`)
	evt, _ := openCodeHarness{}.ParseEvent(raw)
	if evt.Kind != KindError {
		t.Errorf("Kind = %v, want KindError for is_error result", evt.Kind)
	}
	if evt.StopReason != "error_during_execution" {
		t.Errorf("StopReason = %q", evt.StopReason)
	}
}

func TestOpenCode_ParseEvent_Unknown(t *testing.T) {
	raw := []byte(`{"type":"file_edited","sessionID":"s","properties":{}}`)
	evt, err := openCodeHarness{}.ParseEvent(raw)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Kind != KindUnknown {
		t.Errorf("Kind = %v, want KindUnknown for unrecognised event", evt.Kind)
	}
	if string(evt.Raw) != string(raw) {
		t.Errorf("Raw not preserved on unknown event")
	}
	// SessionID is still extracted so the runner can recover it.
	if evt.SessionID != "s" {
		t.Errorf("SessionID = %q, want s", evt.SessionID)
	}
}

func TestOpenCode_ParseEvent_Garbage(t *testing.T) {
	evt, err := openCodeHarness{}.ParseEvent([]byte("not json"))
	if err != nil {
		t.Fatalf("ParseEvent should not error on garbage: %v", err)
	}
	if evt.Kind != KindUnknown {
		t.Errorf("Kind = %v, want KindUnknown", evt.Kind)
	}
}

func TestOpenCode_AuthEnv(t *testing.T) {
	// Provider auth is managed by opencode itself; only the server password
	// is surfaced, and only when set.
	env, err := openCodeHarness{}.AuthEnv(AuthConfig{})
	if err != nil {
		t.Fatalf("AuthEnv: %v", err)
	}
	if len(env) != 0 {
		t.Errorf("AuthEnv with empty config = %v, want empty", env)
	}
	env, _ = openCodeHarness{}.AuthEnv(AuthConfig{OpenCodeServerPassword: "pw"})
	if env["OPENCODE_SERVER_PASSWORD"] != "pw" {
		t.Errorf("AuthEnv = %v, want OPENCODE_SERVER_PASSWORD=pw", env)
	}
}

func TestOpenCode_Capabilities(t *testing.T) {
	caps := openCodeHarness{}.Capabilities()
	if !caps.SupportsResume {
		t.Error("opencode should advertise resume")
	}
	if caps.SupportsSystemPrompt {
		t.Error("opencode has no append-system-prompt flag")
	}
	if !caps.EmitsUsage {
		t.Error("opencode should advertise usage")
	}
	if caps.EmitsCost {
		t.Error("opencode cost is not surfaced reliably; EmitsCost should be false")
	}
}

func TestOpenCode_RegisteredAtInit(t *testing.T) {
	h, ok := Lookup(OpenCode)
	if !ok {
		t.Fatal("OpenCode not registered")
	}
	if h.ID() != OpenCode {
		t.Errorf("Lookup(OpenCode).ID() = %q", h.ID())
	}
}

// parseOpenCodeFixture reads an NDJSON fixture and returns the parsed events,
// one per non-empty line.
func parseOpenCodeFixture(t *testing.T, name string) []Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "opencode", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var evts []Event
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		evt, err := openCodeHarness{}.ParseEvent([]byte(line))
		if err != nil {
			t.Fatalf("ParseEvent(%q): %v", line, err)
		}
		evts = append(evts, evt)
	}
	return evts
}
