package runner

import (
	"testing"

	"latere.ai/x/wallfacer/internal/harness"
)

// TestParseHarnessOutput_ThinkingInert guards that a KindThinking block never
// gets picked as the final answer, even when it is the LAST text-bearing event
// before an empty terminal result. opencode emits reasoning on its own line; if
// the accumulator treated any non-empty Text as the answer fallback, a trailing
// reasoning block would overwrite the real answer.
func TestParseHarnessOutput_ThinkingInert(t *testing.T) {
	h, ok := harness.Lookup(harness.OpenCode)
	if !ok {
		t.Fatal("opencode harness not registered")
	}
	// Answer first, then a reasoning block, then an empty terminal result so the
	// last-text fallback is exercised.
	raw := `{"type":"text","sessionID":"s","part":{"type":"text","text":"the real answer"}}
{"type":"reasoning","sessionID":"s","part":{"type":"reasoning","text":"secret thoughts that are not the answer"}}
{"type":"result","sessionID":"s","result":"","is_error":false,"stop_reason":"end_turn"}`

	out, err := parseHarnessOutput(h, raw)
	if err != nil {
		t.Fatalf("parseHarnessOutput: %v", err)
	}
	if out.Result != "the real answer" {
		t.Errorf("Result = %q, want %q (reasoning must not become the answer)", out.Result, "the real answer")
	}
}
