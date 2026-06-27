package harness

import "testing"

// TestEventKind_String pins the wire tokens served by the normalized
// transcript stream. The frontend renderer switches on these exact strings,
// so a rename here is a breaking wire change.
func TestEventKind_String(t *testing.T) {
	cases := map[EventKind]string{
		KindUnknown:       "unknown",
		KindSystemInit:    "system_init",
		KindAssistantText: "assistant",
		KindThinking:      "thinking",
		KindToolCallStart: "tool_start",
		KindToolCallEnd:   "tool_end",
		KindUserResult:    "user_result",
		KindResult:        "result",
		KindError:         "error",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("EventKind(%d).String() = %q, want %q", k, got, want)
		}
	}
	// An out-of-range value degrades to "unknown" rather than a Go default.
	if got := EventKind(99).String(); got != "unknown" {
		t.Errorf("EventKind(99).String() = %q, want unknown", got)
	}
}
