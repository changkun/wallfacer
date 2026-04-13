package trajectory

import (
	"encoding/json"
	"strings"
	"testing"
)

const claudeSampleNDJSON = `{"type":"system","subtype":"init","apiKeySource":"oauth","claude_code_version":"1.2.3","cwd":"/workspace","tools":["Read","Edit"],"mcp_servers":[],"model":"claude-opus-4-6","permissionMode":"default","slash_commands":["/help"],"output_style":"plain","skills":[],"plugins":[],"uuid":"00000000-0000-0000-0000-000000000001","session_id":"sess-1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]},"parent_tool_use_id":null,"uuid":"00000000-0000-0000-0000-000000000002","session_id":"sess-1"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"done"}]},"parent_tool_use_id":"t1","isSynthetic":true,"uuid":"00000000-0000-0000-0000-000000000003","session_id":"sess-1"}
{"type":"result","subtype":"success","duration_ms":4200,"duration_api_ms":3100,"is_error":false,"num_turns":3,"result":"done","stop_reason":"end_turn","total_cost_usd":0.0123,"usage":{"input_tokens":10,"output_tokens":5},"modelUsage":{"claude-opus-4-6":{"inputTokens":10,"outputTokens":5,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"webSearchRequests":0,"costUSD":0.0123,"contextWindow":200000,"maxOutputTokens":8192}},"permission_denials":[],"uuid":"00000000-0000-0000-0000-000000000004","session_id":"sess-1"}
`

func TestClaudeCodeAdapter_Parse(t *testing.T) {
	t.Parallel()

	tr, err := NewClaudeCodeAdapter().Parse([]byte(claudeSampleNDJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := tr.Provider, ProviderClaudeCode; got != want {
		t.Errorf("Provider = %q, want %q", got, want)
	}
	if got, want := tr.ProviderVersion, "claude-code/1.2.3"; got != want {
		t.Errorf("ProviderVersion = %q, want %q", got, want)
	}
	if got, want := len(tr.Messages), 4; got != want {
		t.Fatalf("len(Messages) = %d, want %d", got, want)
	}

	// Verify discriminators flowed through.
	kinds := []struct{ typ, sub string }{
		{TypeSystem, SubtypeInit},
		{TypeAssistant, ""},
		{TypeUser, ""},
		{TypeResult, SubtypeSuccess},
	}
	for i, want := range kinds {
		got := tr.Messages[i]
		if got.Type != want.typ || got.Subtype != want.sub {
			t.Errorf("Messages[%d] = {%q,%q}, want {%q,%q}", i, got.Type, got.Subtype, want.typ, want.sub)
		}
		if len(got.Raw) == 0 {
			t.Errorf("Messages[%d] Raw is empty", i)
		}
	}
}

func TestClaudeCodeAdapter_TypedDecode(t *testing.T) {
	t.Parallel()

	tr, err := NewClaudeCodeAdapter().Parse([]byte(claudeSampleNDJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var init SDKSystemInit
	if err := tr.Messages[0].Decode(&init); err != nil {
		t.Fatalf("Decode init: %v", err)
	}
	if init.Model != "claude-opus-4-6" {
		t.Errorf("init.Model = %q, want %q", init.Model, "claude-opus-4-6")
	}
	if init.ClaudeCodeVersion != "1.2.3" {
		t.Errorf("init.ClaudeCodeVersion = %q, want %q", init.ClaudeCodeVersion, "1.2.3")
	}

	var user SDKUserMessage
	if err := tr.Messages[2].Decode(&user); err != nil {
		t.Fatalf("Decode user: %v", err)
	}
	if !user.IsSynthetic {
		t.Errorf("user.IsSynthetic = false, want true")
	}
	if user.ParentToolUseID == nil || *user.ParentToolUseID != "t1" {
		t.Errorf("user.ParentToolUseID = %v, want pointer to %q", user.ParentToolUseID, "t1")
	}

	var res SDKResultSuccess
	if err := tr.Messages[3].Decode(&res); err != nil {
		t.Fatalf("Decode result: %v", err)
	}
	if res.NumTurns != 3 {
		t.Errorf("res.NumTurns = %d, want 3", res.NumTurns)
	}
	if res.TotalCostUSD != 0.0123 {
		t.Errorf("res.TotalCostUSD = %v, want 0.0123", res.TotalCostUSD)
	}
	mu, ok := res.ModelUsage["claude-opus-4-6"]
	if !ok {
		t.Fatalf("res.ModelUsage missing claude-opus-4-6 key")
	}
	if mu.InputTokens != 10 || mu.OutputTokens != 5 {
		t.Errorf("model usage tokens = (%d in, %d out), want (10, 5)", mu.InputTokens, mu.OutputTokens)
	}
}

func TestClaudeCodeAdapter_EmptyLinesSkipped(t *testing.T) {
	t.Parallel()

	input := "\n\n" + claudeSampleNDJSON + "\n\n"
	tr, err := NewClaudeCodeAdapter().Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := len(tr.Messages), 4; got != want {
		t.Errorf("len(Messages) = %d, want %d", got, want)
	}
}

func TestClaudeCodeAdapter_MalformedLine(t *testing.T) {
	t.Parallel()

	input := `{"type":"assistant","uuid":"u1","session_id":"s1","parent_tool_use_id":null,"message":{}}` + "\n" +
		`this is not json` + "\n"
	_, err := NewClaudeCodeAdapter().Parse([]byte(input))
	if err == nil {
		t.Fatalf("Parse returned nil error on malformed line")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error = %q, want mention of line 2", err.Error())
	}
}

func TestClaudeCodeAdapter_UnknownTypePreserved(t *testing.T) {
	t.Parallel()

	// A type this package does not model; adapter must keep it, not drop it.
	input := `{"type":"tool_progress","uuid":"u1","session_id":"s1","payload":{"tool":"Read","bytes":42}}` + "\n"
	tr, err := NewClaudeCodeAdapter().Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := len(tr.Messages), 1; got != want {
		t.Fatalf("len(Messages) = %d, want %d", got, want)
	}
	if tr.Messages[0].Type != "tool_progress" {
		t.Errorf("Type = %q, want %q", tr.Messages[0].Type, "tool_progress")
	}
	// Raw payload must still be decodable.
	var fields map[string]json.RawMessage
	if err := tr.Messages[0].Decode(&fields); err != nil {
		t.Errorf("Decode unknown: %v", err)
	}
	if _, ok := fields["payload"]; !ok {
		t.Errorf("unknown payload field dropped")
	}
}

func TestSDKMessage_DecodeEmpty(t *testing.T) {
	t.Parallel()

	var m SDKMessage // no Raw set
	var dst struct{}
	if err := m.Decode(&dst); err != ErrNoRawPayload {
		t.Errorf("Decode empty = %v, want %v", err, ErrNoRawPayload)
	}
}
