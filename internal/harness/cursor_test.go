package harness

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestCursor_BuildArgv_Basic(t *testing.T) {
	argv, stdin, err := cursorHarness{}.BuildArgv(Request{Prompt: "list files"})
	if err != nil {
		t.Fatalf("BuildArgv: %v", err)
	}
	if stdin != nil {
		t.Errorf("stdin = %v, want nil", stdin)
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"-p list files",
		"--output-format stream-json",
		"--sandbox enabled",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %v", want, argv)
		}
	}
}

func TestCursor_BuildArgv_WorkspaceModelResume(t *testing.T) {
	argv, _, _ := cursorHarness{}.BuildArgv(Request{
		Prompt:    "task",
		Cwd:       "/work/dir",
		Model:     "composer",
		SessionID: "sess-42",
	})
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"--workspace /work/dir",
		"--model composer",
		"--resume sess-42",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %v", want, argv)
		}
	}
}

func TestCursor_BuildArgv_PermissionFull(t *testing.T) {
	argv, _, _ := cursorHarness{}.BuildArgv(Request{Prompt: "x", Permission: PermissionFull})
	joined := strings.Join(argv, " ")
	for _, want := range []string{"--force", "--trust", "--approve-mcps"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Full permission argv missing %q: %v", want, argv)
		}
	}
	if strings.Contains(joined, "--mode") {
		t.Errorf("Full permission should not set --mode: %v", argv)
	}
}

func TestCursor_BuildArgv_PermissionEdit(t *testing.T) {
	argv, _, _ := cursorHarness{}.BuildArgv(Request{Prompt: "x", Permission: PermissionEdit})
	joined := strings.Join(argv, " ")
	// Edit must still inject --force; without it cursor only proposes edits.
	if !strings.Contains(joined, "--mode agent") {
		t.Errorf("Edit permission should set --mode agent: %v", argv)
	}
	if !strings.Contains(joined, "--force") {
		t.Errorf("Edit permission must inject --force for headless writes: %v", argv)
	}
}

func TestCursor_BuildArgv_PermissionReadOnly(t *testing.T) {
	argv, _, _ := cursorHarness{}.BuildArgv(Request{Prompt: "x", Permission: PermissionReadOnly})
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--mode plan") {
		t.Errorf("ReadOnly permission should set --mode plan: %v", argv)
	}
	if strings.Contains(joined, "--force") {
		t.Errorf("ReadOnly permission must not inject --force: %v", argv)
	}
}

func TestCursor_BuildArgv_SystemPromptPrepended(t *testing.T) {
	argv, _, _ := cursorHarness{}.BuildArgv(Request{Prompt: "do it", SystemPrompt: "be careful"})
	// SystemPrompt has no native flag; it is prepended into the -p value.
	var promptVal string
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "-p" {
			promptVal = argv[i+1]
		}
	}
	if !strings.Contains(promptVal, "be careful") || !strings.Contains(promptVal, "do it") {
		t.Errorf("system prompt should be prepended into -p value; got %q", promptVal)
	}
	if strings.Contains(strings.Join(argv, " "), "--append-system-prompt") {
		t.Errorf("cursor has no append-system-prompt flag: %v", argv)
	}
}

func TestCursor_BuildArgv_MCPConfig(t *testing.T) {
	argv, _, err := cursorHarness{}.BuildArgv(Request{
		Prompt:     "x",
		MCPServers: []MCPServer{{Name: "fs", Command: "mcp-fs", Args: []string{"--root", "/"}}},
	})
	if err != nil {
		t.Fatalf("BuildArgv: %v", err)
	}
	var cfgPath string
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "--mcp-config" {
			cfgPath = argv[i+1]
		}
	}
	if cfgPath == "" {
		t.Fatalf("argv missing --mcp-config: %v", argv)
	}
	defer func() { _ = os.Remove(cfgPath) }()
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read mcp config: %v", err)
	}
	for _, want := range []string{"mcpServers", "fs", "mcp-fs"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("mcp config missing %q: %s", want, data)
		}
	}
}

func TestCursor_BuildArgv_NoMCPConfigWhenEmpty(t *testing.T) {
	argv, _, _ := cursorHarness{}.BuildArgv(Request{Prompt: "x"})
	if strings.Contains(strings.Join(argv, " "), "--mcp-config") {
		t.Errorf("no MCP servers should mean no --mcp-config: %v", argv)
	}
}

func TestCursor_ParseEvent_SystemInit(t *testing.T) {
	raw := []byte(`{"type":"system","subtype":"init","session_id":"sess-1","model":"Composer 2.5 Fast"}`)
	evt, _ := cursorHarness{}.ParseEvent(raw)
	if evt.Kind != KindSystemInit {
		t.Errorf("Kind = %v, want KindSystemInit", evt.Kind)
	}
	if evt.SessionID != "sess-1" {
		t.Errorf("SessionID = %q", evt.SessionID)
	}
}

func TestCursor_ParseEvent_UserResult(t *testing.T) {
	raw := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hi"}]},"session_id":"s"}`)
	evt, _ := cursorHarness{}.ParseEvent(raw)
	if evt.Kind != KindUserResult {
		t.Errorf("Kind = %v, want KindUserResult", evt.Kind)
	}
}

