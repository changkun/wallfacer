package trajectory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Fixture files committed under testdata/claude-code/ are lightly-edited
// real outputs captured from a local Claude Code run. Schema-drift tests
// in this file decode each line strictly (DisallowUnknownFields) against
// our typed mirror — so when the upstream Zod schema adds a field we
// haven't mirrored yet, a test here fails with a pointer to the missing
// field instead of the drift sneaking in silently.

// TestClaudeCodeAdapter_RealFixture parses a real captured turn and
// asserts the adapter produces the expected event sequence. This is a
// "soft" test — it verifies parsing works end-to-end without enforcing
// schema completeness. The companion strict test enforces that.
func TestClaudeCodeAdapter_RealFixture(t *testing.T) {
	t.Parallel()

	raw := mustRead(t, "testdata/claude-code/sample-turn-with-tools.jsonl")
	tr, err := NewClaudeCodeAdapter().Parse(raw)
	if err != nil {
		t.Fatalf("Parse real fixture: %v", err)
	}

	if got, want := tr.Provider, ProviderClaudeCode; got != want {
		t.Errorf("Provider = %q, want %q", got, want)
	}
	if tr.ProviderVersion == "" {
		t.Errorf("ProviderVersion empty; expected claude-code/<version> from init")
	}

	// Expected event sequence, matching the fixture's 9 lines:
	//   system/init, assistant, assistant, rate_limit_event, user,
	//   assistant, user, assistant, result/success.
	wantTypes := []struct{ Type, Subtype string }{
		{ClaudeTypeSystem, ClaudeSubtypeInit},
		{ClaudeTypeAssistant, ""},
		{ClaudeTypeAssistant, ""},
		{"rate_limit_event", ""},
		{ClaudeTypeUser, ""},
		{ClaudeTypeAssistant, ""},
		{ClaudeTypeUser, ""},
		{ClaudeTypeAssistant, ""},
		{ClaudeTypeResult, ClaudeSubtypeSuccess},
	}
	if got, want := len(tr.Events), len(wantTypes); got != want {
		t.Fatalf("len(Events) = %d, want %d", got, want)
	}
	for i, w := range wantTypes {
		got := tr.Events[i]
		if got.Type != w.Type || got.Subtype != w.Subtype {
			t.Errorf("Events[%d] = {%q,%q}, want {%q,%q}", i, got.Type, got.Subtype, w.Type, w.Subtype)
		}
	}

	// Spot-check typed decoding of the init message.
	var init SDKSystemInit
	if err := tr.Events[0].Decode(&init); err != nil {
		t.Fatalf("Decode init: %v", err)
	}
	if init.Model == "" {
		t.Errorf("init.Model empty")
	}
	if init.ClaudeCodeVersion == "" {
		t.Errorf("init.ClaudeCodeVersion empty")
	}

	// Spot-check typed decoding of the final result.
	var res SDKResultSuccess
	if err := tr.Events[8].Decode(&res); err != nil {
		t.Fatalf("Decode result: %v", err)
	}
	if res.NumTurns == 0 {
		t.Errorf("res.NumTurns = 0, want > 0")
	}
	if res.TotalCostUSD <= 0 {
		t.Errorf("res.TotalCostUSD = %v, want > 0", res.TotalCostUSD)
	}
	if len(res.ModelUsage) == 0 {
		t.Errorf("res.ModelUsage empty")
	}
}

// TestClaudeCodeAdapter_RealFixture_StrictDecode is the schema-drift
// guard. Each event in the fixture is strictly decoded into its typed
// variant with DisallowUnknownFields — any field present in the real
// stream that our Go mirror does not declare is a test failure.
//
// When this test breaks: read the upstream Zod schema at
// src/entrypoints/sdk/coreSchemas.ts in anthropics/claude-code, port
// the new field(s) into claude_types.go, then re-run. Do not silence
// the test — drift should be visible.
//
// Unknown top-level message types (e.g. rate_limit_event on the
// message union) are skipped here rather than failing: they're the
// forward-compat case the Unknown-preservation path already covers.
func TestClaudeCodeAdapter_RealFixture_StrictDecode(t *testing.T) {
	t.Parallel()

	raw := mustRead(t, "testdata/claude-code/sample-turn-with-tools.jsonl")
	tr, err := NewClaudeCodeAdapter().Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	for i, ev := range tr.Events {
		v, ok := typedVariantFor(ev)
		if !ok {
			// Not a variant we've modeled yet; forward-compat pass-through.
			continue
		}
		if err := strictDecode(ev.Raw, v); err != nil {
			t.Errorf("Events[%d] (type=%q subtype=%q) strict decode: %v",
				i, ev.Type, ev.Subtype, err)
		}
	}
}

// typedVariantFor returns a zero-valued pointer to the typed struct
// that maps to the event's type/subtype discriminator, or (nil, false)
// when the event is not one of the modeled variants.
func typedVariantFor(ev StreamEvent) (any, bool) {
	switch ev.Type {
	case ClaudeTypeAssistant:
		return new(SDKAssistantMessage), true
	case ClaudeTypeUser:
		return new(SDKUserMessage), true
	case ClaudeTypeStreamEvent:
		return new(SDKPartialAssistantMessage), true
	case ClaudeTypeSystem:
		if ev.Subtype == ClaudeSubtypeInit {
			return new(SDKSystemInit), true
		}
	case ClaudeTypeResult:
		switch ev.Subtype {
		case ClaudeSubtypeSuccess:
			return new(SDKResultSuccess), true
		case ClaudeSubtypeErrorDuringExecution,
			ClaudeSubtypeErrorMaxTurns,
			ClaudeSubtypeErrorMaxBudgetUSD,
			ClaudeSubtypeErrorMaxStructuredRetry:
			return new(SDKResultError), true
		}
	}
	return nil, false
}

// strictDecode unmarshals data into v with unknown-field detection.
func strictDecode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}
	return b
}
