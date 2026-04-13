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

// TestClaudeCodeAdapter_RealFixture parses a real captured multi-tool
// turn and asserts the adapter produces the expected event mix.
// Long assistant text, thinking, tool_use inputs, and tool_result
// payloads were trimmed before check-in to keep the fixture compact;
// event shape is otherwise unchanged from the on-disk original.
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

	counts := map[string]int{}
	for _, ev := range tr.Events {
		key := ev.Type
		if ev.Subtype != "" {
			key += "/" + ev.Subtype
		}
		counts[key]++
	}
	for _, want := range []string{
		ClaudeTypeSystem + "/" + ClaudeSubtypeInit,
		ClaudeTypeAssistant,
		ClaudeTypeUser,
		ClaudeTypeResult + "/" + ClaudeSubtypeSuccess,
	} {
		if counts[want] == 0 {
			t.Errorf("expected at least one %q event, got none", want)
		}
	}
	// Multi-tool trajectory should have plenty of assistant and user
	// turns, not just a single round-trip.
	if counts[ClaudeTypeAssistant] < 5 {
		t.Errorf("assistant events = %d, want >= 5 (multi-tool trajectory)", counts[ClaudeTypeAssistant])
	}
	if counts[ClaudeTypeUser] < 3 {
		t.Errorf("user events = %d, want >= 3 (tool_result rounds)", counts[ClaudeTypeUser])
	}

	// Find the system.init event and verify it carries usable
	// metadata. Position is not guaranteed — real streams can open
	// with a rate_limit_event before init lands.
	var init SDKSystemInit
	foundInit := false
	for _, ev := range tr.Events {
		if ev.Type == ClaudeTypeSystem && ev.Subtype == ClaudeSubtypeInit {
			if err := ev.Decode(&init); err != nil {
				t.Fatalf("Decode init: %v", err)
			}
			foundInit = true
			break
		}
	}
	if !foundInit {
		t.Fatalf("no system.init event in fixture")
	}
	if init.Model == "" {
		t.Errorf("init.Model empty")
	}
	if init.ClaudeCodeVersion == "" {
		t.Errorf("init.ClaudeCodeVersion empty")
	}

	// The final event must be the terminal result.success.
	last := tr.Events[len(tr.Events)-1]
	if last.Type != ClaudeTypeResult || last.Subtype != ClaudeSubtypeSuccess {
		t.Fatalf("last event = {%q,%q}, want result/success", last.Type, last.Subtype)
	}
	var res SDKResultSuccess
	if err := last.Decode(&res); err != nil {
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

	// Spot-check that assistant messages carry the content block mix
	// this fixture was chosen for — thinking + tool_use + text.
	blockKinds := map[string]int{}
	for _, ev := range tr.Events {
		if ev.Type != ClaudeTypeAssistant {
			continue
		}
		var asm SDKAssistantMessage
		if err := ev.Decode(&asm); err != nil {
			t.Fatalf("Decode assistant: %v", err)
		}
		// message is raw — peek just enough to count block kinds.
		var shaped struct {
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		}
		if err := decodeRaw(asm.Message, &shaped); err != nil {
			t.Fatalf("decode assistant message body: %v", err)
		}
		for _, c := range shaped.Content {
			blockKinds[c.Type]++
		}
	}
	for _, kind := range []string{"thinking", "tool_use", "text"} {
		if blockKinds[kind] == 0 {
			t.Errorf("no assistant content blocks of kind %q", kind)
		}
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

// decodeRaw unmarshals raw into v without the strict-decoder's
// DisallowUnknownFields — used when peeking into nested, vendor-owned
// JSON shapes (e.g. assistant message bodies) that this package
// intentionally does not model.
func decodeRaw(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}
