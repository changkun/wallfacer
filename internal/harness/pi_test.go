package harness

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestPi_BuildArgv_Basic(t *testing.T) {
	argv, stdin, err := piHarness{}.BuildArgv(Request{Prompt: "list files"})
	if err != nil {
		t.Fatalf("BuildArgv: %v", err)
	}
	if stdin != nil {
		t.Errorf("stdin = %v, want nil", stdin)
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{"-p", "--mode json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %v", want, argv)
		}
	}
	// -p is a boolean flag; the prompt is the final positional arg, not the
	// value of -p.
	if argv[len(argv)-1] != "list files" {
		t.Errorf("prompt should be the final positional arg; got %v", argv)
	}
	// The zero-value Permission is ReadOnly, which maps to --tools Read. The
	// executor raises this to a writable permission for real task runs.
	if !strings.Contains(joined, "--tools Read") {
		t.Errorf("zero-value (ReadOnly) permission should set --tools Read: %v", argv)
	}
}

func TestPi_BuildArgv_ProviderModelSplit(t *testing.T) {
	argv, _, _ := piHarness{}.BuildArgv(Request{
		Prompt: "x",
		Model:  "anthropic/claude-sonnet-4-6",
	})
	joined := strings.Join(argv, " ")
	// A "provider/model" string splits into the two-flag form.
	if !strings.Contains(joined, "--provider anthropic") {
		t.Errorf("provider not split out: %v", argv)
	}
	if !strings.Contains(joined, "--model claude-sonnet-4-6") {
		t.Errorf("model not split out: %v", argv)
	}
}

func TestPi_BuildArgv_BareModelNoProvider(t *testing.T) {
	argv, _, _ := piHarness{}.BuildArgv(Request{Prompt: "x", Model: "gemini-2.5-pro"})
	joined := strings.Join(argv, " ")
	// No "/" ⇒ pass --model alone and let pi resolve the provider.
	if !strings.Contains(joined, "--model gemini-2.5-pro") {
		t.Errorf("bare model not passed: %v", argv)
	}
	if strings.Contains(joined, "--provider") {
		t.Errorf("bare model must not emit --provider: %v", argv)
	}
}

func TestPi_BuildArgv_Resume(t *testing.T) {
	argv, _, _ := piHarness{}.BuildArgv(Request{Prompt: "x", SessionID: "sess-42"})
	if !strings.Contains(strings.Join(argv, " "), "--session sess-42") {
		t.Errorf("argv missing --session sess-42: %v", argv)
	}
}

func TestPi_BuildArgv_PermissionReadOnly(t *testing.T) {
	argv, _, _ := piHarness{}.BuildArgv(Request{Prompt: "x", Permission: PermissionReadOnly})
	if !strings.Contains(strings.Join(argv, " "), "--tools Read") {
		t.Errorf("ReadOnly should set --tools Read: %v", argv)
	}
}

func TestPi_BuildArgv_PermissionEdit(t *testing.T) {
	argv, _, _ := piHarness{}.BuildArgv(Request{Prompt: "x", Permission: PermissionEdit})
	if !strings.Contains(strings.Join(argv, " "), "--tools Read,Write,Edit") {
		t.Errorf("Edit should set --tools Read,Write,Edit: %v", argv)
	}
}

func TestPi_BuildArgv_PermissionFull(t *testing.T) {
	argv, _, _ := piHarness{}.BuildArgv(Request{Prompt: "x", Permission: PermissionFull})
	if strings.Contains(strings.Join(argv, " "), "--tools") {
		t.Errorf("Full should omit --tools (all default tools enabled): %v", argv)
	}
}

func TestPi_BuildArgv_SystemPromptPrepended(t *testing.T) {
	argv, _, _ := piHarness{}.BuildArgv(Request{Prompt: "do it", SystemPrompt: "be careful"})
	prompt := argv[len(argv)-1]
	if !strings.Contains(prompt, "be careful") || !strings.Contains(prompt, "do it") {
		t.Errorf("system prompt should be prepended into the positional prompt; got %q", prompt)
	}
	if strings.Contains(strings.Join(argv, " "), "--system-prompt") {
		t.Errorf("pi harness should not emit --system-prompt in v1: %v", argv)
	}
}

func TestPi_ParseEvent_SessionInit(t *testing.T) {
	raw := []byte(`{"type":"session","version":3,"id":"sess-1","cwd":"/tmp"}`)
	evt, _ := piHarness{}.ParseEvent(raw)
	if evt.Kind != KindSystemInit {
		t.Errorf("Kind = %v, want KindSystemInit", evt.Kind)
	}
	if evt.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1 (from header id)", evt.SessionID)
	}
}