func TestCursor_ParseEvent_AssistantText(t *testing.T) {
	raw := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}]},"session_id":"s"}`)
	evt, _ := cursorHarness{}.ParseEvent(raw)
	if evt.Kind != KindAssistantText {
		t.Errorf("Kind = %v, want KindAssistantText", evt.Kind)
	}
	if evt.Text != "hello world" {
		t.Errorf("Text = %q, want %q", evt.Text, "hello world")
	}
}

func TestCursor_ParseEvent_ToolCallStarted(t *testing.T) {
	raw := []byte(`{"type":"tool_call","subtype":"started","call_id":"c1","tool_call":{"shellToolCall":{"args":{}}}}`)
	evt, _ := cursorHarness{}.ParseEvent(raw)
	if evt.Kind != KindToolCallStart {
		t.Errorf("Kind = %v, want KindToolCallStart", evt.Kind)
	}
	if evt.Tool == nil || evt.Tool.ID != "c1" {
		t.Fatalf("Tool = %+v", evt.Tool)
	}
	if evt.Tool.Name != "shellToolCall" {
		t.Errorf("Tool.Name = %q, want shellToolCall", evt.Tool.Name)
	}
}

func TestCursor_ParseEvent_ToolCallCompleted(t *testing.T) {
	raw := []byte(`{"type":"tool_call","subtype":"completed","call_id":"c1","tool_call":{"shellToolCall":{"args":{}}}}`)
	evt, _ := cursorHarness{}.ParseEvent(raw)
	if evt.Kind != KindToolCallEnd {
		t.Errorf("Kind = %v, want KindToolCallEnd", evt.Kind)
	}
	if evt.Tool == nil || len(evt.Tool.Output) == 0 {
		t.Errorf("completed tool call should carry Output: %+v", evt.Tool)
	}
}

func TestCursor_ParseEvent_Result(t *testing.T) {
	raw := []byte(`{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"s","usage":{"inputTokens":100,"outputTokens":50,"cacheReadTokens":7,"cacheWriteTokens":3}}`)
	evt, _ := cursorHarness{}.ParseEvent(raw)
	if evt.Kind != KindResult {
		t.Errorf("Kind = %v, want KindResult", evt.Kind)
	}
	if evt.Subtype != "success" {
		t.Errorf("Subtype = %q", evt.Subtype)
	}
	if evt.Text != "done" {
		t.Errorf("Text = %q", evt.Text)
	}
	if evt.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if evt.Usage.InputTokens != 100 || evt.Usage.OutputTokens != 50 {
		t.Errorf("Usage tokens = %+v", evt.Usage)
	}
	if evt.Usage.CacheReadTokens != 7 {
		t.Errorf("CacheReadTokens = %d, want 7", evt.Usage.CacheReadTokens)
	}
	if evt.Usage.CacheCreationTokens != 3 {
		t.Errorf("CacheCreationTokens = %d, want 3 (from cacheWriteTokens)", evt.Usage.CacheCreationTokens)
	}
}

func TestCursor_ParseEvent_ResultError(t *testing.T) {
	raw := []byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"session_id":"s"}`)
	evt, _ := cursorHarness{}.ParseEvent(raw)
	if evt.Kind != KindError {
		t.Errorf("Kind = %v, want KindError", evt.Kind)
	}
	if evt.Subtype != "error_during_execution" {
		t.Errorf("Subtype = %q", evt.Subtype)
	}
}

func TestCursor_ParseEvent_Unknown(t *testing.T) {
	evt, err := cursorHarness{}.ParseEvent([]byte(`{"type":"future_event"}`))
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Kind != KindUnknown {
		t.Errorf("Kind = %v, want KindUnknown", evt.Kind)
	}
}

// TestCursor_ParseEvent_Fixture replays a full headless run captured from a
// real `cursor-agent -p ... --output-format stream-json --force` invocation
// and asserts the canonical event sequence.
func TestCursor_ParseEvent_Fixture(t *testing.T) {
	f, err := os.Open("testdata/cursor/headless-run.ndjson")
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
		evt, err := cursorHarness{}.ParseEvent([]byte(line))
		if err != nil {
			t.Fatalf("ParseEvent: %v", err)
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
		KindUserResult,
		KindToolCallStart,
		KindToolCallEnd,
		KindAssistantText,
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

func TestCursor_AuthEnv(t *testing.T) {
	env, err := cursorHarness{}.AuthEnv(AuthConfig{CursorAPIKey: "key-test"})
	if err != nil {
		t.Fatalf("AuthEnv: %v", err)
	}
	if env["CURSOR_API_KEY"] != "key-test" {
		t.Errorf("AuthEnv = %v", env)
	}
}

func TestCursor_AuthEnv_Empty(t *testing.T) {
	env, _ := cursorHarness{}.AuthEnv(AuthConfig{})
	if _, ok := env["CURSOR_API_KEY"]; ok {
		t.Errorf("empty key should not set CURSOR_API_KEY: %v", env)
	}
}

func TestCursor_Capabilities(t *testing.T) {
	caps := cursorHarness{}.Capabilities()
	if !caps.SupportsResume {
		t.Error("Cursor should advertise resume")
	}
	if !caps.SupportsMCP {
		t.Error("Cursor should advertise MCP")
	}
	if caps.SupportsSystemPrompt {
		t.Error("Cursor has no append-system-prompt flag; should be false")
	}
	if !caps.EmitsUsage {
		t.Error("Cursor should advertise usage")
	}
	if caps.EmitsCost {
		t.Error("Cursor does not surface cost")
	}
}

func TestCursor_RegisteredAtInit(t *testing.T) {
	h, ok := Lookup(Cursor)
	if !ok {
		t.Fatal("Cursor not registered")
	}
	if h.ID() != Cursor {
		t.Errorf("Lookup(Cursor).ID() = %q", h.ID())
	}
}
