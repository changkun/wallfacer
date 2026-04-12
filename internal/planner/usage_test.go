package planner

import (
	"testing"
)

func TestExtractUsage_StreamJSONResultLine(t *testing.T) {
	raw := []byte(`{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}
{"type":"result","stop_reason":"end_turn","result":"done","session_id":"s1","is_error":false,"total_cost_usd":0.0456,"usage":{"input_tokens":200,"output_tokens":80,"cache_read_input_tokens":15,"cache_creation_input_tokens":5}}`)

	u, ok := ExtractUsage(raw)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.InputTokens != 200 || u.OutputTokens != 80 {
		t.Errorf("tokens: got (%d,%d), want (200,80)", u.InputTokens, u.OutputTokens)
	}
	if u.CacheReadInputTokens != 15 || u.CacheCreationInputTokens != 5 {
		t.Errorf("cache tokens: got (%d,%d), want (15,5)", u.CacheReadInputTokens, u.CacheCreationInputTokens)
	}
	if u.CostUSD != 0.0456 {
		t.Errorf("cost: got %v, want 0.0456", u.CostUSD)
	}
	if u.StopReason != "end_turn" {
		t.Errorf("stop_reason: got %q, want end_turn", u.StopReason)
	}
}

func TestExtractUsage_SingleBlobNoTypeField(t *testing.T) {
	// Some code paths emit a single JSON object without a "type" field.
	// The extractor should still pick it up via the fallback branch.
	raw := []byte(`{"stop_reason":"max_tokens","result":"partial","session_id":"s1","total_cost_usd":0.01,"usage":{"input_tokens":50,"output_tokens":20}}`)

	u, ok := ExtractUsage(raw)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.InputTokens != 50 || u.OutputTokens != 20 {
		t.Errorf("tokens: got (%d,%d), want (50,20)", u.InputTokens, u.OutputTokens)
	}
	if u.StopReason != "max_tokens" {
		t.Errorf("stop_reason: got %q, want max_tokens", u.StopReason)
	}
}

func TestExtractUsage_EmptyOrMalformed(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte(""),
		[]byte("not json"),
		[]byte("{not a real object"),
	}
	for i, raw := range cases {
		if _, ok := ExtractUsage(raw); ok {
			t.Errorf("case %d: expected ok=false for %q", i, raw)
		}
	}
}

func TestExtractUsage_PrefersResultLineOverAssistant(t *testing.T) {
	// If both an assistant-type line and a result-type line are present,
	// the result line wins because it carries usage and stop_reason.
	raw := []byte(`{"type":"assistant","message":{"role":"assistant","content":[]}}
{"type":"result","stop_reason":"end_turn","total_cost_usd":0.02,"usage":{"input_tokens":10,"output_tokens":3}}`)

	u, ok := ExtractUsage(raw)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.InputTokens != 10 || u.StopReason != "end_turn" {
		t.Errorf("expected to pick result line, got %+v", u)
	}
}