func TestPi_ParseEvent_AssistantText(t *testing.T) {
	raw := []byte(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}],"stopReason":"stop"}}`)
	evt, _ := piHarness{}.ParseEvent(raw)
	if evt.Kind != KindAssistantText {
		t.Errorf("Kind = %v, want KindAssistantText", evt.Kind)
	}
	if evt.Text != "hello world" {
		t.Errorf("Text = %q, want %q", evt.Text, "hello world")
	}
}

func TestPi_ParseEvent_NonAssistantMessageEndIgnored(t *testing.T) {
	// A user message_end has no assistant prose to surface.
	raw := []byte(`{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`)
	evt, _ := piHarness{}.ParseEvent(raw)
	if evt.Kind != KindUnknown {
		t.Errorf("Kind = %v, want KindUnknown for a user message_end", evt.Kind)
	}
}

func TestPi_ParseEvent_ToolExecutionStart(t *testing.T) {
	raw := []byte(`{"type":"tool_execution_start","toolCallId":"c1","toolName":"bash","args":{"command":"ls"}}`)
	evt, _ := piHarness{}.ParseEvent(raw)
	if evt.Kind != KindToolCallStart {
		t.Errorf("Kind = %v, want KindToolCallStart", evt.Kind)
	}
	if evt.Tool == nil || evt.Tool.ID != "c1" || evt.Tool.Name != "bash" {
		t.Fatalf("Tool = %+v", evt.Tool)
	}
	if !strings.Contains(string(evt.Tool.Input), "ls") {
		t.Errorf("Tool.Input should carry args: %s", evt.Tool.Input)
	}
}

func TestPi_ParseEvent_ToolExecutionEnd(t *testing.T) {
	raw := []byte(`{"type":"tool_execution_end","toolCallId":"c1","toolName":"bash","result":{"output":"ok","exitCode":0},"isError":false}`)
	evt, _ := piHarness{}.ParseEvent(raw)
	if evt.Kind != KindToolCallEnd {
		t.Errorf("Kind = %v, want KindToolCallEnd", evt.Kind)
	}
	if evt.Tool == nil || len(evt.Tool.Output) == 0 {
		t.Errorf("completed tool call should carry Output: %+v", evt.Tool)
	}
	if evt.Tool.Error != "" {
		t.Errorf("non-error tool end should leave Error empty: %q", evt.Tool.Error)
	}
}

func TestPi_ParseEvent_ToolExecutionEndError(t *testing.T) {
	raw := []byte(`{"type":"tool_execution_end","toolCallId":"c1","toolName":"bash","result":{"output":"boom"},"isError":true}`)
	evt, _ := piHarness{}.ParseEvent(raw)
	if evt.Kind != KindToolCallEnd {
		t.Errorf("Kind = %v, want KindToolCallEnd", evt.Kind)
	}
	if evt.Tool == nil || evt.Tool.Error == "" {
		t.Errorf("error tool end should populate Error: %+v", evt.Tool)
	}
}

func TestPi_ParseEvent_AgentEndResultWithUsage(t *testing.T) {
	raw := []byte(`{"type":"agent_end","messages":[` +
		`{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":[{"type":"text","text":"first"}],"usage":{"input":10,"output":2,"cacheRead":0,"cacheWrite":0},"stopReason":"toolUse"},` +
		`{"role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input":100,"output":50,"cacheRead":7,"cacheWrite":3},"stopReason":"stop"}` +
		`]}`)
	evt, _ := piHarness{}.ParseEvent(raw)
	if evt.Kind != KindResult {
		t.Errorf("Kind = %v, want KindResult", evt.Kind)
	}
	if evt.Text != "done" {
		t.Errorf("Text = %q, want %q (last assistant message)", evt.Text, "done")
	}
	if evt.StopReason != "stop" {
		t.Errorf("StopReason = %q, want stop", evt.StopReason)
	}
	if evt.Usage == nil {
		t.Fatal("Usage is nil")
	}
	// Usage comes from the LAST assistant message, not the tool-use turn.
	if evt.Usage.InputTokens != 100 || evt.Usage.OutputTokens != 50 {
		t.Errorf("Usage tokens = %+v, want input=100 output=50", evt.Usage)
	}
	if evt.Usage.CacheReadTokens != 7 {
		t.Errorf("CacheReadTokens = %d, want 7", evt.Usage.CacheReadTokens)
	}
	if evt.Usage.CacheCreationTokens != 3 {
		t.Errorf("CacheCreationTokens = %d, want 3 (from cacheWrite)", evt.Usage.CacheCreationTokens)
	}
}

func TestPi_ParseEvent_AssistantStringContentIgnored(t *testing.T) {
	// Some messages carry content as a bare string rather than a block array;
	// piMessageText must tolerate that and yield no prose.
	raw := []byte(`{"type":"message_end","message":{"role":"assistant","content":"plain string","stopReason":"stop"}}`)
	evt, _ := piHarness{}.ParseEvent(raw)
	if evt.Kind != KindAssistantText {
		t.Errorf("Kind = %v, want KindAssistantText", evt.Kind)
	}
	if evt.Text != "" {
		t.Errorf("string content should not yield prose; got %q", evt.Text)
	}
}

func TestPi_ParseEvent_AgentEndNoAssistant(t *testing.T) {
	// agent_end with no assistant message still terminates the run; it just
	// carries no text or usage.
	raw := []byte(`{"type":"agent_end","messages":[{"role":"user","content":"hi"}]}`)
	evt, _ := piHarness{}.ParseEvent(raw)
	if evt.Kind != KindResult {
		t.Errorf("Kind = %v, want KindResult", evt.Kind)
	}
	if evt.Text != "" || evt.Usage != nil {
		t.Errorf("no assistant message should mean no text/usage; got text=%q usage=%+v", evt.Text, evt.Usage)
	}
}

func TestPi_ParseEvent_AgentEndError(t *testing.T) {
	raw := []byte(`{"type":"agent_end","messages":[{"role":"assistant","content":[],"stopReason":"error"}]}`)
	evt, _ := piHarness{}.ParseEvent(raw)
	if evt.Kind != KindError {
		t.Errorf("Kind = %v, want KindError for stopReason=error", evt.Kind)
	}
}

func TestPi_ParseEvent_Unknown(t *testing.T) {
	evt, err := piHarness{}.ParseEvent([]byte(`{"type":"queue_update","steering":[]}`))
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Kind != KindUnknown {
		t.Errorf("Kind = %v, want KindUnknown", evt.Kind)
	}
}

func TestPi_ParseEvent_MalformedJSON(t *testing.T) {
	// A non-JSON line is recorded as KindUnknown with Raw preserved, never an
	// error, so the runner tolerates schema drift / stray stderr bleed.
	evt, err := piHarness{}.ParseEvent([]byte(`not json at all`))
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Kind != KindUnknown {
		t.Errorf("Kind = %v, want KindUnknown", evt.Kind)
	}
	if len(evt.Raw) == 0 {
		t.Error("Raw should be preserved even for unparseable lines")
	}
}

// TestPi_ParseEvent_Fixture replays a full headless run captured in the
// shape pi emits under `pi -p --mode json` (see docs/json.md and
// docs/session-format.md in the pi package) and asserts the canonical event
// sequence. The four default tools (Read, Write, Edit, Bash) share one
// tool_execution_* shape; the fixture exercises it with a Bash invocation.
func TestPi_ParseEvent_Fixture(t *testing.T) {
	f, err := os.Open("testdata/pi/headless-run.ndjson")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	var kinds []EventKind
	var sessionID, resultText string
	var resultUsage *Usage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		evt, err := piHarness{}.ParseEvent([]byte(line))
		if err != nil {
			t.Fatalf("ParseEvent: %v", err)
		}
		if evt.Kind == KindUnknown {
			continue
		}
		kinds = append(kinds, evt.Kind)
		if evt.SessionID != "" {
			sessionID = evt.SessionID
		}
		if evt.Kind == KindResult {
			resultText = evt.Text
			resultUsage = evt.Usage
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}

	want := []EventKind{
		KindSystemInit,
		KindAssistantText, // first turn: tool-use message
		KindToolCallStart,
		KindToolCallEnd,
		KindAssistantText, // second turn: final answer
		KindResult,
	}
	if len(kinds) != len(want) {
		t.Fatalf("event kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("event[%d] kind = %v, want %v", i, kinds[i], want[i])
		}
	}
	if sessionID == "" {
		t.Error("no session id seen across the run")
	}
	if resultText == "" {
		t.Error("terminal result carried no text")
	}
	if resultUsage == nil || resultUsage.InputTokens == 0 {
		t.Errorf("terminal result carried no usage: %+v", resultUsage)
	}
}

func TestPi_AuthEnv_Empty(t *testing.T) {
	// Provider keys come from the inherited env; the harness adds none.
	env, err := piHarness{}.AuthEnv(AuthConfig{PiAPIKey: "reserved", AnthropicAPIKey: "k"})
	if err != nil {
		t.Fatalf("AuthEnv: %v", err)
	}
	if len(env) != 0 {
		t.Errorf("AuthEnv should be empty in v1; got %v", env)
	}
}

func TestPi_Capabilities(t *testing.T) {
	caps := piHarness{}.Capabilities()
	if !caps.SupportsResume {
		t.Error("Pi should advertise resume")
	}
	if caps.SupportsSystemPrompt {
		t.Error("Pi has no system-prompt pass-through in v1; should be false")
	}
	if !caps.EmitsUsage {
		t.Error("Pi should advertise usage")
	}
	if caps.EmitsCost {
		t.Error("Pi does not surface cost")
	}
}

func TestPi_RegisteredAtInit(t *testing.T) {
	h, ok := Lookup(Pi)
	if !ok {
		t.Fatal("Pi not registered")
	}
	if h.ID() != Pi {
		t.Errorf("Lookup(Pi).ID() = %q", h.ID())
	}
}
